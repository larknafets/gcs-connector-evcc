package cli

import "testing"

func TestResolveConfigPath_FlagWins(t *testing.T) {
	got := resolveConfigPath("/flag/.env", func(string) string { return "/env/.env" })
	if got != "/flag/.env" {
		t.Errorf("got %q, want /flag/.env", got)
	}
}

func TestResolveConfigPath_FallsBackToEnvVar(t *testing.T) {
	got := resolveConfigPath("", func(key string) string {
		if key == "GCS_CONNECTOR_CONFIG" {
			return "/env/.env"
		}
		return ""
	})
	if got != "/env/.env" {
		t.Errorf("got %q, want /env/.env", got)
	}
}

func TestResolveConfigPath_DefaultsToDotEnvInCwd(t *testing.T) {
	got := resolveConfigPath("", func(string) string { return "" })
	if got != ".env" {
		t.Errorf("got %q, want .env", got)
	}
}
