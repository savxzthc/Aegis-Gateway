package logger

import (
	"context"
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

func init() {
	defaultLogger = slog.New(slog.NewTextHandler(os.Stderr, nil))
}

// Init configures the global logger. format must be "json" or "text".
func Init(format string) {
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, nil)
	} else {
		handler = slog.NewTextHandler(os.Stderr, nil)
	}
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

func Info(msg string, args ...any) {
	defaultLogger.InfoContext(context.Background(), msg, args...)
}

func Error(msg string, args ...any) {
	defaultLogger.ErrorContext(context.Background(), msg, args...)
}

func Warn(msg string, args ...any) {
	defaultLogger.WarnContext(context.Background(), msg, args...)
}

func With(args ...any) *slog.Logger {
	return defaultLogger.With(args...)
}
