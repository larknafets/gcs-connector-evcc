package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdsync "sync"
	"time"

	"github.com/larknafets/gcs-connector-evcc/internal/evcc"
	"github.com/larknafets/gcs-connector-evcc/internal/gcs"
	"github.com/larknafets/gcs-connector-evcc/internal/state"
)

// ErrFatal wraps sync errors that mean the connector must stop entirely
// (currently: GCS rejected the API key/secret). The daemon loop checks for
// this with errors.Is to decide whether to exit the process.
var ErrFatal = errors.New("sync: fatal error")

// sessionSource fetches evcc sessions for one calendar month. *evcc.Client
// is the production adapter; tests use an in-memory fake so Orchestrator's
// own branching logic can be exercised without a real HTTP round-trip -
// *evcc.Client's HTTP behavior has its own coverage in internal/evcc.
type sessionSource interface {
	FetchSessions(ctx context.Context, month, year int) ([]evcc.Session, error)
}

// sessionSink sends charges to GCS and reports what it already has.
// *gcs.Client is the production adapter; tests use an in-memory fake for the
// same reason as sessionSource - *gcs.Client's HTTP-status-to-error mapping
// has its own coverage in internal/gcs.
type sessionSink interface {
	PostCharge(ctx context.Context, payload gcs.ChargePayload) (duplicateSkipped bool, err error)
	GetChargesSince(ctx context.Context, since time.Time) ([]gcs.ExistingCharge, error)
}

// Orchestrator wires the evcc client, GCS client and local state together
// into a single sync cycle. It is the primary test seam: tests construct one
// against in-memory sessionSource/sessionSink fakes and assert on what was
// recorded plus the resulting state.json.
type Orchestrator struct {
	EVCC             sessionSource
	GCS              sessionSink
	Store            *state.Store
	IgnoreVehicles   []string
	IgnoreLoadpoints []string
	// Now is injectable so tests control which month/year get queried.
	Now func() time.Time
	// Logger defaults to slog.Default() if nil.
	Logger *slog.Logger
}

// CycleResult summarizes one RunCycle invocation.
type CycleResult struct {
	Sent             int
	DuplicateSkipped int
	Failed           int
}

// PreviewResult summarizes one Preview (--dry-run) invocation.
type PreviewResult struct {
	NewSessions    int
	AlreadyPresent int
}

func (o *Orchestrator) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

func (o *Orchestrator) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// fetchMonths returns the calendar months (as first-of-month timestamps,
// ascending) eligibleSessions should query: normally just the current and
// previous month, but extended further back if the watermark is older than
// that - otherwise a session stuck behind a permanently failing one (see
// RunCycle) would silently age out of a fixed two-month window and never be
// retried again once evcc stops returning its month.
func fetchMonths(now, watermark time.Time) []time.Time {
	currentFirst := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	start := currentFirst.AddDate(0, -1, 0)

	if !watermark.IsZero() {
		watermarkFirst := time.Date(watermark.Year(), watermark.Month(), 1, 0, 0, 0, 0, watermark.Location())
		if watermarkFirst.Before(start) {
			start = watermarkFirst
		}
	}

	months := []time.Time{start}
	for cur := start; cur.Before(currentFirst); cur = cur.AddDate(0, 1, 0) {
		months = append(months, cur.AddDate(0, 1, 0))
	}
	return months
}

// eligibleSessions fetches every month between the watermark and now from
// evcc (concurrently - these are independent requests) and returns the
// sessions this cycle should consider sending, already filtered
// (finished-only, after the watermark, not ignored) and sorted ascending by
// Finished.
func (o *Orchestrator) eligibleSessions(ctx context.Context, watermark time.Time) ([]evcc.Session, error) {
	months := fetchMonths(o.now(), watermark)

	results := make([][]evcc.Session, len(months))
	errs := make([]error, len(months))
	var wg stdsync.WaitGroup
	for i, m := range months {
		wg.Add(1)
		go func(i int, m time.Time) {
			defer wg.Done()
			sessions, err := o.EVCC.FetchSessions(ctx, int(m.Month()), m.Year())
			if err != nil {
				errs[i] = fmt.Errorf("sync: fetching evcc sessions for %d-%d: %w", m.Year(), int(m.Month()), err)
				return
			}
			results[i] = sessions
		}(i, m)
	}
	wg.Wait()

	var all []evcc.Session
	for i := range months {
		if errs[i] != nil {
			return nil, errs[i]
		}
		all = append(all, results[i]...)
	}

	return filterEligible(all, watermark, o.IgnoreVehicles, o.IgnoreLoadpoints), nil
}

// RunCycle performs one full sync: fetch from evcc, filter, map, send each
// session to GCS in order, and persist the watermark after every individual
// success. It never sends more than one HTTP request to GCS per session
// (matching the Connector-API's one-record-per-POST contract).
//
// The watermark only ever advances over a contiguous run of successes from
// the start of the sorted list: once any session fails (429 exhausted or
// 422), later sessions may still be attempted, but the watermark stops
// moving - otherwise a session after the failure could jump the watermark
// past the failed one, and it would never be retried.
func (o *Orchestrator) RunCycle(ctx context.Context) (CycleResult, error) {
	var result CycleResult

	unlock, err := o.Store.Lock(ctx)
	if err != nil {
		return result, fmt.Errorf("sync: %w", err)
	}
	defer unlock()

	st, corrupted, err := o.Store.Load()
	if err != nil {
		return result, fmt.Errorf("sync: loading state: %w", err)
	}
	if corrupted {
		o.logger().Warn("state.json missing or unreadable, starting from an empty watermark")
	}

	sessions, err := o.eligibleSessions(ctx, st.LastSyncedFinishedAt)
	if err != nil {
		o.logger().Warn("aborting sync cycle: could not fetch sessions from evcc", "error", err)
		return result, err
	}

	hitFailure := false

	for _, session := range sessions {
		payload, err := ToChargePayload(session)
		if err != nil {
			// Unreachable in practice: eligibleSessions already filters to
			// finished-only sessions, which is the only precondition
			// ToChargePayload enforces. Treat defensively as a skip.
			o.logger().Warn("skipping session with unmappable payload", "session_id", session.ID, "error", err)
			result.Failed++
			hitFailure = true
			continue
		}

		duplicate, err := o.GCS.PostCharge(ctx, payload)
		if err != nil {
			if errors.Is(err, gcs.ErrUnauthorized) {
				o.logger().Error("GCS rejected the API key/secret, stopping connector", "error", err)
				return result, fmt.Errorf("%w: %w", ErrFatal, err)
			}
			if errors.Is(err, gcs.ErrRateLimited) || errors.Is(err, gcs.ErrInvalidPayload) {
				o.logger().Warn("skipping session after send failure, will retry next cycle", "session_id", session.ID, "error", err)
				result.Failed++
				hitFailure = true
				continue
			}
			// Network/unreachable: abort the whole cycle, matching evcc's
			// unreachable handling - next cycle retries everything from the
			// current (already-persisted) watermark.
			o.logger().Warn("aborting sync cycle: GCS unreachable", "error", err)
			return result, err
		}

		if duplicate {
			result.DuplicateSkipped++
		} else {
			result.Sent++
		}

		if !hitFailure {
			watermark := *session.Finished
			if err := o.Store.Save(state.State{LastSyncedFinishedAt: watermark}); err != nil {
				return result, fmt.Errorf("sync: persisting state: %w", err)
			}
		}
	}

	return result, nil
}

// Preview implements --dry-run: it fetches, filters and maps sessions like
// RunCycle, but classifies each as new or already-present via GET
// /api/v1/connector/charges?since= instead of POSTing, and never touches
// state.json.
func (o *Orchestrator) Preview(ctx context.Context) (PreviewResult, error) {
	var result PreviewResult

	st, corrupted, err := o.Store.Load()
	if err != nil {
		return result, fmt.Errorf("sync: loading state: %w", err)
	}
	if corrupted {
		o.logger().Warn("state.json missing or unreadable, previewing from an empty watermark")
	}

	sessions, err := o.eligibleSessions(ctx, st.LastSyncedFinishedAt)
	if err != nil {
		return result, err
	}

	existing, err := o.GCS.GetChargesSince(ctx, st.LastSyncedFinishedAt)
	if err != nil {
		return result, fmt.Errorf("sync: fetching existing charges: %w", err)
	}
	existingIDs := make(map[string]bool, len(existing))
	for _, c := range existing {
		existingIDs[c.ExternalSessionID] = true
	}

	for _, session := range sessions {
		payload, err := ToChargePayload(session)
		if err != nil {
			continue
		}
		if existingIDs[payload.ExternalSessionID] {
			result.AlreadyPresent++
		} else {
			result.NewSessions++
		}
	}

	return result, nil
}
