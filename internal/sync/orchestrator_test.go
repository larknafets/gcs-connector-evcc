package sync

import (
	"context"
	"errors"
	"fmt"
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

// fakeSource is an in-memory sessionSource, keyed by "month-year" matching
// fetchMonths' query shape. It records every (month, year) it was asked for,
// so tests can assert on which months eligibleSessions fetched.
type fakeSource struct {
	mu       stdsync.Mutex
	sessions map[string][]evcc.Session
	queries  []string
	err      error // if set, every FetchSessions call fails with this
}

func (f *fakeSource) FetchSessions(ctx context.Context, month, year int) ([]evcc.Session, error) {
	key := fmt.Sprintf("%d-%d", month, year)
	f.mu.Lock()
	f.queries = append(f.queries, key)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.sessions[key], nil
}

// fakeSink is an in-memory sessionSink. It records every posted payload and
// returns a scripted result per session (by ExternalSessionID), defaulting
// to a plain success.
type fakeSink struct {
	mu                  stdsync.Mutex
	posts               []gcs.ChargePayload
	errBySessionID      map[string]error
	duplicateSessionIDs map[string]bool
	existing            []gcs.ExistingCharge
}

func (f *fakeSink) PostCharge(ctx context.Context, payload gcs.ChargePayload) (bool, error) {
	f.mu.Lock()
	f.posts = append(f.posts, payload)
	f.mu.Unlock()

	if err, ok := f.errBySessionID[payload.ExternalSessionID]; ok {
		return false, err
	}
	return f.duplicateSessionIDs[payload.ExternalSessionID], nil
}

func (f *fakeSink) GetChargesSince(ctx context.Context, since time.Time) ([]gcs.ExistingCharge, error) {
	return f.existing, nil
}

func newTestOrchestrator(t *testing.T, source *fakeSource, sink *fakeSink, now time.Time) (*Orchestrator, *state.Store, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".env")
	store := state.NewStore(configPath)

	orch := &Orchestrator{
		EVCC:  source,
		GCS:   sink,
		Store: store,
		Now:   func() time.Time { return now },
	}
	return orch, store, configPath
}

func TestRunCycle_HappyPath_SendsAllNewFinishedSessions(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{sessions: map[string][]evcc.Session{
		"8-2026": {
			{ID: 1, Created: mustTime("2026-08-14T10:00:00Z"), Finished: mustTimePtr("2026-08-14T11:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 5.0},
			{ID: 2, Created: mustTime("2026-08-14T12:00:00Z"), Finished: mustTimePtr("2026-08-14T13:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 3.0},
		},
		"7-2026": {},
	}}
	sink := &fakeSink{}

	orch, store, _ := newTestOrchestrator(t, source, sink, now)

	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, result.Sent)
	assert.Equal(t, 0, result.DuplicateSkipped)
	assert.Equal(t, 0, result.Failed)
	assert.Len(t, sink.posts, 2)

	st, corrupted, err := store.Load()
	require.NoError(t, err)
	assert.False(t, corrupted)
	assert.True(t, st.LastSyncedFinishedAt.Equal(time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC)))
}

func TestRunCycle_QueriesCurrentAndPreviousMonth(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 30, 0, 0, time.UTC) // just after month boundary
	source := &fakeSource{sessions: map[string][]evcc.Session{}}
	sink := &fakeSink{}

	orch, _, _ := newTestOrchestrator(t, source, sink, now)
	_, err := orch.RunCycle(context.Background())
	require.NoError(t, err)

	got := map[string]bool{}
	for _, q := range source.queries {
		got[q] = true
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
	source := &fakeSource{sessions: map[string][]evcc.Session{
		"5-2026": {
			{ID: 1, Created: mustTime("2026-05-10T09:00:00Z"), Finished: mustTimePtr("2026-05-10T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
		},
		"6-2026": {},
		"7-2026": {},
		"8-2026": {},
	}}
	sink := &fakeSink{errBySessionID: map[string]error{"1": gcs.ErrInvalidPayload}}

	orch, store, _ := newTestOrchestrator(t, source, sink, now)
	// Simulate a watermark left stale three months ago by a prior cycle.
	require.NoError(t, store.Save(state.State{LastSyncedFinishedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}))

	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.Failed)
	assert.Len(t, sink.posts, 1, "the stuck session must still be attempted")

	got := map[string]bool{}
	for _, q := range source.queries {
		got[q] = true
	}
	assert.True(t, got["5-2026"], "must still query the stale session's month")
}

func TestRunCycle_UnfinishedSessionNotSent(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{sessions: map[string][]evcc.Session{
		"8-2026": {
			{ID: 1, Created: mustTime("2026-08-15T10:00:00Z"), Finished: nil, Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 5.0},
		},
		"7-2026": {},
	}}
	sink := &fakeSink{}

	orch, _, _ := newTestOrchestrator(t, source, sink, now)
	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Empty(t, sink.posts)
}

func TestRunCycle_IgnoredVehicleNotSent(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{sessions: map[string][]evcc.Session{
		"8-2026": {
			{ID: 1, Created: mustTime("2026-08-14T10:00:00Z"), Finished: mustTimePtr("2026-08-14T11:00:00Z"), Loadpoint: "Garage", Vehicle: "Kühlschrank Garage", ChargedEnergy: 1.0},
		},
		"7-2026": {},
	}}
	sink := &fakeSink{}

	orch, _, _ := newTestOrchestrator(t, source, sink, now)
	orch.IgnoreVehicles = []string{"kühlschrank garage"}

	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, result.Sent)
	assert.Empty(t, sink.posts)
}

func TestRunCycle_PartialFailure_WatermarkStopsAtFirstFailure(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{sessions: map[string][]evcc.Session{
		"8-2026": {
			{ID: 1, Created: mustTime("2026-08-14T09:00:00Z"), Finished: mustTimePtr("2026-08-14T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
			{ID: 2, Created: mustTime("2026-08-14T11:00:00Z"), Finished: mustTimePtr("2026-08-14T12:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 2.0},
			{ID: 3, Created: mustTime("2026-08-14T13:00:00Z"), Finished: mustTimePtr("2026-08-14T14:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 3.0},
		},
		"7-2026": {},
	}}
	sink := &fakeSink{errBySessionID: map[string]error{"2": gcs.ErrInvalidPayload}}

	orch, store, _ := newTestOrchestrator(t, source, sink, now)
	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, result.Sent) // sessions 1 and 3
	assert.Equal(t, 1, result.Failed)
	assert.Len(t, sink.posts, 3) // all three were attempted

	st, _, err := store.Load()
	require.NoError(t, err)
	// watermark must NOT advance past session 1's Finished, even though
	// session 3 (after the failure) succeeded - otherwise session 2 would
	// never be retried.
	assert.True(t, st.LastSyncedFinishedAt.Equal(time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)),
		"got watermark %s", st.LastSyncedFinishedAt)
}

func TestRunCycle_PartialFailure_RateLimited_WatermarkStopsAtFirstFailure(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{sessions: map[string][]evcc.Session{
		"8-2026": {
			{ID: 1, Created: mustTime("2026-08-14T09:00:00Z"), Finished: mustTimePtr("2026-08-14T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
			{ID: 2, Created: mustTime("2026-08-14T11:00:00Z"), Finished: mustTimePtr("2026-08-14T12:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 2.0},
			{ID: 3, Created: mustTime("2026-08-14T13:00:00Z"), Finished: mustTimePtr("2026-08-14T14:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 3.0},
		},
		"7-2026": {},
	}}
	// gcs.ErrRateLimited already means "retries exhausted" by the time
	// sessionSink sees it - retryablehttp's retry loop is gcs.Client's own
	// concern, covered by internal/gcs's tests, not Orchestrator's.
	sink := &fakeSink{errBySessionID: map[string]error{"2": gcs.ErrRateLimited}}

	orch, store, _ := newTestOrchestrator(t, source, sink, now)
	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, result.Sent) // sessions 1 and 3
	assert.Equal(t, 1, result.Failed)
	assert.Len(t, sink.posts, 3, "all three sessions must have been attempted")

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
	source := &fakeSource{sessions: map[string][]evcc.Session{
		"8-2026": {
			{ID: 1, Created: mustTime("2026-08-14T09:00:00Z"), Finished: mustTimePtr("2026-08-14T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
			{ID: 2, Created: mustTime("2026-08-14T11:00:00Z"), Finished: mustTimePtr("2026-08-14T12:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 2.0},
		},
		"7-2026": {},
	}}
	sink := &fakeSink{errBySessionID: map[string]error{"1": gcs.ErrUnauthorized}}

	orch, _, _ := newTestOrchestrator(t, source, sink, now)
	_, err := orch.RunCycle(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFatal)
	assert.Len(t, sink.posts, 1, "must stop immediately, never attempt session 2")
}

func TestRunCycle_DuplicateSkippedCountsAsSuccessAndAdvancesWatermark(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{sessions: map[string][]evcc.Session{
		"8-2026": {
			{ID: 1, Created: mustTime("2026-08-14T09:00:00Z"), Finished: mustTimePtr("2026-08-14T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
		},
		"7-2026": {},
	}}
	sink := &fakeSink{duplicateSessionIDs: map[string]bool{"1": true}}

	orch, store, _ := newTestOrchestrator(t, source, sink, now)
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
	source := &fakeSource{err: errors.New("connection refused")}
	sink := &fakeSink{}

	orch, store, _ := newTestOrchestrator(t, source, sink, time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC))
	_, err := orch.RunCycle(context.Background())
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrFatal))

	_, corrupted, loadErr := store.Load()
	require.NoError(t, loadErr)
	assert.False(t, corrupted)
}

func TestRunCycle_MissingStateFile_TreatsAsZeroWatermark(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{sessions: map[string][]evcc.Session{
		"8-2026": {
			{ID: 1, Created: mustTime("2026-08-01T09:00:00Z"), Finished: mustTimePtr("2026-08-01T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
		},
		"7-2026": {},
	}}
	sink := &fakeSink{}

	orch, _, _ := newTestOrchestrator(t, source, sink, now)
	result, err := orch.RunCycle(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.Sent)
	assert.Len(t, sink.posts, 1)
}

func TestPreview_ReportsNewVsAlreadyPresentWithoutSendingOrWritingState(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{sessions: map[string][]evcc.Session{
		"8-2026": {
			{ID: 1, Created: mustTime("2026-08-14T09:00:00Z"), Finished: mustTimePtr("2026-08-14T10:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 1.0},
			{ID: 2, Created: mustTime("2026-08-14T11:00:00Z"), Finished: mustTimePtr("2026-08-14T12:00:00Z"), Loadpoint: "Garage", Vehicle: "James", ChargedEnergy: 2.0},
		},
		"7-2026": {},
	}}
	sink := &fakeSink{existing: []gcs.ExistingCharge{
		{ExternalSessionID: "1", StartAt: time.Now(), EndAt: time.Now(), ChargedEnergyWh: 1000},
	}}

	orch, store, _ := newTestOrchestrator(t, source, sink, now)
	result, err := orch.Preview(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.NewSessions)
	assert.Equal(t, 1, result.AlreadyPresent)
	assert.Empty(t, sink.posts, "dry-run must never POST")

	_, corrupted, loadErr := store.Load()
	require.NoError(t, loadErr)
	assert.False(t, corrupted)
	st, _, _ := store.Load()
	assert.True(t, st.LastSyncedFinishedAt.IsZero(), "dry-run must never write state")
}
