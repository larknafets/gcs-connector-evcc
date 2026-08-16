// Package config loads and validates the connector's .env configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
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
	if err := requireWebhookSecretIfPortSet(webhookPort, webhookSecret); err != nil {
		return Config{}, err
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

// optionsJSON is the shape of a Home Assistant Supervisor add-on's
// /data/options.json - the Supervisor's only way of delivering config to an
// add-on container (never a .env file, never per-field environment
// variables). Field names match the .env variable names 1:1. Unlike .env,
// ignore_vehicles/ignore_loadpoints arrive as real JSON arrays rather than a
// comma-separated string.
type optionsJSON struct {
	APIBaseURL          string   `json:"api_base_url"`
	EVCCBaseURL         string   `json:"evcc_base_url"`
	APIKey              string   `json:"api_key"`
	APISecret           string   `json:"api_secret"`
	SiteName            string   `json:"site_name"`
	SyncIntervalMinutes *int     `json:"sync_interval_minutes"`
	IgnoreVehicles      []string `json:"ignore_vehicles"`
	IgnoreLoadpoints    []string `json:"ignore_loadpoints"`
	Debug               bool     `json:"debug"`
	LogFile             string   `json:"log_file"`
	WebhookPort         int      `json:"webhook_port"`
	WebhookSecret       string   `json:"webhook_secret"`
}

// FromOptionsJSON reads and validates a Home Assistant Supervisor
// options.json file into a Config, applying the same validation rules as
// FromMap/Load. A JSON field absent from options.json unmarshals to Go's
// zero value (0 for webhook_port), which already means "disabled" - the
// same default FromMap uses - so unlike FromMap's .env parsing there is no
// separate "field present but blank" case to detect here. The port range
// is still checked explicitly: the add-on's options schema constrains it
// too, but that's an external file this package can't see or trust as the
// only guard against a malformed or hand-edited options.json.
func FromOptionsJSON(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var opts optionsJSON
	if err := json.Unmarshal(raw, &opts); err != nil {
		return Config{}, fmt.Errorf("config: parsing %s: %w", path, err)
	}

	for _, field := range []struct {
		name  string
		value string
	}{
		{"api_base_url", opts.APIBaseURL},
		{"evcc_base_url", opts.EVCCBaseURL},
		{"api_key", opts.APIKey},
		{"api_secret", opts.APISecret},
		{"site_name", opts.SiteName},
	} {
		if strings.TrimSpace(field.value) == "" {
			return Config{}, fmt.Errorf("config: missing required field %q", field.name)
		}
	}

	interval := DefaultSyncIntervalMinutes
	if opts.SyncIntervalMinutes != nil {
		if *opts.SyncIntervalMinutes <= 0 {
			return Config{}, fmt.Errorf("config: sync_interval_minutes must be a positive integer, got %d", *opts.SyncIntervalMinutes)
		}
		interval = *opts.SyncIntervalMinutes
	}

	if opts.WebhookPort != 0 && (opts.WebhookPort < 1 || opts.WebhookPort > 65535) {
		return Config{}, fmt.Errorf("config: webhook_port must be a valid port number, got %d", opts.WebhookPort)
	}
	webhookSecret := strings.TrimSpace(opts.WebhookSecret)
	if err := requireWebhookSecretIfPortSet(opts.WebhookPort, webhookSecret); err != nil {
		return Config{}, err
	}

	return Config{
		APIBaseURL:          opts.APIBaseURL,
		EVCCBaseURL:         opts.EVCCBaseURL,
		APIKey:              opts.APIKey,
		APISecret:           opts.APISecret,
		SiteName:            opts.SiteName,
		SyncIntervalMinutes: interval,
		IgnoreVehicles:      opts.IgnoreVehicles,
		IgnoreLoadpoints:    opts.IgnoreLoadpoints,
		Debug:               opts.Debug,
		LogFile:             opts.LogFile,
		Webhook:             WebhookConfig{Port: opts.WebhookPort, Secret: webhookSecret},
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
// requireWebhookSecretIfPortSet enforces the one webhook validation rule
// that's identical (same condition, same message) across both FromMap and
// FromOptionsJSON, so a future change to it only needs to happen once. port
// and trimmedSecret must already be resolved to their final values - it
// does no parsing or trimming itself.
func requireWebhookSecretIfPortSet(port int, trimmedSecret string) error {
	if port != 0 && trimmedSecret == "" {
		return fmt.Errorf("config: webhook_secret is required when webhook_port is set")
	}
	return nil
}

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
