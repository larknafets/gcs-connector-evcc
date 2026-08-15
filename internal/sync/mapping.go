// Package sync maps evcc sessions onto GCS charge payloads, filters which
// sessions are eligible to send, and orchestrates a full sync cycle.
package sync

import (
	"fmt"
	"math"
	"strconv"

	"github.com/larknafets/gc-connector-evcc/internal/evcc"
	"github.com/larknafets/gc-connector-evcc/internal/gcs"
)

// ToChargePayload maps a finished evcc session onto the GCS Connector-API
// payload. It never reads evcc's price/pricePerKWh/co2PerKWh fields, because
// evcc.Session has no fields for them in the first place.
func ToChargePayload(s evcc.Session, siteName string) (gcs.ChargePayload, error) {
	if s.Finished == nil {
		return gcs.ChargePayload{}, fmt.Errorf("sync: session %d has no finished timestamp", s.ID)
	}

	return gcs.ChargePayload{
		ExternalLoadpointName: s.Loadpoint,
		ExternalSessionID:     strconv.Itoa(s.ID),
		StartAt:               s.Created,
		EndAt:                 *s.Finished,
		ChargedEnergyWh:       int(math.Round(s.ChargedEnergy * 1000)),
		CleanPercentage:       s.SolarPercentage,
		VehicleName:           s.Vehicle,
		SiteName:              siteName,
	}, nil
}
