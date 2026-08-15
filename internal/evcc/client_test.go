package evcc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchSessions_ParsesResponseAndQueryParams(t *testing.T) {
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"id": 971,
				"created": "2026-08-14T17:12:33.771023743+02:00",
				"finished": "2026-08-15T08:00:03.081852626+02:00",
				"loadpoint": "Garage",
				"vehicle": "James",
				"chargedEnergy": 7.262842655181885,
				"solarPercentage": 99.94476571059126,
				"price": 0.005961066957002568,
				"pricePerKWh": 0.0008207622331938407
			},
			{
				"id": 972,
				"created": "2026-08-15T09:00:00+02:00",
				"finished": null,
				"loadpoint": "Garage",
				"vehicle": "James",
				"chargedEnergy": 1.5,
				"solarPercentage": 50.0
			}
		]`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	sessions, err := client.FetchSessions(context.Background(), 8, 2026)
	require.NoError(t, err)

	assert.Equal(t, "/api/sessions", gotPath)
	assert.Equal(t, "month=8&year=2026", gotQuery)

	require.Len(t, sessions, 2)

	first := sessions[0]
	assert.Equal(t, 971, first.ID)
	assert.Equal(t, "Garage", first.Loadpoint)
	assert.Equal(t, "James", first.Vehicle)
	assert.InDelta(t, 7.262842655181885, first.ChargedEnergy, 1e-9)
	require.NotNil(t, first.SolarPercentage)
	assert.InDelta(t, 99.94476571059126, *first.SolarPercentage, 1e-9)
	require.NotNil(t, first.Finished)
	assert.True(t, first.Finished.Equal(time.Date(2026, 8, 15, 8, 0, 3, 81852626, first.Finished.Location())))

	second := sessions[1]
	assert.Nil(t, second.Finished)
}

func TestFetchSessions_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.FetchSessions(context.Background(), 8, 2026)
	require.Error(t, err)
}

func TestFetchSessions_Unreachable(t *testing.T) {
	client := NewClient("http://127.0.0.1:1")
	_, err := client.FetchSessions(context.Background(), 8, 2026)
	require.Error(t, err)
}
