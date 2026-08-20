// Package cli wires cobra commands to the connector's building blocks
// (config, evcc/gcs clients, sync orchestrator, daemon loop, wizard). It is
// deliberately thin glue: substantial logic lives in the packages it wires
// together, each already covered at its own test seam.
package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/larknafets/gcs-connector-evcc/internal/config"
	"github.com/larknafets/gcs-connector-evcc/internal/evcc"
	"github.com/larknafets/gcs-connector-evcc/internal/gcs"
	"github.com/larknafets/gcs-connector-evcc/internal/loop"
	"github.com/larknafets/gcs-connector-evcc/internal/state"
	gcssync "github.com/larknafets/gcs-connector-evcc/internal/sync"
	"github.com/larknafets/gcs-connector-evcc/internal/webhook"
	"github.com/larknafets/gcs-connector-evcc/internal/wizard"
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
	cfg, effectiveConfigPath, err := loadConfig(configPath, defaultSupervisorOptionsPath)
	if err != nil {
		return err
	}

	logger, closeLogger, err := buildLogger(cfg)
	if err != nil {
		return err
	}
	defer closeLogger()

	gcsClient := gcs.NewClient(cfg.APIBaseURL, cfg.APIKey, cfg.APISecret, logger)

	orch := &gcssync.Orchestrator{
		EVCC:             evcc.NewClient(cfg.EVCCBaseURL),
		GCS:              gcsClient,
		Store:            state.NewStore(effectiveConfigPath),
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

	// The webhook listener binds before the daemon loop starts, so a port
	// conflict fails startup immediately instead of surfacing later from a
	// background goroutine.
	triggerCh, startWebhook, err := setupWebhookListener(cfg, logger)
	if err != nil {
		return err
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
		Trigger: triggerCh,
	}

	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if startWebhook != nil {
		go startWebhook(shutdownCtx)
		logger.Info("webhook-Listener gestartet, wartet auf Trigger-Requests", "port", cfg.Webhook.Port)
	}

	return runner.Run(shutdownCtx)
}

// setupWebhookListener binds the optional sync-now webhook listener
// (internal/webhook) if cfg.Webhook.Port is set, so a port conflict is
// reported synchronously here at startup rather than surfacing later from a
// background goroutine. It returns the trigger channel to wire into
// loop.Runner.Trigger and a start function that serves until ctx is
// canceled; both are nil when the webhook is disabled (the default).
func setupWebhookListener(cfg config.Config, logger *slog.Logger) (trigger chan struct{}, start func(ctx context.Context), err error) {
	if cfg.Webhook.Port == 0 {
		return nil, nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Webhook.Port))
	if err != nil {
		return nil, nil, fmt.Errorf("webhook-Listener konnte nicht auf Port %d starten: %w", cfg.Webhook.Port, err)
	}

	trigger = make(chan struct{}, 1)
	whServer := &webhook.Server{Secret: cfg.Webhook.Secret, Trigger: trigger, Logger: logger}
	start = func(ctx context.Context) {
		if err := whServer.Serve(ctx, ln); err != nil {
			logger.Error("webhook-Listener beendet", "error", err)
		}
	}
	return trigger, start, nil
}
