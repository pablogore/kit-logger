package logger

import "sync"

// resetGlobalsForTest returns every package-level global to its zero state.
//
// It lives in a _test.go file on purpose: the reset is a testing affordance,
// not API, so production consumers never see it. Without it the sync.Once
// guards would make the lazy-init and EnsureLoggerOnce paths testable exactly
// once per process, and the order tests happen to run in would decide which
// test got to observe them.
//
// The Onces are reset by assignment rather than through reflection: a
// sync.Once is a plain struct, and assigning a fresh one is both legal and
// obvious, where reflection on its internals is neither.
func resetGlobalsForTest() {
	globalLogger.Store(nil)
	globalOnce = sync.Once{}
	ensureLoggerOnce = sync.Once{}
	globalContextFieldExtractor.Store(nil)
}

// peekGlobalForTest returns the global logger without triggering L's lazy
// construction, so a test can assert whether one was installed at all. Reading
// L() instead would install one as a side effect of the assertion.
func peekGlobalForTest() Logger {
	if p := globalLogger.Load(); p != nil {
		return *p
	}
	return nil
}

// restoreGlobalForTest puts back a logger captured with peekGlobalForTest,
// including the "there was none" case that SetGlobal deliberately rejects.
func restoreGlobalForTest(log Logger) {
	if log == nil {
		globalLogger.Store(nil)
		return
	}
	globalLogger.Store(&log)
}
