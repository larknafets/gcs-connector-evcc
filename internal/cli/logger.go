package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/larknafets/gc-connector-evcc/internal/config"
)

// buildLogger sets up structured logging per the connector's log_file/debug
// config: log_file empty means stdout, debug=true lowers the level to
// Debug. Callers that want the GCS retry client's per-attempt request logs
// (method/URL on every attempt, including retries) must additionally assign
// the returned logger to that client's HTTP.Logger field, since *slog.Logger
// already satisfies retryablehttp.LeveledLogger.
// The returned closer must be called before the process exits.
func buildLogger(cfg config.Config) (logger *slog.Logger, closer func() error, err error) {
	out := os.Stdout
	closer = func() error { return nil }

	if cfg.LogFile != "" {
		f, openErr := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if openErr != nil {
			return nil, nil, fmt.Errorf("cli: opening log_file %s: %w", cfg.LogFile, openErr)
		}
		out = f
		closer = f.Close
	}

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})
	return slog.New(handler), closer, nil
}
