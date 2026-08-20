package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validEnvContent = `api_base_url="https://gcs.example.com"
evcc_base_url="http://192.168.1.50:7070"
api_key="key123"
api_secret="secret456"
`

const validOptionsJSON = `{
	"api_base_url": "https://gcs.example.com",
	"evcc_base_url": "http://192.168.1.50:7070",
	"api_key": "key999",
	"api_secret": "secret999"
}`

func TestLoadConfig_UsesOptionsJSONWhenPresent(t *testing.T) {
	dir := t.TempDir()
	optionsPath := filepath.Join(dir, "options.json")
	require.NoError(t, os.WriteFile(optionsPath, []byte(validOptionsJSON), 0o600))

	// configPath deliberately points at a .env that doesn't exist, to prove
	// options.json wins outright rather than merely being tried first.
	cfg, effectivePath, err := loadConfig(filepath.Join(dir, "nonexistent.env"), optionsPath)
	require.NoError(t, err)
	assert.Equal(t, "key999", cfg.APIKey)
	assert.Equal(t, optionsPath, effectivePath)
}

func TestLoadConfig_FallsBackToEnvWhenOptionsJSONAbsent(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte(validEnvContent), 0o600))

	cfg, effectivePath, err := loadConfig(envPath, filepath.Join(dir, "options.json"))
	require.NoError(t, err)
	assert.Equal(t, "key123", cfg.APIKey)
	assert.Equal(t, envPath, effectivePath)
}

func TestLoadConfig_MissingBothReturnsFriendlyInitHint(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	_, _, err := loadConfig(envPath, filepath.Join(dir, "options.json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gcs-connector init")
}

func TestLoadConfig_InvalidOptionsJSONSurfacesError(t *testing.T) {
	dir := t.TempDir()
	optionsPath := filepath.Join(dir, "options.json")
	require.NoError(t, os.WriteFile(optionsPath, []byte(`{"api_base_url": ""}`), 0o600))

	_, _, err := loadConfig(filepath.Join(dir, "nonexistent.env"), optionsPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Supervisor-Config")
}

func TestLoadConfig_InvalidEnvSurfacesError(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte(`api_base_url="only this field"`), 0o600))

	_, _, err := loadConfig(envPath, filepath.Join(dir, "options.json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ungültige Config")
}
