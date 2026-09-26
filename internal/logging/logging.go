// Package logging sets up slog. The TUI owns the terminal, so logs go to a file.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Open returns a logger writing to path and a function that closes the file.
// With debug set, debug records are included.
func Open(path string, debug bool) (*slog.Logger, func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return New(f, debug), f.Close, nil
}

// New returns a text logger writing to w.
func New(w io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// Discard returns a logger that drops everything. Handy in tests.
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
