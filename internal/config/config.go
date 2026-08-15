// Package config loads and validates the connector's .env configuration.
package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds the ten .env variables documented for the connector.
type Config struct {
	APIBaseURL          string
	EVCCBaseURL         string
	APIKey              string
	APISecret           string
	SiteName            string
	SyncIntervalMinutes int
	IgnoreVehicles      []string
	IgnoreLoadpoints    []string
	Debug               bool
	LogFile             string
}

var requiredFields = []string{
	"api_base_url",
	"evcc_base_url",
	"api_key",
	"api_secret",
	"site_name",
	"sync_interval_minutes",
}

// Load reads and validates the .env file at path.
func Load(path string) (Config, error) {
	env, err := godotenv.Read(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: reading %s: %w", path, err)
	}
	return FromMap(env)
}

// FromMap validates and parses a raw key/value map (as produced by godotenv)
// into a Config.
func FromMap(env map[string]string) (Config, error) {
	for _, field := range requiredFields {
		if strings.TrimSpace(env[field]) == "" {
			return Config{}, fmt.Errorf("config: missing required field %q", field)
		}
	}

	interval, err := strconv.Atoi(env["sync_interval_minutes"])
	if err != nil || interval <= 0 {
		return Config{}, fmt.Errorf("config: sync_interval_minutes must be a positive integer, got %q", env["sync_interval_minutes"])
	}

	return Config{
		APIBaseURL:          env["api_base_url"],
		EVCCBaseURL:         env["evcc_base_url"],
		APIKey:              env["api_key"],
		APISecret:           env["api_secret"],
		SiteName:            env["site_name"],
		SyncIntervalMinutes: interval,
		IgnoreVehicles:      splitList(env["ignore_vehicles"]),
		IgnoreLoadpoints:    splitList(env["ignore_loadpoints"]),
		Debug:               strings.EqualFold(strings.TrimSpace(env["debug"]), "true"),
		LogFile:             env["log_file"],
	}, nil
}

// splitList parses a comma-separated config value into a trimmed slice,
// dropping empty entries.
func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
