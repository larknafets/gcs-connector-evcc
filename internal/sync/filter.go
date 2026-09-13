package sync

import (
	"sort"
	"strings"
	"time"

	"github.com/larknafets/gcs-connector-evcc/internal/evcc"
)

// filterEligible narrows sessions down to the ones a sync cycle should
// consider sending: finished, after watermark, vehicle-allowed and
// loadpoint-not-ignored, sorted ascending by Finished. It owns the order
// internally - sortByFinished dereferences Finished unconditionally, so it
// must run after finished-only sessions have been isolated; the other two
// steps are order-independent with respect to each other and to that
// constraint.
func filterEligible(sessions []evcc.Session, watermark time.Time, syncVehicles, ignoreLoadpoints []string) []evcc.Session {
	eligible := filterFinished(sessions)
	eligible = filterAfterWatermark(eligible, watermark)
	eligible = filterAllowed(eligible, syncVehicles, ignoreLoadpoints)
	return sortByFinished(eligible)
}

// filterFinished keeps only sessions that have completed (Finished != nil).
func filterFinished(sessions []evcc.Session) []evcc.Session {
	result := make([]evcc.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Finished != nil {
			result = append(result, s)
		}
	}
	return result
}

// filterAfterWatermark keeps only sessions whose Finished timestamp is
// strictly after watermark. Sessions without a Finished timestamp are
// dropped too, but callers should not rely on that - it's a side effect of
// the nil-guard here, not this function's job.
func filterAfterWatermark(sessions []evcc.Session, watermark time.Time) []evcc.Session {
	result := make([]evcc.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Finished != nil && s.Finished.After(watermark) {
			result = append(result, s)
		}
	}
	return result
}

// filterAllowed keeps sessions whose Vehicle is either empty (evcc couldn't
// attribute the session to a known vehicle - a guest charge, always synced)
// or listed in syncVehicles, and drops sessions whose Loadpoint matches
// ignoreLoadpoints. Both lists match exactly and case-insensitively; an
// empty syncVehicles list allows no named vehicle through, guest sessions
// excepted.
func filterAllowed(sessions []evcc.Session, syncVehicles, ignoreLoadpoints []string) []evcc.Session {
	vehicles := toLowerSet(syncVehicles)
	loadpoints := toLowerSet(ignoreLoadpoints)

	result := make([]evcc.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Vehicle != "" && !vehicles[strings.ToLower(s.Vehicle)] {
			continue
		}
		if loadpoints[strings.ToLower(s.Loadpoint)] {
			continue
		}
		result = append(result, s)
	}
	return result
}

// sortByFinished returns sessions sorted ascending by Finished timestamp.
// Callers must filter out sessions with a nil Finished first.
func sortByFinished(sessions []evcc.Session) []evcc.Session {
	result := make([]evcc.Session, len(sessions))
	copy(result, sessions)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Finished.Before(*result[j].Finished)
	})
	return result
}

func toLowerSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[strings.ToLower(v)] = true
	}
	return set
}
