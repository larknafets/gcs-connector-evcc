package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeOptionsJSON writes raw as a temp options.json and returns its path.
func writeOptionsJSON(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "options.json")
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
	return path
}

func validRawFields() rawFields {
	return rawFields{
		APIBaseURL:  "https://gcs.example.com",
		EVCCBaseURL: "http://192.168.1.50:7070",
		APIKey:      "key123",
		APISecret:   "secret456",
		SiteName:    "Zuhause Carport",
	}
}

func intPtr(v int) *int { return &v }

// --- validate: every rule shared by FromMap and FromOptionsJSON, tested once. ---

func TestValidate_ValidMinimalConfig(t *testing.T) {
	cfg, err := validate(validRawFields())
	require.NoError(t, err)

	assert.Equal(t, "https://gcs.example.com", cfg.APIBaseURL)
	assert.Equal(t, "http://192.168.1.50:7070", cfg.EVCCBaseURL)
	assert.Equal(t, "key123", cfg.APIKey)
	assert.Equal(t, "secret456", cfg.APISecret)
	assert.Equal(t, "Zuhause Carport", cfg.SiteName)
	assert.Equal(t, 60, cfg.SyncIntervalMinutes)
	assert.Empty(t, cfg.IgnoreVehicles)
	assert.Empty(t, cfg.IgnoreLoadpoints)
	assert.False(t, cfg.Debug)
	assert.Equal(t, "", cfg.LogFile)
	assert.Equal(t, 0, cfg.Webhook.Port)
	assert.Equal(t, "", cfg.Webhook.Secret)
}

func TestValidate_SyncIntervalDefaultsTo60WhenUnset(t *testing.T) {
	cfg, err := validate(validRawFields())
	require.NoError(t, err)
	assert.Equal(t, 60, cfg.SyncIntervalMinutes)
}

func TestValidate_SyncIntervalUsesExplicitValue(t *testing.T) {
	raw := validRawFields()
	raw.SyncIntervalMinutes = intPtr(30)

	cfg, err := validate(raw)
	require.NoError(t, err)
	assert.Equal(t, 30, cfg.SyncIntervalMinutes)
}

func TestValidate_SyncIntervalMustBePositiveWhenSet(t *testing.T) {
	for _, v := range []int{0, -5} {
		t.Run(fmt.Sprintf("%d", v), func(t *testing.T) {
			raw := validRawFields()
			raw.SyncIntervalMinutes = intPtr(v)

			_, err := validate(raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "sync_interval_minutes")
		})
	}
}

func TestValidate_WebhookFieldsParsed(t *testing.T) {
	raw := validRawFields()
	raw.WebhookPort = intPtr(8080)
	raw.WebhookSecret = "s3cr3t"

	cfg, err := validate(raw)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Webhook.Port)
	assert.Equal(t, "s3cr3t", cfg.Webhook.Secret)
}

func TestValidate_WebhookPortWithoutSecretFails(t *testing.T) {
	raw := validRawFields()
	raw.WebhookPort = intPtr(8080)

	_, err := validate(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook_secret")
}

func TestValidate_WebhookPortOutOfRangeWhenSet(t *testing.T) {
	for _, v := range []int{0, -1, 65536, 100000} {
		t.Run(fmt.Sprintf("%d", v), func(t *testing.T) {
			raw := validRawFields()
			raw.WebhookPort = intPtr(v)
			raw.WebhookSecret = "s3cr3t"

			_, err := validate(raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "webhook_port")
		})
	}
}

func TestValidate_OptionalFieldsParsed(t *testing.T) {
	raw := validRawFields()
	raw.IgnoreVehicles = []string{"James", "Kühlschrank Garage"}
	raw.IgnoreLoadpoints = []string{"Werkstatt"}
	raw.Debug = true
	raw.LogFile = "/var/log/gcs-connector.log"

	cfg, err := validate(raw)
	require.NoError(t, err)

	assert.Equal(t, []string{"James", "Kühlschrank Garage"}, cfg.IgnoreVehicles)
	assert.Equal(t, []string{"Werkstatt"}, cfg.IgnoreLoadpoints)
	assert.True(t, cfg.Debug)
	assert.Equal(t, "/var/log/gcs-connector.log", cfg.LogFile)
}

func TestValidate_MissingRequiredField(t *testing.T) {
	for field, clear := range map[string]func(*rawFields){
		"api_base_url":  func(r *rawFields) { r.APIBaseURL = "" },
		"evcc_base_url": func(r *rawFields) { r.EVCCBaseURL = "" },
		"api_key":       func(r *rawFields) { r.APIKey = "" },
		"api_secret":    func(r *rawFields) { r.APISecret = "" },
		"site_name":     func(r *rawFields) { r.SiteName = "" },
	} {
		t.Run(field, func(t *testing.T) {
			raw := validRawFields()
			clear(&raw)

			_, err := validate(raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), field)
		})
	}
}

// --- FromMap: only what's specific to the .env/map source. ---

func validEnv() map[string]string {
	return map[string]string{
		"api_base_url":          "https://gcs.example.com",
		"evcc_base_url":         "http://192.168.1.50:7070",
		"api_key":               "key123",
		"api_secret":            "secret456",
		"site_name":             "Zuhause Carport",
		"sync_interval_minutes": "60",
	}
}

func TestFromMap_ValidMinimalConfig(t *testing.T) {
	cfg, err := FromMap(validEnv())
	require.NoError(t, err)
	assert.Equal(t, 60, cfg.SyncIntervalMinutes)
}

func TestFromMap_SyncIntervalDefaultsTo60WhenUnset(t *testing.T) {
	env := validEnv()
	delete(env, "sync_interval_minutes")

	cfg, err := FromMap(env)
	require.NoError(t, err)
	assert.Equal(t, 60, cfg.SyncIntervalMinutes)
}

func TestFromMap_SyncIntervalDefaultsTo60WhenEmpty(t *testing.T) {
	env := validEnv()
	env["sync_interval_minutes"] = ""

	cfg, err := FromMap(env)
	require.NoError(t, err)
	assert.Equal(t, 60, cfg.SyncIntervalMinutes)
}

func TestFromMap_SyncIntervalExplicitZeroFails(t *testing.T) {
	// Unlike FromOptionsJSON (see TestFromOptionsJSON_WebhookPortZeroStaysDisabledEvenWhenExplicit),
	// .env can distinguish "field present" from "field absent" for every
	// field, so an explicit "0" is a validation error, not a default.
	env := validEnv()
	env["sync_interval_minutes"] = "0"

	_, err := FromMap(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync_interval_minutes")
}

func TestFromMap_SyncIntervalNotANumberFails(t *testing.T) {
	env := validEnv()
	env["sync_interval_minutes"] = "not-a-number"

	_, err := FromMap(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync_interval_minutes")
}

func TestFromMap_WebhookSecretIsTrimmed(t *testing.T) {
	env := validEnv()
	env["webhook_port"] = "8080"
	env["webhook_secret"] = "  s3cr3t  \n"

	cfg, err := FromMap(env)
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t", cfg.Webhook.Secret)
}

func TestFromMap_WebhookPortExplicitZeroFails(t *testing.T) {
	// Unlike FromOptionsJSON, an explicit "0" in .env is a validation error
	// rather than a synonym for "disabled" - see rawFields.
	env := validEnv()
	env["webhook_port"] = "0"
	env["webhook_secret"] = "s3cr3t"

	_, err := FromMap(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook_port")
}

func TestFromMap_WebhookPortNotANumberFails(t *testing.T) {
	env := validEnv()
	env["webhook_port"] = "not-a-number"
	env["webhook_secret"] = "s3cr3t"

	_, err := FromMap(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook_port")
}

func TestFromMap_OptionalFieldsCoerced(t *testing.T) {
	env := validEnv()
	env["ignore_vehicles"] = "James, Kühlschrank Garage"
	env["ignore_loadpoints"] = "Werkstatt"
	env["debug"] = "true"
	env["log_file"] = "/var/log/gcs-connector.log"

	cfg, err := FromMap(env)
	require.NoError(t, err)

	assert.Equal(t, []string{"James", "Kühlschrank Garage"}, cfg.IgnoreVehicles)
	assert.Equal(t, []string{"Werkstatt"}, cfg.IgnoreLoadpoints)
	assert.True(t, cfg.Debug)
	assert.Equal(t, "/var/log/gcs-connector.log", cfg.LogFile)
}

func TestFromMap_EmptyIgnoreListsStayEmpty(t *testing.T) {
	env := validEnv()
	env["ignore_vehicles"] = ""

	cfg, err := FromMap(env)
	require.NoError(t, err)

	assert.Empty(t, cfg.IgnoreVehicles)
}

// TestFromMap_MissingRequiredField proves the wiring from a missing env key
// to validate's required-field check; the full 5-field rule is covered once,
// at the rule level, by TestValidate_MissingRequiredField.
func TestFromMap_MissingRequiredField(t *testing.T) {
	env := validEnv()
	delete(env, "site_name")

	_, err := FromMap(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "site_name")
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/.env")
	require.Error(t, err)
}

// --- FromOptionsJSON: only what's specific to the Supervisor options.json source. ---

func TestFromOptionsJSON_ValidMinimalConfig(t *testing.T) {
	path := writeOptionsJSON(t, `{
		"api_base_url": "https://gcs.example.com",
		"evcc_base_url": "http://192.168.1.50:7070",
		"api_key": "key123",
		"api_secret": "secret456",
		"site_name": "Zuhause Carport"
	}`)

	cfg, err := FromOptionsJSON(path)
	require.NoError(t, err)
	assert.Equal(t, 60, cfg.SyncIntervalMinutes, "must default like FromMap when the field is absent")
}

func TestFromOptionsJSON_OptionalFieldsCoerced(t *testing.T) {
	path := writeOptionsJSON(t, `{
		"api_base_url": "https://gcs.example.com",
		"evcc_base_url": "http://192.168.1.50:7070",
		"api_key": "key123",
		"api_secret": "secret456",
		"site_name": "Zuhause Carport",
		"ignore_vehicles": ["James", "Kühlschrank Garage"],
		"ignore_loadpoints": ["Werkstatt"],
		"debug": true,
		"log_file": "/var/log/gcs-connector.log",
		"webhook_port": 8734,
		"webhook_secret": "s3cr3t"
	}`)

	cfg, err := FromOptionsJSON(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"James", "Kühlschrank Garage"}, cfg.IgnoreVehicles)
	assert.Equal(t, []string{"Werkstatt"}, cfg.IgnoreLoadpoints)
	assert.True(t, cfg.Debug)
	assert.Equal(t, "/var/log/gcs-connector.log", cfg.LogFile)
	assert.Equal(t, 8734, cfg.Webhook.Port)
	assert.Equal(t, "s3cr3t", cfg.Webhook.Secret)
}

func TestFromOptionsJSON_SyncIntervalExplicitZeroFails(t *testing.T) {
	// sync_interval_minutes is *int in optionsJSON, so - unlike
	// webhook_port below - explicit 0 is distinguishable from absent and is
	// still a validation error.
	path := writeOptionsJSON(t, `{
		"api_base_url": "https://gcs.example.com",
		"evcc_base_url": "http://192.168.1.50:7070",
		"api_key": "key123",
		"api_secret": "secret456",
		"site_name": "Zuhause Carport",
		"sync_interval_minutes": 0
	}`)

	_, err := FromOptionsJSON(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync_interval_minutes")
}

func TestFromOptionsJSON_WebhookPortZeroStaysDisabledEvenWhenExplicit(t *testing.T) {
	// Unlike FromMap's .env parsing (where an explicitly-set "0" is
	// rejected, since presence there means the user typed something), the
	// JSON path can't distinguish an absent field from an explicit 0 - both
	// are treated as "disabled", matching FromOptionsJSON's documented
	// design.
	path := writeOptionsJSON(t, `{
		"api_base_url": "https://gcs.example.com",
		"evcc_base_url": "http://192.168.1.50:7070",
		"api_key": "key123",
		"api_secret": "secret456",
		"site_name": "Zuhause Carport",
		"webhook_port": 0
	}`)

	cfg, err := FromOptionsJSON(path)
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.Webhook.Port)
}

// TestFromOptionsJSON_MissingRequiredField proves the wiring from a missing
// JSON key to validate's required-field check; the full 5-field rule is
// covered once, at the rule level, by TestValidate_MissingRequiredField.
func TestFromOptionsJSON_MissingRequiredField(t *testing.T) {
	path := writeOptionsJSON(t, `{
		"api_base_url": "https://gcs.example.com",
		"evcc_base_url": "http://192.168.1.50:7070",
		"api_key": "key123",
		"api_secret": "secret456"
	}`)

	_, err := FromOptionsJSON(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "site_name")
}

func TestFromOptionsJSON_FileNotFound(t *testing.T) {
	_, err := FromOptionsJSON("/nonexistent/options.json")
	require.Error(t, err)
}

func TestFromOptionsJSON_InvalidJSON(t *testing.T) {
	path := writeOptionsJSON(t, `{not valid json`)
	_, err := FromOptionsJSON(path)
	require.Error(t, err)
}
