package wizard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/larknafets/gcs-connector-evcc/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultAnswers_PrefillsOptionalFieldsOnly(t *testing.T) {
	a := DefaultAnswers()
	assert.Equal(t, "false", a.Debug)
	assert.Equal(t, "60", a.SyncIntervalMinutes)
	assert.Equal(t, "", a.LogFile)
	assert.Equal(t, "", a.IgnoreVehicles)
	assert.Equal(t, "", a.IgnoreLoadpoints)
	assert.Equal(t, "", a.APIBaseURL)
	assert.Equal(t, "", a.WebhookPort)
	assert.Equal(t, "", a.WebhookSecret)
}

func TestAnswersFromConfig_RoundTripsAllFields(t *testing.T) {
	cfg := config.Config{
		APIBaseURL:          "https://gcs.example.com",
		EVCCBaseURL:         "http://192.168.1.50:7070",
		APIKey:              "key123",
		APISecret:           "secret456",
		SiteName:            "Zuhause Carport",
		SyncIntervalMinutes: 60,
		IgnoreVehicles:      []string{"James", "Zweitwagen"},
		IgnoreLoadpoints:    []string{"Werkstatt"},
		Debug:               true,
		LogFile:             "/var/log/gcs.log",
		Webhook:             config.WebhookConfig{Port: 8080, Secret: "s3cr3t"},
	}

	a := AnswersFromConfig(cfg)
	assert.Equal(t, "https://gcs.example.com", a.APIBaseURL)
	assert.Equal(t, "http://192.168.1.50:7070", a.EVCCBaseURL)
	assert.Equal(t, "key123", a.APIKey)
	assert.Equal(t, "secret456", a.APISecret)
	assert.Equal(t, "Zuhause Carport", a.SiteName)
	assert.Equal(t, "60", a.SyncIntervalMinutes)
	assert.Equal(t, "James, Zweitwagen", a.IgnoreVehicles)
	assert.Equal(t, "Werkstatt", a.IgnoreLoadpoints)
	assert.Equal(t, "true", a.Debug)
	assert.Equal(t, "/var/log/gcs.log", a.LogFile)
	assert.Equal(t, "8080", a.WebhookPort)
	assert.Equal(t, "s3cr3t", a.WebhookSecret)
}

func TestAnswersFromConfig_WebhookPortZeroBecomesEmptyString(t *testing.T) {
	cfg := config.Config{Webhook: config.WebhookConfig{Port: 0}}
	a := AnswersFromConfig(cfg)
	assert.Equal(t, "", a.WebhookPort)
}

func TestWriteEnvFile_ProducesConfigThatConfigPackageCanLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	answers := Answers{
		APIBaseURL:          "https://gcs.example.com",
		EVCCBaseURL:         "http://192.168.1.50:7070",
		APIKey:              "key123",
		APISecret:           "secret456",
		SiteName:            "Zuhause Carport",
		SyncIntervalMinutes: "60",
		IgnoreVehicles:      "James, Zweitwagen",
		IgnoreLoadpoints:    "",
		Debug:               "true",
		LogFile:             "",
	}

	require.NoError(t, WriteEnvFile(path, answers))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "https://gcs.example.com", cfg.APIBaseURL)
	assert.Equal(t, 60, cfg.SyncIntervalMinutes)
	assert.Equal(t, []string{"James", "Zweitwagen"}, cfg.IgnoreVehicles)
	assert.True(t, cfg.Debug)
}

func TestWriteEnvFile_RoundTripsWebhookFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	answers := DefaultAnswers()
	answers.APIBaseURL = "https://gcs.example.com"
	answers.EVCCBaseURL = "http://192.168.1.50:7070"
	answers.APIKey = "k"
	answers.APISecret = "s"
	answers.SiteName = "site"
	answers.WebhookPort = "8080"
	answers.WebhookSecret = "s3cr3t"

	require.NoError(t, WriteEnvFile(path, answers))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Webhook.Port)
	assert.Equal(t, "s3cr3t", cfg.Webhook.Secret)
}

func TestWriteEnvFile_EscapesBackslashesForWindowsPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	answers := DefaultAnswers()
	answers.APIBaseURL = "https://gcs.example.com"
	answers.EVCCBaseURL = "http://192.168.1.50:7070"
	answers.APIKey = "k"
	answers.APISecret = "s"
	answers.SiteName = "site"
	answers.SyncIntervalMinutes = "30"
	answers.LogFile = `C:\Users\me\log.txt`

	require.NoError(t, WriteEnvFile(path, answers))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, `C:\Users\me\log.txt`, cfg.LogFile)
}

func TestWriteEnvFile_OverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("stale=data\n"), 0o644))

	answers := DefaultAnswers()
	answers.APIBaseURL = "https://gcs.example.com"
	answers.EVCCBaseURL = "http://192.168.1.50:7070"
	answers.APIKey = "k"
	answers.APISecret = "s"
	answers.SiteName = "site"
	answers.SyncIntervalMinutes = "30"

	require.NoError(t, WriteEnvFile(path, answers))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "stale=data")
}

func TestConfirmOverwrite(t *testing.T) {
	assert.True(t, ConfirmOverwrite(false, false), "no existing file: always proceed")
	assert.True(t, ConfirmOverwrite(true, true), "existing file, user confirmed: proceed")
	assert.False(t, ConfirmOverwrite(true, false), "existing file, user declined: don't proceed")
}

func TestCheckReachable_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.NoError(t, CheckReachable(ctx, server.URL))
}

func TestCheckReachable_Unreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	assert.Error(t, CheckReachable(ctx, "http://127.0.0.1:1"))
}
