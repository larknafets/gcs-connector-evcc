package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/larknafets/gcs-connector-evcc/internal/config"
)

// defaultSupervisorOptionsPath is where a Home Assistant Supervisor add-on
// container always finds its resolved options - never a .env file, never
// per-field environment variables. Its presence is the signal the connector
// is running as a Supervisor add-on at all; there is no separate opt-in
// flag.
const defaultSupervisorOptionsPath = "/data/options.json"

// loadConfig loads the connector's config, preferring optionsPath (the
// Supervisor's options.json) when it exists, falling back to the .env-based
// configPath otherwise - so binary/Docker Compose/wizard usage is
// unaffected on hosts where optionsPath never exists. It also returns the
// path callers should pass to state.NewStore, so state.json ends up
// co-located with whichever config source was actually used (this lands
// state.json under /data automatically in the Supervisor case, since
// state.NewStore derives its directory from this path).
func loadConfig(configPath, optionsPath string) (cfg config.Config, effectiveConfigPath string, err error) {
	if _, statErr := os.Stat(optionsPath); statErr == nil {
		cfg, err = config.FromOptionsJSON(optionsPath)
		if err != nil {
			return config.Config{}, "", fmt.Errorf("ungültige Supervisor-Config unter %s: %w", optionsPath, err)
		}
		return cfg, optionsPath, nil
	}

	if _, statErr := os.Stat(configPath); errors.Is(statErr, os.ErrNotExist) {
		return config.Config{}, "", fmt.Errorf("keine Config unter %s gefunden - bitte zuerst \"gcs-connector init\" ausführen", configPath)
	}

	cfg, err = config.Load(configPath)
	if err != nil {
		return config.Config{}, "", fmt.Errorf("ungültige Config unter %s: %w", configPath, err)
	}
	return cfg, configPath, nil
}
