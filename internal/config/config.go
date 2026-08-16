// Package config loads and validates the connector's .env configuration.
package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// DefaultSyncIntervalMinutes is used when sync_interval_minutes is left
// unset in the .env file.
const DefaultSyncIntervalMinutes = 60

// WebhookConfig groups the fields needed to run the optional sync-now
// webhook listener. Port == 0 means the listener is disabled (the default).
type WebhookConfig struct {
	Port   int
	Secret string
}

// Config holds the .env variables documented for the connector.
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
	Webhook             WebhookConfig
}

var requiredFields = []string{
	"api_base_url",
	"evcc_base_url",
	"api_key",
	"api_secret",
	"site_name",
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

	interval := DefaultSyncIntervalMinutes
	if v, present, convErr := parseOptionalInt(env["sync_interval_minutes"]); present {
		if convErr != nil || v <= 0 {
			return Config{}, fmt.Errorf("config: sync_interval_minutes must be a positive integer, got %q", env["sync_interval_minutes"])
		}
		interval = v
	}

	webhookPort := 0
	if v, present, convErr := parseOptionalInt(env["webhook_port"]); present {
		if convErr != nil || v <= 0 || v > 65535 {
			return Config{}, fmt.Errorf("config: webhook_port must be a valid port number, got %q", env["webhook_port"])
		}
		webhookPort = v
	}
	webhookSecret := strings.TrimSpace(env["webhook_secret"])
	if webhookPort != 0 && webhookSecret == "" {
		return Config{}, fmt.Errorf("config: webhook_secret is required when webhook_port is set")
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
		Webhook:             WebhookConfig{Port: webhookPort, Secret: webhookSecret},
	}, nil
}

// parseOptionalInt parses an optional integer .env value: present reports
// whether raw was non-blank at all, so callers can tell "field left empty,
// use my default" apart from "field set but not a valid integer" (err) -
// both sync_interval_minutes and webhook_port are optional but need
// different validation ranges and error messages, so the numeric-vs-blank
// split is all this helper shares between them.
func parseOptionalInt(raw string) (v int, present bool, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false, nil
	}
	v, err = strconv.Atoi(trimmed)
	return v, true, err
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
