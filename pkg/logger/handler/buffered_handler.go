package handler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Errors returned by BufferedHandler.
var (
	// ErrBufferFull is returned by Handle when the buffer has no room for the
	// record. The record is not delivered and Dropped is incremented.
	//
	// Note that slog.Logger discards the error returned by a handler, so
	// callers that need to observe overflow must read Dropped.
	ErrBufferFull = errors.New("buffered handler: buffer full")

	// ErrShutdown is returned by Handle, Flush and Shutdown once Shutdown has
	// been called. A rejected record is not delivered and Rejected is
	// incremented.
	ErrShutdown = errors.New("buffered handler: shut down")
)

// lifecycleState is the state of a bufferedCore.
//
//	running ──Shutdown()──▶ shuttingDown ──(queue drained | ctx done)──▶ stopped
//
// Invariants:
//   - Records are accepted only in running.
//   - The transition out of running happens under core.mu held for writing, so
//     no Handle call can be mid-send when it completes: after the transition
//     every record that was ever accepted is already in the queue.
//   - The queue channel is never closed, so no send can ever panic. The worker
//     is told to stop through shutdownCh instead.
type lifecycleState int32

const (
	stateRunning lifecycleState = iota
	stateShuttingDown
	stateStopped
)

// bufferedItem is one unit of work for the shared worker.
//
// ctx and next travel with the record so that an asynchronously delivered
// record is handed to exactly the derived handler that produced it, with the
// context of the call that produced it.
type bufferedItem struct {
	ctx    context.Context
	record slog.Record
	next   slog.Handler

	// barrier, when non-nil, marks a Flush checkpoint instead of a record.
	// The worker closes it after everything enqueued before it has been
	// delivered downstream.
	barrier chan struct{}
}

// bufferedCore owns the queue and the single worker goroutine. Every handler
// derived through WithAttrs or WithGroup shares one core.
type bufferedCore struct {
	queue chan bufferedItem

	// shutdownCh is closed once to tell the worker to drain and exit.
	shutdownCh chan struct{}
	closeOnce  sync.Once

	// workerDone is closed by the worker when it returns.
	workerDone chan struct{}

	// mu guards state. It is taken for reading on the Handle hot path and for
	// writing only by Shutdown and by the worker's final transition.
	mu    sync.RWMutex
	state lifecycleState

	dropped       atomic.Uint64
	rejected      atomic.Uint64
	handlerErrors atomic.Uint64

	errMu    sync.Mutex
	firstErr error
}

// BufferedHandler enqueues log records and delivers them asynchronously from a
// single worker goroutine.
//
// A BufferedHandler is a cheap view over a shared core: WithAttrs and WithGroup
// return a new view that reuses the same queue and worker, so deriving handlers
// (as slog.Logger.With does) allocates no goroutines and no buffers.
//
// If the buffer is full, Handle returns ErrBufferFull and increments Dropped.
// Use Flush to wait for delivery of accepted records and Shutdown to stop the
// handler and drain what it already accepted.
type BufferedHandler struct {
	core *bufferedCore
	next slog.Handler
}

// NewBufferedHandler creates a handler that buffers up to size records and
// delivers them to next from a single background goroutine.
//
// size is clamped to a minimum of 1. The caller should call Shutdown to release
// the worker goroutine and drain accepted records.
func NewBufferedHandler(next slog.Handler, size int) *BufferedHandler {
	if size < 1 {
		size = 1
	}
	c := &bufferedCore{
		queue:      make(chan bufferedItem, size),
		shutdownCh: make(chan struct{}),
		workerDone: make(chan struct{}),
		state:      stateRunning,
	}
	go c.run()
	return &BufferedHandler{core: c, next: next}
}

// Enabled reports whether the wrapped handler is enabled for level.
func (h *BufferedHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle enqueues record without blocking.
//
// The record is cloned before it crosses the goroutine boundary, so the caller
// may keep using its own record afterwards. ctx is delivered to the wrapped
// handler together with the record.
//
// It returns ErrBufferFull if the buffer is full and ErrShutdown if Shutdown
// has been called. It never blocks and never panics.
func (h *BufferedHandler) Handle(ctx context.Context, record slog.Record) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c := h.core

	c.mu.RLock()
	if c.state != stateRunning {
		c.mu.RUnlock()
		c.rejected.Add(1)
		return ErrShutdown
	}
	// The send is non-blocking, so the read lock is never held while waiting.
	// Holding it across the send is what guarantees that Shutdown's write lock
	// cannot complete while a send is in flight.
	select {
	case c.queue <- bufferedItem{ctx: ctx, record: record.Clone(), next: h.next}:
		c.mu.RUnlock()
		return nil
	default:
		c.mu.RUnlock()
		c.dropped.Add(1)
		return ErrBufferFull
	}
}

// WithAttrs returns a view of this handler whose records carry attrs. It shares
// this handler's queue and worker: no goroutine and no buffer are created.
func (h *BufferedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return &BufferedHandler{core: h.core, next: h.next}
	}
	return &BufferedHandler{core: h.core, next: h.next.WithAttrs(attrs)}
}

// WithGroup returns a view of this handler whose records are nested under name.
// It shares this handler's queue and worker: no goroutine and no buffer are
// created.
func (h *BufferedHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return &BufferedHandler{core: h.core, next: h.next}
	}
	return &BufferedHandler{core: h.core, next: h.next.WithGroup(name)}
}

// Flush returns once every record accepted before the call has been delivered
// to the wrapped handler, or ctx expires.
//
// It does not stop the handler: records may be accepted again afterwards.
//
// Flush returns the first downstream error observed since the previous Flush or
// Shutdown, if any; that error is reported once and then cleared. Use
// HandlerErrors for a count that is never cleared. It returns ctx.Err() on
// timeout and ErrShutdown if the handler is shutting down or stopped.
func (h *BufferedHandler) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c := h.core

	c.mu.RLock()
	stopped := c.state != stateRunning
	c.mu.RUnlock()
	if stopped {
		return ErrShutdown
	}

	// A barrier must get into the queue even when the queue is momentarily
	// full, so this send blocks — bounded by ctx and by the worker's lifetime.
	// The queue is never closed, so a blocking send here cannot panic.
	barrier := make(chan struct{})
	select {
	case c.queue <- bufferedItem{barrier: barrier}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.workerDone:
		return ErrShutdown
	}

	// The queue is FIFO and there is exactly one worker, so the barrier is
	// closed only after every earlier item has returned from next.Handle.
	select {
	case <-barrier:
		return c.takeErr()
	case <-ctx.Done():
		return ctx.Err()
	case <-c.workerDone:
		return ErrShutdown
	}
}

// Shutdown stops accepting records, delivers everything already accepted, and
// waits for the worker to exit — or returns ctx.Err() if ctx expires first.
//
// It is safe to call Shutdown concurrently and repeatedly. The first call that
// observes the drained worker reports the first downstream error seen, if any;
// later calls return nil. Handle returns ErrShutdown after Shutdown is called
// and never panics.
func (h *BufferedHandler) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c := h.core

	// Leaving stateRunning under the write lock is the whole safety argument:
	// it waits for every in-flight Handle to finish its send and prevents any
	// new one from starting, so the queue is complete from here on.
	c.mu.Lock()
	if c.state == stateRunning {
		c.state = stateShuttingDown
	}
	c.mu.Unlock()

	c.closeOnce.Do(func() { close(c.shutdownCh) })

	select {
	case <-c.workerDone:
		return c.takeErr()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Dropped is the number of records rejected because the buffer was full.
func (h *BufferedHandler) Dropped() uint64 { return h.core.dropped.Load() }

// Rejected is the number of records rejected because the handler was shutting
// down or stopped.
func (h *BufferedHandler) Rejected() uint64 { return h.core.rejected.Load() }

// HandlerErrors is the total number of errors returned by the wrapped handler.
// Unlike the error reported by Flush and Shutdown, this counter is never reset.
func (h *BufferedHandler) HandlerErrors() uint64 { return h.core.handlerErrors.Load() }

// run is the single worker. It delivers items until told to shut down, then
// drains whatever is still queued and exits.
func (h *bufferedCore) run() {
	defer func() {
		h.mu.Lock()
		h.state = stateStopped
		h.mu.Unlock()
		close(h.workerDone)
	}()

	for {
		select {
		case item := <-h.queue:
			h.process(item)
		case <-h.shutdownCh:
			h.drain()
			return
		}
	}
}

// drain delivers every item currently in the queue and returns. It is only
// called after the core has left stateRunning, so no further sends are
// possible and an empty queue is final.
func (h *bufferedCore) drain() {
	for {
		select {
		case item := <-h.queue:
			h.process(item)
		default:
			return
		}
	}
}

func (h *bufferedCore) process(item bufferedItem) {
	if item.barrier != nil {
		close(item.barrier)
		return
	}
	if err := item.next.Handle(item.ctx, item.record); err != nil {
		h.recordErr(err)
	}
}

func (h *bufferedCore) recordErr(err error) {
	h.handlerErrors.Add(1)
	h.errMu.Lock()
	if h.firstErr == nil {
		h.firstErr = err
	}
	h.errMu.Unlock()
}

// takeErr returns and clears the first downstream error observed since the last
// call, so a failure is reported exactly once to a Flush or Shutdown caller.
func (h *bufferedCore) takeErr() error {
	h.errMu.Lock()
	defer h.errMu.Unlock()
	err := h.firstErr
	h.firstErr = nil
	return err
}
