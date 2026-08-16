package cli

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/larknafets/gcs-connector-evcc/internal/config"
)

func TestBuildLogger_DebugFalseUsesInfoLevel(t *testing.T) {
	logger, closer, err := buildLogger(config.Config{Debug: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closer()

	if logger.Enabled(nil, slog.LevelDebug) {
		t.Error("expected debug level disabled")
	}
	if !logger.Enabled(nil, slog.LevelInfo) {
		t.Error("expected info level enabled")
	}
}

func TestBuildLogger_DebugTrueUsesDebugLevel(t *testing.T) {
	logger, closer, err := buildLogger(config.Config{Debug: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closer()

	if !logger.Enabled(nil, slog.LevelDebug) {
		t.Error("expected debug level enabled")
	}
}

func TestBuildLogger_WritesToConfiguredLogFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connector.log")

	logger, closer, err := buildLogger(config.Config{LogFile: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	logger.Info("hello")
	closer()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected log file to exist: %v", err)
	}
	if len(raw) == 0 {
		t.Error("expected log file to contain output")
	}
}
