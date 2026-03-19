package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"meshserver/internal/config"
)

// New creates a JSON logger that writes to stdout and a rolling log file path.
func New(cfg *config.Config) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create log dir: %w", err)
	}

	path := filepath.Join(cfg.LogDir, "meshserver.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}

	handler := slog.NewJSONHandler(io.MultiWriter(os.Stdout, file), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler), file, nil
}
