package gcs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func float64Ptr(v float64) *float64 { return &v }

func samplePayload() ChargePayload {
	return ChargePayload{
		ExternalLoadpointName: "Garage",
		ExternalSessionID:     "971",
		StartAt:               time.Date(2026, 8, 14, 17, 12, 33, 0, time.UTC),
		EndAt:                 time.Date(2026, 8, 15, 8, 0, 3, 0, time.UTC),
		ChargedEnergyWh:       7263,
		GreenPercentage:       float64Ptr(99.94),
		VehicleName:           "James",
		SiteName:              "Zuhause Carport",
	}
}

func fastRetryClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c := NewClient(baseURL, "key123", "secret456", nil)
	c.HTTP.RetryMax = 1
	c.HTTP.RetryWaitMin = time.Millisecond
	c.HTTP.RetryWaitMax = 2 * time.Millisecond
	return c
}

func TestNewClient_WiresLoggerIntoRetryClient(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := NewClient("http://example.invalid", "key", "secret", logger)
	assert.Same(t, logger, c.HTTP.Logger)
}

func TestNewClient_NilLoggerDoesNotPanicOnUse(t *testing.T) {
	// A naive `retryClient.Logger = logger` would store a non-nil interface
	// wrapping a nil *slog.Logger, which retryablehttp would later try to
	// call methods on. Exercise a real (failing, to force a log attempt)
	// request to prove that doesn't happen.
	c := NewClient("http://127.0.0.1:1", "key", "secret", nil)
	c.HTTP.RetryMax = 0

	assert.NotPanics(t, func() {
		_, _ = c.PostCharge(context.Background(), samplePayload())
	})
}

func TestPostCharge_SuccessSendsHeadersAndPayload(t *testing.T) {
	var gotPath, gotKey, gotSecret string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-API-Key")
		gotSecret = r.Header.Get("X-API-Secret")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"created"}`))
	}))
	defer server.Close()

	client := fastRetryClient(t, server.URL)
	duplicate, err := client.PostCharge(context.Background(), samplePayload())
	require.NoError(t, err)
	assert.False(t, duplicate)

	assert.Equal(t, "/api/v1/connector/charges", gotPath)
	assert.Equal(t, "key123", gotKey)
	assert.Equal(t, "secret456", gotSecret)
	assert.Equal(t, "Garage", gotBody["external_loadpoint_name"])
	assert.Equal(t, "971", gotBody["external_session_id"])
	assert.Equal(t, float64(7263), gotBody["charged_energy_wh"])
	assert.Equal(t, "Zuhause Carport", gotBody["site_name"])
	assert.InDelta(t, 99.94, gotBody["green_percentage"], 1e-9)
	assert.NotContains(t, gotBody, "clean_percentage")
	assert.NotContains(t, gotBody, "price")
	assert.NotContains(t, gotBody, "pricePerKWh")
	assert.NotContains(t, gotBody, "co2PerKWh")
}

func TestPostCharge_EmptyVehicleNameIsSentNotOmitted(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"created"}`))
	}))
	defer server.Close()

	payload := samplePayload()
	payload.VehicleName = ""

	client := fastRetryClient(t, server.URL)
	_, err := client.PostCharge(context.Background(), payload)
	require.NoError(t, err)

	assert.Contains(t, gotBody, "vehicle_name")
	assert.Equal(t, "", gotBody["vehicle_name"])
}

func TestPostCharge_DuplicateSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"duplicate_skipped"}`))
	}))
	defer server.Close()

	client := fastRetryClient(t, server.URL)
	duplicate, err := client.PostCharge(context.Background(), samplePayload())
	require.NoError(t, err)
	assert.True(t, duplicate)
}

func TestPostCharge_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := fastRetryClient(t, server.URL)
	_, err := client.PostCharge(context.Background(), samplePayload())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnauthorized))
}

func TestPostCharge_InvalidPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"external_loadpoint_name missing"}`))
	}))
	defer server.Close()

	client := fastRetryClient(t, server.URL)
	_, err := client.PostCharge(context.Background(), samplePayload())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidPayload))
}

func TestPostCharge_RateLimitedAfterRetriesExhausted(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := fastRetryClient(t, server.URL)
	_, err := client.PostCharge(context.Background(), samplePayload())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRateLimited))
	assert.GreaterOrEqual(t, attempts, 2)
}

func TestGetChargesSince_ParsesExistingCharges(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{"external_session_id":"970","start_at":"2026-08-14T06:00:32Z","end_at":"2026-08-14T06:43:23Z","charged_energy_wh":2555}
		]`))
	}))
	defer server.Close()

	client := fastRetryClient(t, server.URL)
	since := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	charges, err := client.GetChargesSince(context.Background(), since)
	require.NoError(t, err)
	require.Len(t, charges, 1)
	assert.Equal(t, "970", charges[0].ExternalSessionID)
	assert.Equal(t, "since=2026-08-01T00%3A00%3A00Z", gotQuery)
}
