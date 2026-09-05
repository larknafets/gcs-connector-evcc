package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/larknafets/gcs-connector-evcc/internal/config"
)

// defaultSupervisorOptionsPath is where a Home Assistant Supervisor add-on
// container always finds its resolved options - never a .env file, never
// per-field environment variables. Its presence is the signal the connector
// is running as a Supervisor add-on at all; there is no separate opt-in
// flag.
const defaultSupervisorOptionsPath = "/data/options.json"

// defaultSupervisorStateDir is where state.json lives under Supervisor: the
// add-on's addon_config mount (map: addon_config:rw in config.yaml), which
// the Supervisor host exposes under app_configs/<slug> - unlike
// /data (where options.json lives), it's user-visible via Samba/SSH and not
// swept up in the private, undocumented add-on data folder.
const defaultSupervisorStateDir = "/config"

// loadConfig loads the connector's config, preferring optionsPath (the
// Supervisor's options.json) when it exists, falling back to the .env-based
// configPath otherwise - so binary/Docker Compose/wizard usage is
// unaffected on hosts where optionsPath never exists. It also returns the
// directory callers should pass to state.NewStore: supervisorStateDir for
// the Supervisor case, or configPath's own directory otherwise (matching
// the Docker/binary convention of keeping .env and state.json side by
// side).
func loadConfig(configPath, optionsPath, supervisorStateDir string) (cfg config.Config, stateDir string, err error) {
	if _, statErr := os.Stat(optionsPath); statErr == nil {
		cfg, err = config.FromOptionsJSON(optionsPath)
		if err != nil {
			return config.Config{}, "", fmt.Errorf("ungültige Supervisor-Config unter %s: %w", optionsPath, err)
		}
		return cfg, supervisorStateDir, nil
	}

	if _, statErr := os.Stat(configPath); errors.Is(statErr, os.ErrNotExist) {
		return config.Config{}, "", fmt.Errorf("keine Config unter %s gefunden - bitte zuerst \"gcs-connector init\" ausführen", configPath)
	}

	cfg, err = config.Load(configPath)
	if err != nil {
		return config.Config{}, "", fmt.Errorf("ungültige Config unter %s: %w", configPath, err)
	}
	return cfg, filepath.Dir(configPath), nil
}
