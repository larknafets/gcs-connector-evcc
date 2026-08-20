// Package gcs talks to the GCS Connector-API (POST/GET
// /api/v1/connector/charges), authenticating via the X-API-Key/X-API-Secret
// header pair documented in gcs-platform's 03-api-endpunkte.md, Abschnitt A.
package gcs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// Sentinel errors PostCharge can wrap, so callers can classify a failure
// with errors.Is without depending on HTTP status codes directly.
var (
	// ErrUnauthorized means the configured API key/secret was rejected (401).
	// Fatal: retrying with the same credentials will not help.
	ErrUnauthorized = errors.New("gcs: unauthorized (invalid api key/secret)")
	// ErrInvalidPayload means GCS rejected the payload as malformed (422).
	// Points at a connector mapping bug; safe to skip this one session.
	ErrInvalidPayload = errors.New("gcs: invalid payload (422)")
	// ErrRateLimited means the rate limit (429) was still in effect after
	// go-retryablehttp exhausted its retries.
	ErrRateLimited = errors.New("gcs: rate limited (429), retries exhausted")
)

// ChargePayload is the body of a single POST /api/v1/connector/charges
// request. It deliberately has no field for price/pricePerKWh/co2PerKWh:
// those must never leave the evcc client, so there is nowhere here to put
// them even by mistake.
type ChargePayload struct {
	ExternalLoadpointName string    `json:"external_loadpoint_name"`
	ExternalSessionID     string    `json:"external_session_id"`
	StartAt               time.Time `json:"start_at"`
	EndAt                 time.Time `json:"end_at"`
	ChargedEnergyWh       int       `json:"charged_energy_wh"`
	GreenPercentage       *float64  `json:"green_percentage,omitempty"`
	VehicleName           string    `json:"vehicle_name"`
}

// ExistingCharge is a record returned by GET /api/v1/connector/charges?since=,
// used by --dry-run to show which sessions the server already has.
type ExistingCharge struct {
	ExternalSessionID string    `json:"external_session_id"`
	StartAt           time.Time `json:"start_at"`
	EndAt             time.Time `json:"end_at"`
	ChargedEnergyWh   int       `json:"charged_energy_wh"`
}

type postChargeResponse struct {
	Status string `json:"status"`
}

// Client talks to a single GCS instance's Connector-API.
type Client struct {
	BaseURL   string
	APIKey    string
	APISecret string
	HTTP      *retryablehttp.Client
}

// NewClient returns a Client for the GCS instance at baseURL. logger may be
// nil, in which case retry attempts go unlogged; *slog.Logger satisfies
// retryablehttp.LeveledLogger, so a debug-level logger also surfaces
// retryablehttp's own per-attempt request logs, including retries.
func NewClient(baseURL, apiKey, apiSecret string, logger *slog.Logger) *Client {
	retryClient := retryablehttp.NewClient()
	// retryClient.Logger is interface{} and retryablehttp.NewClient defaults
	// it to a log.Logger writing to stderr. Assigning a nil *slog.Logger to
	// it directly would produce a non-nil interface wrapping a nil pointer
	// (retryablehttp's own `c.Logger == nil` check wouldn't catch that), so
	// guard explicitly and fall back to a true nil interface - "don't log" -
	// rather than that default.
	if logger != nil {
		retryClient.Logger = logger
	} else {
		retryClient.Logger = nil
	}
	retryClient.RetryMax = 4
	retryClient.RetryWaitMin = 1 * time.Second
	retryClient.RetryWaitMax = 30 * time.Second
	// 401/422 are permanent for this request; DefaultRetryPolicy already
	// only retries on 429/5xx/network errors, not other 4xx statuses.
	//
	// ErrorHandler runs once retries are exhausted, while the last response
	// is still available (the default behavior discards it) - use that to
	// turn "429, out of retries" into ErrRateLimited specifically.
	retryClient.ErrorHandler = func(resp *http.Response, err error, numTries int) (*http.Response, error) {
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			return nil, ErrRateLimited
		}
		if err != nil {
			return nil, fmt.Errorf("gcs: request failed after %d attempts: %w", numTries, err)
		}
		return nil, fmt.Errorf("gcs: request failed after %d attempts", numTries)
	}

	return &Client{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		APISecret: apiSecret,
		HTTP:      retryClient,
	}
}

// PostCharge sends a single charge record. duplicateSkipped reports whether
// GCS recognized it as an already-known duplicate (still a success).
func (c *Client) PostCharge(ctx context.Context, payload ChargePayload) (duplicateSkipped bool, err error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("gcs: encoding payload: %w", err)
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/connector/charges", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("gcs: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeaders(req.Request)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return false, ErrUnauthorized
	case http.StatusUnprocessableEntity:
		return false, ErrInvalidPayload
	case http.StatusTooManyRequests:
		return false, ErrRateLimited
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("gcs: unexpected status %d posting charge", resp.StatusCode)
	}

	var parsed postChargeResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return false, fmt.Errorf("gcs: decoding response: %w", err)
	}
	return parsed.Status == "duplicate_skipped", nil
}

// GetChargesSince returns the charges GCS already has for this host since the
// given timestamp, used by --dry-run to preview what a real sync would skip.
func (c *Client) GetChargesSince(ctx context.Context, since time.Time) ([]ExistingCharge, error) {
	u := c.BaseURL + "/api/v1/connector/charges?" + url.Values{
		"since": {since.UTC().Format(time.RFC3339)},
	}.Encode()

	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("gcs: building request: %w", err)
	}
	c.setAuthHeaders(req.Request)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gcs: unexpected status %d fetching existing charges", resp.StatusCode)
	}

	var charges []ExistingCharge
	if err := json.NewDecoder(resp.Body).Decode(&charges); err != nil {
		return nil, fmt.Errorf("gcs: decoding response: %w", err)
	}
	return charges, nil
}

func (c *Client) setAuthHeaders(req *http.Request) {
	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("X-API-Secret", c.APISecret)
}
