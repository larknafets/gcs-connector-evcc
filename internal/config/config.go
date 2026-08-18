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

// rawFields is the intermediate representation both FromMap and
// FromOptionsJSON produce before handing off to validate, which owns every
// rule shared between the two sources. Each adapter does only its own
// format-specific coercion (env-string parsing vs. JSON typing); everything
// here is already the right Go type. SyncIntervalMinutes and WebhookPort are
// pointers so validate can tell "explicitly set" apart from "left unset" -
// each adapter decides for itself what "explicitly set" means for its own
// source (see the doc comments on FromMap and FromOptionsJSON).
type rawFields struct {
	APIBaseURL          string
	EVCCBaseURL         string
	APIKey              string
	APISecret           string
	SiteName            string
	SyncIntervalMinutes *int
	WebhookPort         *int
	WebhookSecret       string
	IgnoreVehicles      []string
	IgnoreLoadpoints    []string
	Debug               bool
	LogFile             string
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
// into a Config. A field counts as "explicitly set" whenever its raw string
// is non-blank - so an explicit "0" for sync_interval_minutes or
// webhook_port is a validation error, not silently treated as unset.
func FromMap(env map[string]string) (Config, error) {
	raw := rawFields{
		APIBaseURL:       env["api_base_url"],
		EVCCBaseURL:      env["evcc_base_url"],
		APIKey:           env["api_key"],
		APISecret:        env["api_secret"],
		SiteName:         env["site_name"],
		IgnoreVehicles:   splitList(env["ignore_vehicles"]),
		IgnoreLoadpoints: splitList(env["ignore_loadpoints"]),
		Debug:            strings.EqualFold(strings.TrimSpace(env["debug"]), "true"),
		LogFile:          env["log_file"],
		WebhookSecret:    strings.TrimSpace(env["webhook_secret"]),
	}

	if v, present, convErr := parseOptionalInt(env["sync_interval_minutes"]); present {
		if convErr != nil {
			return Config{}, fmt.Errorf("config: sync_interval_minutes must be a positive integer, got %q", env["sync_interval_minutes"])
		}
		raw.SyncIntervalMinutes = &v
	}

	if v, present, convErr := parseOptionalInt(env["webhook_port"]); present {
		if convErr != nil {
			return Config{}, fmt.Errorf("config: webhook_port must be a valid port number, got %q", env["webhook_port"])
		}
		raw.WebhookPort = &v
	}

	return validate(raw)
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
// FromMap/Load. Unlike FromMap's .env parsing, webhook_port has no separate
// "explicitly set" case to preserve here: a JSON field absent from
// options.json unmarshals to Go's zero value (0), which already means
// "disabled" - the same default FromMap uses - so an explicit 0 in
// options.json is indistinguishable from an absent field and is treated the
// same way (see rawFields). sync_interval_minutes doesn't have this
// limitation because optionsJSON declares it as *int.
func FromOptionsJSON(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var opts optionsJSON
	if err := json.Unmarshal(data, &opts); err != nil {
		return Config{}, fmt.Errorf("config: parsing %s: %w", path, err)
	}

	raw := rawFields{
		APIBaseURL:          opts.APIBaseURL,
		EVCCBaseURL:         opts.EVCCBaseURL,
		APIKey:              opts.APIKey,
		APISecret:           opts.APISecret,
		SiteName:            opts.SiteName,
		SyncIntervalMinutes: opts.SyncIntervalMinutes,
		IgnoreVehicles:      opts.IgnoreVehicles,
		IgnoreLoadpoints:    opts.IgnoreLoadpoints,
		Debug:               opts.Debug,
		LogFile:             opts.LogFile,
		WebhookSecret:       strings.TrimSpace(opts.WebhookSecret),
	}
	if opts.WebhookPort != 0 {
		port := opts.WebhookPort
		raw.WebhookPort = &port
	}

	return validate(raw)
}

// validate applies every config rule shared by FromMap and FromOptionsJSON:
// required fields, the sync_interval_minutes and webhook_port ranges, and
// the webhook_secret-requires-webhook_port rule. A nil SyncIntervalMinutes
// or WebhookPort defaults to DefaultSyncIntervalMinutes / disabled without
// triggering its range check; a non-nil pointer is always range-checked,
// even if it points at zero or a negative value.
func validate(raw rawFields) (Config, error) {
	for _, field := range []struct {
		name  string
		value string
	}{
		{"api_base_url", raw.APIBaseURL},
		{"evcc_base_url", raw.EVCCBaseURL},
		{"api_key", raw.APIKey},
		{"api_secret", raw.APISecret},
		{"site_name", raw.SiteName},
	} {
		if strings.TrimSpace(field.value) == "" {
			return Config{}, fmt.Errorf("config: missing required field %q", field.name)
		}
	}

	interval := DefaultSyncIntervalMinutes
	if raw.SyncIntervalMinutes != nil {
		if *raw.SyncIntervalMinutes <= 0 {
			return Config{}, fmt.Errorf("config: sync_interval_minutes must be a positive integer, got %d", *raw.SyncIntervalMinutes)
		}
		interval = *raw.SyncIntervalMinutes
	}

	webhookPort := 0
	if raw.WebhookPort != nil {
		if *raw.WebhookPort < 1 || *raw.WebhookPort > 65535 {
			return Config{}, fmt.Errorf("config: webhook_port must be a valid port number, got %d", *raw.WebhookPort)
		}
		webhookPort = *raw.WebhookPort
	}
	if err := requireWebhookSecretIfPortSet(webhookPort, raw.WebhookSecret); err != nil {
		return Config{}, err
	}

	return Config{
		APIBaseURL:          raw.APIBaseURL,
		EVCCBaseURL:         raw.EVCCBaseURL,
		APIKey:              raw.APIKey,
		APISecret:           raw.APISecret,
		SiteName:            raw.SiteName,
		SyncIntervalMinutes: interval,
		IgnoreVehicles:      raw.IgnoreVehicles,
		IgnoreLoadpoints:    raw.IgnoreLoadpoints,
		Debug:               raw.Debug,
		LogFile:             raw.LogFile,
		Webhook:             WebhookConfig{Port: webhookPort, Secret: raw.WebhookSecret},
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

// requireWebhookSecretIfPortSet enforces the one webhook validation rule
// that isn't a simple range check. port and trimmedSecret must already be
// resolved to their final values - it does no parsing or trimming itself.
func requireWebhookSecretIfPortSet(port int, trimmedSecret string) error {
	if port != 0 && trimmedSecret == "" {
		return fmt.Errorf("config: webhook_secret is required when webhook_port is set")
	}
	return nil
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
