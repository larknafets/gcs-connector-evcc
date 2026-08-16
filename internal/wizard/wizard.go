// Package wizard implements `gcs-connector init`. The interactive huh-based
// form (RunInit) is intentionally thin and untested; all substantial logic
// - default values, existing-config prefill, .env rendering, overwrite
// confirmation, connectivity checks - lives in small, directly testable
// functions.
package wizard

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/larknafets/gcs-connector-evcc/internal/config"
)

// Answers holds the raw string form of the .env variables, as collected
// from wizard prompts (before config.FromMap parses/validates them).
type Answers struct {
	APIBaseURL          string
	EVCCBaseURL         string
	APIKey              string
	APISecret           string
	SiteName            string
	SyncIntervalMinutes string
	IgnoreVehicles      string
	IgnoreLoadpoints    string
	Debug               string
	LogFile             string
	WebhookPort         string
	WebhookSecret       string
}

// DefaultAnswers returns Answers with sensible defaults pre-filled for the
// fields the wizard lets a user skip with Enter: debug, sync_interval_minutes,
// log_file, the two ignore lists, and the two webhook fields. Required
// fields with no sensible default are left empty.
func DefaultAnswers() Answers {
	return Answers{
		Debug:               "false",
		SyncIntervalMinutes: fmt.Sprintf("%d", config.DefaultSyncIntervalMinutes),
	}
}

// AnswersFromConfig converts an already-loaded Config back into Answers, so
// `gcs-connector init` run against an existing .env can prefill the current
// values instead of starting blank.
func AnswersFromConfig(cfg config.Config) Answers {
	debug := "false"
	if cfg.Debug {
		debug = "true"
	}
	return Answers{
		APIBaseURL:          cfg.APIBaseURL,
		EVCCBaseURL:         cfg.EVCCBaseURL,
		APIKey:              cfg.APIKey,
		APISecret:           cfg.APISecret,
		SiteName:            cfg.SiteName,
		SyncIntervalMinutes: fmt.Sprintf("%d", cfg.SyncIntervalMinutes),
		IgnoreVehicles:      strings.Join(cfg.IgnoreVehicles, ", "),
		IgnoreLoadpoints:    strings.Join(cfg.IgnoreLoadpoints, ", "),
		Debug:               debug,
		LogFile:             cfg.LogFile,
		WebhookPort:         webhookPortString(cfg.Webhook.Port),
		WebhookSecret:       cfg.Webhook.Secret,
	}
}

// webhookPortString renders a Config.Webhook.Port back into the wizard's
// string form: 0 (disabled) becomes an empty field, matching how an unset
// webhook_port is represented in the .env file.
func webhookPortString(port int) string {
	if port == 0 {
		return ""
	}
	return fmt.Sprintf("%d", port)
}

// ConfirmOverwrite decides whether writing should proceed: always if no
// file exists yet, otherwise only if the user confirmed the overwrite.
func ConfirmOverwrite(pathExists bool, userConfirmed bool) bool {
	if !pathExists {
		return true
	}
	return userConfirmed
}

var envFieldOrder = []struct {
	key   string
	value func(Answers) string
}{
	{"api_base_url", func(a Answers) string { return a.APIBaseURL }},
	{"evcc_base_url", func(a Answers) string { return a.EVCCBaseURL }},
	{"api_key", func(a Answers) string { return a.APIKey }},
	{"api_secret", func(a Answers) string { return a.APISecret }},
	{"site_name", func(a Answers) string { return a.SiteName }},
	{"sync_interval_minutes", func(a Answers) string { return a.SyncIntervalMinutes }},
	{"ignore_vehicles", func(a Answers) string { return a.IgnoreVehicles }},
	{"ignore_loadpoints", func(a Answers) string { return a.IgnoreLoadpoints }},
	{"debug", func(a Answers) string { return a.Debug }},
	{"log_file", func(a Answers) string { return a.LogFile }},
	{"webhook_port", func(a Answers) string { return a.WebhookPort }},
	{"webhook_secret", func(a Answers) string { return a.WebhookSecret }},
}

// WriteEnvFile renders answers as a .env file at path, in the documented
// variable order, and writes/overwrites it.
func WriteEnvFile(path string, answers Answers) error {
	var b strings.Builder
	for _, field := range envFieldOrder {
		value := strings.ReplaceAll(field.value(answers), `\`, `\\`)
		value = strings.ReplaceAll(value, `"`, `\"`)
		fmt.Fprintf(&b, "%s=\"%s\"\n", field.key, value)
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("wizard: writing %s: %w", path, err)
	}
	return nil
}

// CheckReachable performs a lightweight reachability check against baseURL:
// success means "something answered", not that the response was a 2xx - the
// wizard only cares whether the host is up, not whether the endpoint it hit
// is meaningful.
func CheckReachable(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return fmt.Errorf("wizard: building request: %w", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("wizard: %s not reachable: %w", baseURL, err)
	}
	defer resp.Body.Close()
	return nil
}
