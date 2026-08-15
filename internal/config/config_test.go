package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	for _, field := range []string{"api_base_url", "evcc_base_url", "api_key", "api_secret", "site_name", "sync_interval_minutes"} {
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
	cases := []string{"0", "-5", "not-a-number", ""}
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
