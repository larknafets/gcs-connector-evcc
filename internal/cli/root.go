// Package cli wires cobra commands to the connector's building blocks
// (config, evcc/gcs clients, sync orchestrator, daemon loop, wizard). It is
// deliberately thin glue: substantial logic lives in the packages it wires
// together, each already covered at its own test seam.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/larknafets/gc-connector-evcc/internal/config"
	"github.com/larknafets/gc-connector-evcc/internal/evcc"
	"github.com/larknafets/gc-connector-evcc/internal/gcs"
	"github.com/larknafets/gc-connector-evcc/internal/loop"
	"github.com/larknafets/gc-connector-evcc/internal/state"
	gcssync "github.com/larknafets/gc-connector-evcc/internal/sync"
	"github.com/larknafets/gc-connector-evcc/internal/wizard"
)

// Execute builds and runs the gcs-connector command tree.
func Execute() error {
	var configFlag string
	var dryRun bool

	root := &cobra.Command{
		Use:           "gcs-connector",
		Short:         "Synct evcc-Ladedaten mit der GCS-Plattform",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath(configFlag, os.Getenv)
			return runMain(cmd.Context(), path, dryRun)
		},
	}
	root.PersistentFlags().StringVar(&configFlag, "config", "", "Pfad zur .env-Datei (Default: GCS_CONNECTOR_CONFIG oder ./.env)")
	root.Flags().BoolVar(&dryRun, "dry-run", false, "Einmal-Test: zeigt, was gesendet würde, ohne zu senden oder Zustand zu ändern")

	root.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Interaktiver Setup-Wizard, erzeugt die .env-Datei",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath(configFlag, os.Getenv)
			return wizard.RunInit(cmd.Context(), path)
		},
	})

	return root.Execute()
}

// runMain loads the config and either runs one dry-run preview or starts
// the daemon loop, depending on dryRun.
func runMain(ctx context.Context, configPath string, dryRun bool) error {
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("keine Config unter %s gefunden - bitte zuerst \"gcs-connector init\" ausführen", configPath)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("ungültige Config unter %s: %w", configPath, err)
	}

	logger, closeLogger, err := buildLogger(cfg)
	if err != nil {
		return err
	}
	defer closeLogger()

	gcsClient := gcs.NewClient(cfg.APIBaseURL, cfg.APIKey, cfg.APISecret)
	// *slog.Logger satisfies retryablehttp.LeveledLogger, so debug=true
	// (which lowers logger's level to Debug) also surfaces retryablehttp's
	// own per-attempt request logs, including retries.
	gcsClient.HTTP.Logger = logger

	orch := &gcssync.Orchestrator{
		EVCC:             evcc.NewClient(cfg.EVCCBaseURL),
		GCS:              gcsClient,
		Store:            state.NewStore(configPath),
		SiteName:         cfg.SiteName,
		IgnoreVehicles:   cfg.IgnoreVehicles,
		IgnoreLoadpoints: cfg.IgnoreLoadpoints,
		Logger:           logger,
	}

	if dryRun {
		result, err := orch.Preview(ctx)
		if err != nil {
			return fmt.Errorf("dry-run fehlgeschlagen: %w", err)
		}
		logger.Info("dry-run Vorschau", "neu", result.NewSessions, "bereits_vorhanden", result.AlreadyPresent)
		return nil
	}

	runner := &loop.Runner{
		Interval: time.Duration(cfg.SyncIntervalMinutes) * time.Minute,
		RunCycle: func(ctx context.Context) error {
			result, err := orch.RunCycle(ctx)
			if err != nil {
				return err
			}
			logger.Info("Sync-Takt abgeschlossen", "gesendet", result.Sent, "duplikate", result.DuplicateSkipped, "fehlgeschlagen", result.Failed)
			return nil
		},
		IsFatal: func(err error) bool { return errors.Is(err, gcssync.ErrFatal) },
		OnError: func(err error) {
			logger.Warn("Sync-Takt fehlgeschlagen, nächster Versuch beim nächsten Takt", "error", err)
		},
	}

	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return runner.Run(shutdownCtx)
}
