// Package evcc reads charging session data from a local evcc instance's
// unauthenticated HTTP API (GET /api/sessions).
//
// Only the fields the connector actually needs are decoded. evcc also
// returns price/pricePerKWh/co2PerKWh/referencePricePerKWh/referenceCo2PerKWh
// on each session; those are deliberately not part of Session, so they can
// never end up forwarded to the GCS API.
package evcc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Session is a single evcc charging session, as returned by GET /api/sessions.
type Session struct {
	ID              int
	Created         time.Time
	Finished        *time.Time
	Loadpoint       string
	Vehicle         string
	ChargedEnergy   float64 // kWh
	SolarPercentage *float64
}

type sessionDTO struct {
	ID              int        `json:"id"`
	Created         time.Time  `json:"created"`
	Finished        *time.Time `json:"finished"`
	Loadpoint       string     `json:"loadpoint"`
	Vehicle         string     `json:"vehicle"`
	ChargedEnergy   float64    `json:"chargedEnergy"`
	SolarPercentage *float64   `json:"solarPercentage"`
}

// Client talks to a single evcc instance's HTTP API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient returns a Client for the evcc instance at baseURL
// (e.g. "http://192.168.1.50:7070", without a trailing "/api").
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

// FetchSessions returns all sessions evcc recorded for the given calendar
// month/year, including in-progress sessions (Session.Finished is nil for
// those).
func (c *Client) FetchSessions(ctx context.Context, month, year int) ([]Session, error) {
	u := c.BaseURL + "/api/sessions?" + url.Values{
		"month": {strconv.Itoa(month)},
		"year":  {strconv.Itoa(year)},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("evcc: building request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("evcc: fetching sessions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("evcc: unexpected status %d fetching sessions", resp.StatusCode)
	}

	var dtos []sessionDTO
	if err := json.NewDecoder(resp.Body).Decode(&dtos); err != nil {
		return nil, fmt.Errorf("evcc: decoding sessions response: %w", err)
	}

	sessions := make([]Session, len(dtos))
	for i, d := range dtos {
		sessions[i] = Session{
			ID:              d.ID,
			Created:         d.Created,
			Finished:        d.Finished,
			Loadpoint:       d.Loadpoint,
			Vehicle:         d.Vehicle,
			ChargedEnergy:   d.ChargedEnergy,
			SolarPercentage: d.SolarPercentage,
		}
	}
	return sessions, nil
}
