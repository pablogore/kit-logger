package testdata

import "log/slog"

type LogInvoker struct{}

func (LogInvoker) Invoke(logger *slog.Logger) {
	deeperCall(logger)
}

func deeperCall(logger *slog.Logger) {
	logger.Info("testing component")
}
