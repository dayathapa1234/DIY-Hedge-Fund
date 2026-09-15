package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"market-data-service/internal/config"
)

type Logger struct {
	*slog.Logger
	closer io.Closer
}

func New(cfg config.Config) (*Logger, error) {
	var writers []io.Writer
	var file *os.File

	if cfg.LogOutput == "stdout" || cfg.LogOutput == "both" {
		writers = append(writers, os.Stdout)
	}

	if cfg.LogOutput == "file" || cfg.LogOutput == "both" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogFilePath), 0o755); err != nil {
			return nil, err
		}
		var err error
		file, err = os.OpenFile(cfg.LogFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		writers = append(writers, file)
	}

	writer := io.Writer(os.Stdout)
	if len(writers) == 1 {
		writer = writers[0]
	}
	if len(writers) > 1 {
		writer = io.MultiWriter(writers...)
	}

	return &Logger{Logger: slog.New(slog.NewJSONHandler(writer, nil)), closer: file}, nil
}

func (l *Logger) Close() error {
	if l == nil || l.closer == nil {
		return nil
	}
	return l.closer.Close()
}
