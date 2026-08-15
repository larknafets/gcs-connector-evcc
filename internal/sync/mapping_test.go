package sync

import (
	"testing"
	"time"

	"github.com/larknafets/gc-connector-evcc/internal/evcc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToChargePayload_MapsFieldsAndRoundsWh(t *testing.T) {
	finished := time.Date(2026, 8, 15, 8, 0, 3, 0, time.UTC)
	created := time.Date(2026, 8, 14, 17, 12, 33, 0, time.UTC)
	solar := 99.94476571059126

	session := evcc.Session{
		ID:              971,
		Created:         created,
		Finished:        &finished,
		Loadpoint:       "Garage",
		Vehicle:         "James",
		ChargedEnergy:   7.262842655181885,
		SolarPercentage: &solar,
	}

	payload, err := ToChargePayload(session, "Zuhause Carport")
	require.NoError(t, err)

	assert.Equal(t, "Garage", payload.ExternalLoadpointName)
	assert.Equal(t, "971", payload.ExternalSessionID)
	assert.Equal(t, created, payload.StartAt)
	assert.Equal(t, finished, payload.EndAt)
	assert.Equal(t, 7263, payload.ChargedEnergyWh) // 7.262842655... kWh rounds to 7263 Wh
	require.NotNil(t, payload.CleanPercentage)
	assert.InDelta(t, solar, *payload.CleanPercentage, 1e-9)
	assert.Equal(t, "James", payload.VehicleName)
	assert.Equal(t, "Zuhause Carport", payload.SiteName)
}

func TestToChargePayload_MissingSolarPercentageOmitted(t *testing.T) {
	finished := time.Date(2026, 8, 15, 8, 0, 3, 0, time.UTC)
	session := evcc.Session{
		ID:            971,
		Created:       finished.Add(-time.Hour),
		Finished:      &finished,
		Loadpoint:     "Garage",
		Vehicle:       "James",
		ChargedEnergy: 1.0,
	}

	payload, err := ToChargePayload(session, "Zuhause Carport")
	require.NoError(t, err)
	assert.Nil(t, payload.CleanPercentage)
}

func TestToChargePayload_RequiresFinishedSession(t *testing.T) {
	session := evcc.Session{
		ID:            972,
		Created:       time.Now(),
		Finished:      nil,
		ChargedEnergy: 1.0,
	}

	_, err := ToChargePayload(session, "Zuhause Carport")
	require.Error(t, err)
}

func TestToChargePayload_RoundsHalfUp(t *testing.T) {
	finished := time.Now()
	session := evcc.Session{
		ID:            1,
		Created:       finished.Add(-time.Hour),
		Finished:      &finished,
		ChargedEnergy: 0.0005, // 0.5 Wh -> rounds to 1 Wh (round half away from zero)
	}

	payload, err := ToChargePayload(session, "site")
	require.NoError(t, err)
	assert.Equal(t, 1, payload.ChargedEnergyWh)
}
