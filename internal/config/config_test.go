package config

import (
	"encoding/json"
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

func TestFromMap_WebhookFieldsParsed(t *testing.T) {
	env := validEnv()
	env["webhook_port"] = "8080"
	env["webhook_secret"] = "s3cr3t"

	cfg, err := FromMap(env)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Webhook.Port)
	assert.Equal(t, "s3cr3t", cfg.Webhook.Secret)
}

func TestFromMap_WebhookSecretIsTrimmed(t *testing.T) {
	env := validEnv()
	env["webhook_port"] = "8080"
	env["webhook_secret"] = "  s3cr3t  \n"

	cfg, err := FromMap(env)
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t", cfg.Webhook.Secret)
}

func TestFromMap_WebhookPortWithoutSecretFails(t *testing.T) {
	env := validEnv()
	env["webhook_port"] = "8080"

	_, err := FromMap(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook_secret")
}

func TestFromMap_WebhookPortInvalid(t *testing.T) {
	cases := []string{"0", "-1", "not-a-number", "70000"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			env := validEnv()
			env["webhook_port"] = v
			env["webhook_secret"] = "s3cr3t"

			_, err := FromMap(env)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "webhook_port")
		})
	}
}

func TestFromMap_OptionalFieldsParsed(t *testing.T) {
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

func TestFromMap_MissingRequiredField(t *testing.T) {
	for _, field := range []string{"api_base_url", "evcc_base_url", "api_key", "api_secret", "site_name"} {
		t.Run(field, func(t *testing.T) {
			env := validEnv()
			delete(env, field)

			_, err := FromMap(env)
			require.Error(t, err)
			assert.Contains(t, err.Error(), field)
		})
	}
}

func TestFromMap_SyncIntervalMustBePositiveInteger(t *testing.T) {
	cases := []string{"0", "-5", "not-a-number"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			env := validEnv()
			env["sync_interval_minutes"] = v

			_, err := FromMap(env)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "sync_interval_minutes")
		})
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/.env")
	require.Error(t, err)
}

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

	assert.Equal(t, "https://gcs.example.com", cfg.APIBaseURL)
	assert.Equal(t, "http://192.168.1.50:7070", cfg.EVCCBaseURL)
	assert.Equal(t, "key123", cfg.APIKey)
	assert.Equal(t, "secret456", cfg.APISecret)
	assert.Equal(t, "Zuhause Carport", cfg.SiteName)
	assert.Equal(t, 60, cfg.SyncIntervalMinutes, "must default like FromMap when the field is absent")
	assert.Empty(t, cfg.IgnoreVehicles)
	assert.Empty(t, cfg.IgnoreLoadpoints)
	assert.False(t, cfg.Debug)
	assert.Equal(t, "", cfg.LogFile)
	assert.Equal(t, 0, cfg.Webhook.Port)
	assert.Equal(t, "", cfg.Webhook.Secret)
}

func TestFromOptionsJSON_IgnoreListsAreRealJSONArraysNotCommaSeparated(t *testing.T) {
	path := writeOptionsJSON(t, `{
		"api_base_url": "https://gcs.example.com",
		"evcc_base_url": "http://192.168.1.50:7070",
		"api_key": "key123",
		"api_secret": "secret456",
		"site_name": "Zuhause Carport",
		"ignore_vehicles": ["James", "Kühlschrank Garage"],
		"ignore_loadpoints": ["Werkstatt"]
	}`)

	cfg, err := FromOptionsJSON(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"James", "Kühlschrank Garage"}, cfg.IgnoreVehicles)
	assert.Equal(t, []string{"Werkstatt"}, cfg.IgnoreLoadpoints)
}

func TestFromOptionsJSON_OptionalFieldsParsed(t *testing.T) {
	path := writeOptionsJSON(t, `{
		"api_base_url": "https://gcs.example.com",
		"evcc_base_url": "http://192.168.1.50:7070",
		"api_key": "key123",
		"api_secret": "secret456",
		"site_name": "Zuhause Carport",
		"sync_interval_minutes": 30,
		"debug": true,
		"log_file": "/var/log/gcs-connector.log",
		"webhook_port": 8734,
		"webhook_secret": "s3cr3t"
	}`)

	cfg, err := FromOptionsJSON(path)
	require.NoError(t, err)
	assert.Equal(t, 30, cfg.SyncIntervalMinutes)
	assert.True(t, cfg.Debug)
	assert.Equal(t, "/var/log/gcs-connector.log", cfg.LogFile)
	assert.Equal(t, 8734, cfg.Webhook.Port)
	assert.Equal(t, "s3cr3t", cfg.Webhook.Secret)
}

func TestFromOptionsJSON_MissingRequiredField(t *testing.T) {
	base := map[string]string{
		"api_base_url":  "https://gcs.example.com",
		"evcc_base_url": "http://192.168.1.50:7070",
		"api_key":       "key123",
		"api_secret":    "secret456",
		"site_name":     "Zuhause Carport",
	}
	for _, field := range []string{"api_base_url", "evcc_base_url", "api_key", "api_secret", "site_name"} {
		t.Run(field, func(t *testing.T) {
			fields := map[string]string{}
			for k, v := range base {
				if k != field {
					fields[k] = v
				}
			}
			raw, err := json.Marshal(fields)
			require.NoError(t, err)
			path := writeOptionsJSON(t, string(raw))

			_, err = FromOptionsJSON(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), field)
		})
	}
}

func TestFromOptionsJSON_SyncIntervalMustBePositiveWhenSet(t *testing.T) {
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

func TestFromOptionsJSON_WebhookPortWithoutSecretFails(t *testing.T) {
	path := writeOptionsJSON(t, `{
		"api_base_url": "https://gcs.example.com",
		"evcc_base_url": "http://192.168.1.50:7070",
		"api_key": "key123",
		"api_secret": "secret456",
		"site_name": "Zuhause Carport",
		"webhook_port": 8734
	}`)

	_, err := FromOptionsJSON(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook_secret")
}

func TestFromOptionsJSON_WebhookPortOutOfRangeFails(t *testing.T) {
	cases := []int{-1, 65536, 100000}
	for _, port := range cases {
		t.Run(fmt.Sprintf("%d", port), func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{
				"api_base_url":   "https://gcs.example.com",
				"evcc_base_url":  "http://192.168.1.50:7070",
				"api_key":        "key123",
				"api_secret":     "secret456",
				"site_name":      "Zuhause Carport",
				"webhook_port":   port,
				"webhook_secret": "s3cr3t",
			})
			require.NoError(t, err)
			path := writeOptionsJSON(t, string(raw))

			_, err = FromOptionsJSON(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "webhook_port")
		})
	}
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

func TestFromOptionsJSON_FileNotFound(t *testing.T) {
	_, err := FromOptionsJSON("/nonexistent/options.json")
	require.Error(t, err)
}

func TestFromOptionsJSON_InvalidJSON(t *testing.T) {
	path := writeOptionsJSON(t, `{not valid json`)
	_, err := FromOptionsJSON(path)
	require.Error(t, err)
}
