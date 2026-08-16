package wizard

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/larknafets/gcs-connector-evcc/internal/config"
)

// ErrAborted is returned when the user declines to overwrite an existing
// config or declines to continue after a failed connectivity check.
var ErrAborted = errors.New("wizard: aborted by user")

// RunInit runs the interactive `gcs-connector init` flow and writes the
// resulting .env file to configPath. It is intentionally thin: every
// decision it makes (defaults, prefill, overwrite confirmation, .env
// rendering) delegates to the tested functions in this package.
func RunInit(ctx context.Context, configPath string) error {
	_, statErr := os.Stat(configPath)
	exists := statErr == nil

	answers := DefaultAnswers()
	if exists {
		if cfg, err := config.Load(configPath); err == nil {
			answers = AnswersFromConfig(cfg)
		}

		var overwrite bool
		if err := huh.NewConfirm().
			Title("Bestehende Config gefunden unter " + configPath + " - überschreiben?").
			Value(&overwrite).
			Run(); err != nil {
			return fmt.Errorf("wizard: %w", err)
		}
		if !ConfirmOverwrite(exists, overwrite) {
			return ErrAborted
		}
	}

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("api_base_url").Description("Adresse der GCS-Instanz").Value(&answers.APIBaseURL),
		huh.NewInput().Title("evcc_base_url").Description("Adresse der lokalen evcc-Instanz").Value(&answers.EVCCBaseURL),
		huh.NewInput().Title("api_key").Value(&answers.APIKey),
		huh.NewInput().Title("api_secret").Password(true).Value(&answers.APISecret),
		huh.NewInput().Title("site_name").Value(&answers.SiteName),
		huh.NewInput().Title("sync_interval_minutes").Value(&answers.SyncIntervalMinutes),
		huh.NewInput().Title("ignore_vehicles").Description("kommagetrennt, optional").Value(&answers.IgnoreVehicles),
		huh.NewInput().Title("ignore_loadpoints").Description("kommagetrennt, optional").Value(&answers.IgnoreLoadpoints),
		huh.NewInput().Title("debug").Description("true/false").Value(&answers.Debug),
		huh.NewInput().Title("log_file").Description("leer = stdout").Value(&answers.LogFile),
	))
	if err := form.Run(); err != nil {
		return fmt.Errorf("wizard: %w", err)
	}

	for _, check := range []struct {
		name string
		url  string
	}{
		{"api_base_url", answers.APIBaseURL},
		{"evcc_base_url", answers.EVCCBaseURL},
	} {
		if err := CheckReachable(ctx, check.url); err != nil {
			var proceed bool
			if confirmErr := huh.NewConfirm().
				Title(fmt.Sprintf("%s (%s) ist nicht erreichbar - trotzdem übernehmen?", check.name, check.url)).
				Value(&proceed).
				Run(); confirmErr != nil {
				return fmt.Errorf("wizard: %w", confirmErr)
			}
			if !proceed {
				return ErrAborted
			}
		}
	}

	return WriteEnvFile(configPath, answers)
}
