// Command gcs-connector syncs completed evcc charging sessions to the GCS
// Connector-API. See the repo README and docs/agents for details.
package main

import (
	"fmt"
	"os"

	"github.com/larknafets/gcs-connector-evcc/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}
