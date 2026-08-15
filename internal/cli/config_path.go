package cli

// resolveConfigPath applies the documented precedence for locating the
// connector's .env file: --config flag, then GCS_CONNECTOR_CONFIG, then the
// default of ".env" in the working directory.
func resolveConfigPath(flagValue string, getenv func(string) string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := getenv("GCS_CONNECTOR_CONFIG"); v != "" {
		return v
	}
	return ".env"
}
