package logger

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

// DefaultShutdownTimeout bounds the drain performed by ExitWithFlush. A process
// that is on its way out must not hang on a stuck downstream handler.
const DefaultShutdownTimeout = 5 * time.Second

// ErrLoggerShutdown is returned by Flush and Sync once the logger's shutdown has
// begun: there is no longer a "deliver what I just logged" to honour. Records
// submitted from that point on are discarded rather than delivered, and no log
// call panics or blocks.
//
// Shutdown does not return it for the ordinary reason. A Shutdown that follows a
// successful one observes the shared final result — normally nil — because the
// question it asks ("is the pipeline drained?") has been answered, not refused.
// It surfaces ErrLoggerShutdown only if a lifecycle handler reported that error
// itself.
var ErrLoggerShutdown = errors.New("logger: shut down")

// ManagedLogger is a Logger with an explicit, host-owned lifecycle: the host
// decides when in-flight records must be delivered and when the logger stops
// accepting new ones.
//
// Logger itself is unchanged, so existing consumers keep compiling. Type-assert
// to ManagedLogger to reach the lifecycle:
//
//	if m, ok := log.(logger.ManagedLogger); ok {
//		defer m.Shutdown(ctx)
//	}
type ManagedLogger interface {
	Logger

	// Flush returns once every record accepted before the call has been
	// delivered downstream, or ctx expires. It does not stop the logger.
	Flush(ctx context.Context) error

	// Shutdown stops accepting records, delivers those already accepted and
	// releases resources. It is idempotent.
	//
	// ctx bounds how long this call waits for the drain, not how long the
	// drain may take: the shutdown is one shared operation, so a caller that
	// gives up early gets its own ctx.Err() while the drain continues for the
	// callers still waiting.
	Shutdown(ctx context.Context) error
}

// Flusher is implemented by handlers that hold records the host may need to
// deliver before the process exits — today, BufferedHandler.
//
// Both methods must tolerate being called more than once: discovery over a
// hand-built chain can reach the same handler by more than one route.
type Flusher interface {
	Flush(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// Unwrapper lets a decorating handler expose the single handler it wraps, so
// lifecycle discovery can traverse a chain assembled outside New.
type Unwrapper interface {
	Unwrap() slog.Handler
}

// MultiUnwrapper is the fan-out counterpart of Unwrapper, implemented by
// handlers that delegate to several handlers at once.
type MultiUnwrapper interface {
	UnwrapAll() []slog.Handler
}

// maxUnwrapDepth bounds lifecycle discovery. A handler chain is a few links
// long in practice; the bound exists so a handler that unwraps to itself — or
// to a cycle — cannot hang construction. Depth is used instead of a visited set
// because slog.Handler implementations are not required to be comparable, and
// using one as a map key can panic.
const maxUnwrapDepth = 32

// collectFlushers walks a handler chain and returns every lifecycle-bearing
// handler it can reach, outermost first.
//
// It is only used for chains this package did not build (Config.Handler): when
// New assembles the pipeline itself it keeps a direct reference instead, so
// discovery never depends on a third-party handler implementing Unwrap. A
// handler that does not is not a failure — it simply ends that branch of the
// walk, as TestCollectFlushers_StopsAtAnOpaqueHandler pins.
//
// The walk does not deduplicate, so a caller's graph that reaches one handler
// through two routes yields it twice. That is harmless because Flusher requires
// idempotent Flush and Shutdown, and deduplicating would need the visited set
// this function deliberately avoids.
func collectFlushers(h slog.Handler) []Flusher {
	var found []Flusher

	var walk func(slog.Handler, int)
	walk = func(h slog.Handler, depth int) {
		if h == nil || depth > maxUnwrapDepth {
			return
		}
		if f, ok := h.(Flusher); ok {
			found = append(found, f)
		}
		switch u := h.(type) {
		case MultiUnwrapper:
			for _, next := range u.UnwrapAll() {
				walk(next, depth+1)
			}
		case Unwrapper:
			walk(u.Unwrap(), depth+1)
		}
	}
	walk(h, 0)

	return found
}

// lifecycleGroup owns the lifecycle of one logger pipeline. Loggers derived
// through With and WithContext share the same group by pointer, so shutting
// down a derived logger shuts down the root buffer it writes into.
//
// A nil *lifecycleGroup is valid and behaves as a logger with nothing to
// flush, which keeps hand-built SlogLogger values (as tests construct them)
// working.
type lifecycleGroup struct {
	handlers []Flusher

	// startOnce launches the shutdown exactly once. Starting the shutdown and
	// waiting for it are deliberately separate: the shutdown is one shared
	// operation, and each caller only decides how long it is willing to wait
	// for it.
	startOnce sync.Once

	// done is closed once the shutdown has run to completion, after err has
	// been written. A caller that receives from it therefore observes the
	// final err with no further synchronisation.
	done chan struct{}
	err  error

	// stopping is set before the first handler is asked to stop, so admission
	// closes at the instant the shutdown begins rather than when it finishes.
	stopping atomic.Bool

	// rejected counts the records discarded because the logger's shutdown had
	// already begun when they were submitted. Without it those records would
	// be accounted for nowhere: they never reach a handler, so no handler
	// counter can see them.
	rejected atomic.Uint64
}

func newLifecycleGroup(handlers ...Flusher) *lifecycleGroup {
	return &lifecycleGroup{handlers: handlers, done: make(chan struct{})}
}

// isStopping reports whether the logger has stopped accepting records. It turns
// true when the shutdown starts, not when it completes.
func (g *lifecycleGroup) isStopping() bool {
	return g != nil && g.stopping.Load()
}

// rejectRecord accounts for one record discarded after shutdown.
func (g *lifecycleGroup) rejectRecord() {
	if g != nil {
		g.rejected.Add(1)
	}
}

func (g *lifecycleGroup) rejectedCount() uint64 {
	if g == nil {
		return 0
	}
	return g.rejected.Load()
}

func (g *lifecycleGroup) flush(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if g.stopping.Load() {
		return ErrLoggerShutdown
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var first error
	for _, f := range g.handlers {
		if err := translateLifecycleErr(f.Flush(ctx)); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// shutdown starts the shutdown once and then waits for it under ctx.
//
// Starting and waiting are separate on purpose. Letting the first caller run
// the drain under its own context would make that context global: a caller
// with a 1ms deadline would freeze the result for a caller that was willing to
// wait five seconds, and every later caller would inherit a
// DeadlineExceeded that describes nobody's situation but the first one's. The
// underlying worker does not stop draining when a context expires anyway
// (see BufferedHandler), so a frozen error would also be a lie about what the
// pipeline actually did.
//
// So the shutdown is one shared operation with no deadline of its own, and ctx
// bounds only how long this caller waits for it. Callers that wait to the end
// all observe the same final error; a caller that gives up early gets its own
// ctx.Err() and the operation keeps going.
func (g *lifecycleGroup) shutdown(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	g.startOnce.Do(func() {
		// Close admission first. Every handler shutdown below stops accepting
		// records as its own first step, so leaving this until the drain is
		// over would open a window in which a record passes the logger-level
		// gate — consuming a rate-limit token and firing a counter — only to
		// be rejected by the handler underneath.
		g.stopping.Store(true)

		go func() {
			g.err = g.shutdownAllToCompletion()
			close(g.done)
		}()
	})

	select {
	case <-g.done:
		return g.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// shutdownAllToCompletion stops every lifecycle handler and waits for each one
// to finish draining.
//
// It uses a context with no deadline because the drain's duration belongs to
// the handlers, not to whichever caller happened to start it. That is not an
// unbounded wait dressed up as a bounded one: this goroutine lives exactly as
// long as the handler workers it waits on, so a downstream handler that blocks
// forever holds this goroutine and its own worker — the same worker it was
// already holding before this change.
func (g *lifecycleGroup) shutdownAllToCompletion() error {
	var first error
	for _, f := range g.handlers {
		if err := translateLifecycleErr(f.Shutdown(context.Background())); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// translateLifecycleErr maps the handler package's shutdown sentinel onto the
// logger-level one, so consumers match a single error regardless of which
// handler produced it.
func translateLifecycleErr(err error) error {
	if err != nil && errors.Is(err, handler.ErrShutdown) {
		return ErrLoggerShutdown
	}
	return err
}
