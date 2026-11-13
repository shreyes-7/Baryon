package logging

import (
	"io"
	"log/slog"
	"os"
	"sync"
)

var (
	defaultLogger *slog.Logger
	mu            sync.RWMutex
)

func init() {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// SetOutput redirects the default logger to an io.Writer (e.g. io.Discard during interactive TUI mode).
func SetOutput(w io.Writer, level slog.Level) {
	mu.Lock()
	defer mu.Unlock()
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
	})
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// Logger returns the global structured logger.
func Logger() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return defaultLogger
}

