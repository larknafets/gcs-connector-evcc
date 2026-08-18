package sync

import (
	"sort"
	"strings"
	"time"

	"github.com/larknafets/gcs-connector-evcc/internal/evcc"
)

// filterEligible narrows sessions down to the ones a sync cycle should
// consider sending: finished, after watermark, not on an ignore list, sorted
// ascending by Finished. It owns the order internally - sortByFinished
// dereferences Finished unconditionally, so it must run after finished-only
// sessions have been isolated; the other two steps are order-independent
// with respect to each other and to that constraint.
func filterEligible(sessions []evcc.Session, watermark time.Time, ignoreVehicles, ignoreLoadpoints []string) []evcc.Session {
	eligible := filterFinished(sessions)
	eligible = filterAfterWatermark(eligible, watermark)
	eligible = filterIgnored(eligible, ignoreVehicles, ignoreLoadpoints)
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

// filterIgnored drops sessions whose Vehicle or Loadpoint matches one of the
// configured ignore lists, exactly and case-insensitively.
func filterIgnored(sessions []evcc.Session, ignoreVehicles, ignoreLoadpoints []string) []evcc.Session {
	vehicles := toLowerSet(ignoreVehicles)
	loadpoints := toLowerSet(ignoreLoadpoints)

	result := make([]evcc.Session, 0, len(sessions))
	for _, s := range sessions {
		if vehicles[strings.ToLower(s.Vehicle)] || loadpoints[strings.ToLower(s.Loadpoint)] {
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
