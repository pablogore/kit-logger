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

// ErrLoggerShutdown is returned by Flush, Shutdown and Sync once the logger has
// been shut down. Records submitted after that point are discarded rather than
// delivered, and no log call panics or blocks.
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

	// Shutdown stops accepting records, delivers those already accepted
	// (bounded by ctx) and releases resources. It is idempotent.
	Shutdown(ctx context.Context) error
}

// Flusher is implemented by handlers that hold records the host may need to
// deliver before the process exits — today, BufferedHandler.
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

	once    sync.Once
	done    chan struct{}
	err     error
	stopped atomic.Bool

	// rejected counts the records discarded because the logger was already
	// shut down when they were submitted. Without it those records would be
	// accounted for nowhere: they never reach a handler, so no handler
	// counter can see them.
	rejected atomic.Uint64
}

func newLifecycleGroup(handlers ...Flusher) *lifecycleGroup {
	return &lifecycleGroup{handlers: handlers, done: make(chan struct{})}
}

func (g *lifecycleGroup) isStopped() bool {
	return g != nil && g.stopped.Load()
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
	if g.stopped.Load() {
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

// shutdown runs the drain exactly once and reports the same result to every
// caller, including the hundredth concurrent one.
//
// A caller that did not win the race waits for the winner rather than returning
// a fabricated nil — but it still honours its own context, so a caller with a
// tight deadline is never held hostage by a slower one.
func (g *lifecycleGroup) shutdown(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ran := false
	g.once.Do(func() {
		ran = true
		g.err = g.shutdownAll(ctx)
		g.stopped.Store(true)
		close(g.done)
	})
	if ran {
		return g.err
	}

	select {
	case <-g.done:
		return g.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *lifecycleGroup) shutdownAll(ctx context.Context) error {
	// An already-expired context still stops every handler from accepting new
	// records — that part costs nothing and must happen — but it buys no
	// draining time, and the deadline is what the caller has to hear about.
	expired := ctx.Err()

	var first error
	for _, f := range g.handlers {
		if err := translateLifecycleErr(f.Shutdown(ctx)); err != nil && first == nil {
			first = err
		}
	}
	if expired != nil {
		return expired
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
