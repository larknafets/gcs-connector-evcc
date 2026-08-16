package sync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	stdsync "sync"
	"testing"
	"time"

	"github.com/larknafets/gcs-connector-evcc/internal/evcc"
	"github.com/larknafets/gcs-connector-evcc/internal/gcs"
	"github.com/larknafets/gcs-connector-evcc/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEVCCSession is the JSON shape the fake evcc server emits.
type fakeEVCCSession struct {
	ID              int      `json:"id"`
	Created         string   `json:"created"`
	Finished        *string  `json:"finished"`
	Loadpoint       string   `json:"loadpoint"`
	Vehicle         string   `json:"vehicle"`
	ChargedEnergy   float64  `json:"chargedEnergy"`
	SolarPercentage *float64 `json:"solarPercentage"`
}

// newFakeEVCC serves sessionsByMonthYear["8-2026"] style buckets and records
// every query it receives.
func newFakeEVCC(t *testing.T, sessionsByMonthYear map[string][]fakeEVCCSession) (*httptest.Server, *[]string) {
	t.Helper()
	var mu stdsync.Mutex
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()

		q := r.URL.Query()
		key := q.Get("month") + "-" + q.Get("year")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sessionsByMonthYear[key])
	}))
	t.Cleanup(server.Close)
	return server, &queries
}

type fakeGCSBehavior struct {
	statusBySessionID map[string]int // external_session_id -> HTTP status to respond with
}

type recordedPost struct {
	payload gcs.ChargePayload
}

// newFakeGCS serves POST /api/v1/connector/charges and GET .../charges,
// recording every posted payload and letting tests script per-session
// failures via behavior.statusBySessionID.
func newFakeGCS(t *testing.T, behavior fakeGCSBehavior, existing []gcs.ExistingCharge) (*httptest.Server, *[]recordedPost) {
	t.Helper()
	var mu stdsync.Mutex
	var posts []recordedPost

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/connector/charges":
			var payload gcs.ChargePayload
			_ = json.NewDecoder(r.Body).Decode(&payload)

			mu.Lock()
			posts = append(posts, recordedPost{payload: payload})
			mu.Unlock()

			if status, ok := behavior.statusBySessionID[payload.ExternalSessionID]; ok && status != http.StatusOK && status != http.StatusCreated {
				w.WriteHeader(status)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"status":"created"}`))

		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/connector/charges":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(existing)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, &posts
}

func newTestOrchestrator(t *testing.T, evccURL, gcsURL string, now time.Time) (*Orchestrator, *state.Store, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".env")
	store := state.NewStore(configPath)

	gcsClient := gcs.NewClient(gcsURL, "key", "secret")
	gcsClient.HTTP.RetryMax = 1
	gcsClient.HTTP.RetryWaitMin = time.Millisecond
	gcsClient.HTTP.RetryWaitMax = 2 * time.Millisecond

	orch := &Orchestrator{
		EVCC:     evcc.NewClient(evccURL),
		GCS:      gcsClient,
		Store:    store,
		SiteName: "Zuhause Carport",
		Now:      func() time.Time { return now },
	}
	return orch, store, configPath
}

func strPtr(s string) *string { return &s }

func TestRunCycle_HappyPath_SendsAllNewFinishedSessions(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sessions := map[string][]fakeEVCCSession{
		"8-2026": {
			{ID: 1, Created: "2026-08-14T10:00:00Z", Finished: strPtr("2026-08-14T11:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 5.0},
			{ID: 2, Created: "2026-08-14T12:00:00Z", Finished: strPtr("2026-08-14T13:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 3.0},
		},
		"7-2026": {},
	}
	evccServer, _ := newFakeEVCC(t, sessions)
	gcsServer, posts := newFakeGCS(t, fakeGCSBehavior{}, nil)

	orch, store, _ := newTestOrchestrator(t, evccServer.URL, gcsServer.URL, now)

	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, result.Sent)
	assert.Equal(t, 0, result.DuplicateSkipped)
	assert.Equal(t, 0, result.Failed)
	assert.Len(t, *posts, 2)

	st, corrupted, err := store.Load()
	require.NoError(t, err)
	assert.False(t, corrupted)
	assert.True(t, st.LastSyncedFinishedAt.Equal(time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC)))
}

func TestRunCycle_QueriesCurrentAndPreviousMonth(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 30, 0, 0, time.UTC) // just after month boundary
	evccServer, queries := newFakeEVCC(t, map[string][]fakeEVCCSession{})
	gcsServer, _ := newFakeGCS(t, fakeGCSBehavior{}, nil)

	orch, _, _ := newTestOrchestrator(t, evccServer.URL, gcsServer.URL, now)
	_, err := orch.RunCycle(context.Background())
	require.NoError(t, err)

	got := map[string]bool{}
	for _, q := range *queries {
		v, _ := url.ParseQuery(q)
		got[v.Get("month")+"-"+v.Get("year")] = true
	}
	assert.True(t, got["8-2026"], "expected current month queried")
	assert.True(t, got["7-2026"], "expected previous month queried")
}

func TestFetchMonths_ZeroWatermarkIsCurrentAndPreviousMonth(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	months := fetchMonths(now, time.Time{})
	require.Len(t, months, 2)
	assert.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), months[0])
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), months[1])
}

func TestFetchMonths_RecentWatermarkStillOnlyCurrentAndPreviousMonth(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	watermark := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	months := fetchMonths(now, watermark)
	require.Len(t, months, 2)
	assert.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), months[0])
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), months[1])
}

func TestFetchMonths_StaleWatermarkExtendsRangeBackToItsMonth(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	watermark := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC) // three months stale
	months := fetchMonths(now, watermark)
	require.Len(t, months, 4)
	assert.Equal(t, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), months[0])
	assert.Equal(t, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), months[1])
	assert.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), months[2])
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), months[3])
}

func TestRunCycle_StaleWatermark_PermanentlyFailingSessionIsNotSilentlyDroppedAfterTwoMonths(t *testing.T) {
	// Regression test: a session finished ~3 months before "now" must still
	// be fetched (and thus still retried) as long as the watermark hasn't
	// advanced past it - a naive fixed current+previous-month fetch window
	// would silently stop fetching its month and lose it forever.
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sessions := map[string][]fakeEVCCSession{
		"5-2026": {
			{ID: 1, Created: "2026-05-10T09:00:00Z", Finished: strPtr("2026-05-10T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
		},
		"6-2026": {},
		"7-2026": {},
		"8-2026": {},
	}
	evccServer, queries := newFakeEVCC(t, sessions)
	gcsServer, posts := newFakeGCS(t, fakeGCSBehavior{
		statusBySessionID: map[string]int{"1": http.StatusUnprocessableEntity},
	}, nil)

	orch, store, _ := newTestOrchestrator(t, evccServer.URL, gcsServer.URL, now)
	// Simulate a watermark left stale three months ago by a prior cycle.
	require.NoError(t, store.Save(state.State{LastSyncedFinishedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}))

	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.Failed)
	assert.Len(t, *posts, 1, "the stuck session must still be attempted")

	got := map[string]bool{}
	for _, q := range *queries {
		v, _ := url.ParseQuery(q)
		got[v.Get("month")+"-"+v.Get("year")] = true
	}
	assert.True(t, got["5-2026"], "must still query the stale session's month")
}

func TestRunCycle_UnfinishedSessionNotSent(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sessions := map[string][]fakeEVCCSession{
		"8-2026": {
			{ID: 1, Created: "2026-08-15T10:00:00Z", Finished: nil, Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 5.0},
		},
		"7-2026": {},
	}
	evccServer, _ := newFakeEVCC(t, sessions)
	gcsServer, posts := newFakeGCS(t, fakeGCSBehavior{}, nil)

	orch, _, _ := newTestOrchestrator(t, evccServer.URL, gcsServer.URL, now)
	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Empty(t, *posts)
}

func TestRunCycle_IgnoredVehicleNotSent(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sessions := map[string][]fakeEVCCSession{
		"8-2026": {
			{ID: 1, Created: "2026-08-14T10:00:00Z", Finished: strPtr("2026-08-14T11:00:00Z"), Loadpoint: "Garage", Vehicle: "Kühlschrank Garage", ChargedEnergy: 1.0},
		},
		"7-2026": {},
	}
	evccServer, _ := newFakeEVCC(t, sessions)
	gcsServer, posts := newFakeGCS(t, fakeGCSBehavior{}, nil)

	orch, _, _ := newTestOrchestrator(t, evccServer.URL, gcsServer.URL, now)
	orch.IgnoreVehicles = []string{"kühlschrank garage"}

	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Empty(t, *posts)
}

func TestRunCycle_PartialFailure_WatermarkStopsAtFirstFailure(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sessions := map[string][]fakeEVCCSession{
		"8-2026": {
			{ID: 1, Created: "2026-08-14T09:00:00Z", Finished: strPtr("2026-08-14T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
			{ID: 2, Created: "2026-08-14T11:00:00Z", Finished: strPtr("2026-08-14T12:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 2.0},
			{ID: 3, Created: "2026-08-14T13:00:00Z", Finished: strPtr("2026-08-14T14:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 3.0},
		},
		"7-2026": {},
	}
	evccServer, _ := newFakeEVCC(t, sessions)
	gcsServer, posts := newFakeGCS(t, fakeGCSBehavior{
		statusBySessionID: map[string]int{"2": http.StatusUnprocessableEntity},
	}, nil)

	orch, store, _ := newTestOrchestrator(t, evccServer.URL, gcsServer.URL, now)
	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, result.Sent) // sessions 1 and 3
	assert.Equal(t, 1, result.Failed)
	assert.Len(t, *posts, 3) // all three were attempted

	st, _, err := store.Load()
	require.NoError(t, err)
	// watermark must NOT advance past session 1's Finished, even though
	// session 3 (after the failure) succeeded - otherwise session 2 would
	// never be retried.
	assert.True(t, st.LastSyncedFinishedAt.Equal(time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)),
		"got watermark %s", st.LastSyncedFinishedAt)
}

func TestRunCycle_Unauthorized_ReturnsFatalErrorAndStopsProcessing(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sessions := map[string][]fakeEVCCSession{
		"8-2026": {
			{ID: 1, Created: "2026-08-14T09:00:00Z", Finished: strPtr("2026-08-14T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
			{ID: 2, Created: "2026-08-14T11:00:00Z", Finished: strPtr("2026-08-14T12:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 2.0},
		},
		"7-2026": {},
	}
	evccServer, _ := newFakeEVCC(t, sessions)
	gcsServer, posts := newFakeGCS(t, fakeGCSBehavior{
		statusBySessionID: map[string]int{"1": http.StatusUnauthorized},
	}, nil)

	orch, _, _ := newTestOrchestrator(t, evccServer.URL, gcsServer.URL, now)
	_, err := orch.RunCycle(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFatal)
	assert.Len(t, *posts, 1, "must stop immediately, never attempt session 2")
}

func TestRunCycle_DuplicateSkippedCountsAsSuccessAndAdvancesWatermark(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sessions := map[string][]fakeEVCCSession{
		"8-2026": {
			{ID: 1, Created: "2026-08-14T09:00:00Z", Finished: strPtr("2026-08-14T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
		},
		"7-2026": {},
	}
	evccServer, _ := newFakeEVCC(t, sessions)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"duplicate_skipped"}`))
	}))
	t.Cleanup(server.Close)

	orch, store, _ := newTestOrchestrator(t, evccServer.URL, server.URL, now)
	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Equal(t, 1, result.DuplicateSkipped)

	st, _, _ := store.Load()
	assert.True(t, st.LastSyncedFinishedAt.Equal(time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)))
}

func TestRunCycle_EVCCUnreachable_AbortsCycleWithoutError(t *testing.T) {
	// evcc unreachable must not crash the connector - RunCycle returns a
	// non-fatal error the caller logs, state is left untouched.
	orch, store, _ := newTestOrchestrator(t, "http://127.0.0.1:1", "http://127.0.0.1:1", time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC))
	_, err := orch.RunCycle(context.Background())
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrFatal))

	_, corrupted, loadErr := store.Load()
	require.NoError(t, loadErr)
	assert.False(t, corrupted)
}

func TestRunCycle_MissingStateFile_TreatsAsZeroWatermark(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sessions := map[string][]fakeEVCCSession{
		"8-2026": {
			{ID: 1, Created: "2026-08-01T09:00:00Z", Finished: strPtr("2026-08-01T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
		},
		"7-2026": {},
	}
	evccServer, _ := newFakeEVCC(t, sessions)
	gcsServer, posts := newFakeGCS(t, fakeGCSBehavior{}, nil)

	orch, _, _ := newTestOrchestrator(t, evccServer.URL, gcsServer.URL, now)
	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.Sent)
	assert.Len(t, *posts, 1)
}

func TestPreview_ReportsNewVsAlreadyPresentWithoutSendingOrWritingState(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sessions := map[string][]fakeEVCCSession{
		"8-2026": {
			{ID: 1, Created: "2026-08-14T09:00:00Z", Finished: strPtr("2026-08-14T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
			{ID: 2, Created: "2026-08-14T11:00:00Z", Finished: strPtr("2026-08-14T12:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 2.0},
		},
		"7-2026": {},
	}
	evccServer, _ := newFakeEVCC(t, sessions)
	gcsServer, posts := newFakeGCS(t, fakeGCSBehavior{}, []gcs.ExistingCharge{
		{ExternalSessionID: "1", StartAt: time.Now(), EndAt: time.Now(), ChargedEnergyWh: 1000},
	})

	orch, store, _ := newTestOrchestrator(t, evccServer.URL, gcsServer.URL, now)
	result, err := orch.Preview(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.NewSessions)
	assert.Equal(t, 1, result.AlreadyPresent)
	assert.Empty(t, *posts, "dry-run must never POST")

	_, corrupted, loadErr := store.Load()
	require.NoError(t, loadErr)
	assert.False(t, corrupted)
	st, _, _ := store.Load()
	assert.True(t, st.LastSyncedFinishedAt.IsZero(), "dry-run must never write state")
}
