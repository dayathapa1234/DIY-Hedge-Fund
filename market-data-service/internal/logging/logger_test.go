package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"market-data-service/internal/config"
)

func TestFileLoggerWritesJSONLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "service.log")
	logger, err := New(config.Config{LogOutput: "file", LogFilePath: path})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("test log", "key", "value")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "test log") || !strings.Contains(string(content), "value") {
		t.Fatalf("expected log file content, got %s", string(content))
	}
}
