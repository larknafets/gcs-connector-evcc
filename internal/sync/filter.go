package sync

import (
	"sort"
	"strings"
	"time"

	"github.com/larknafets/gcs-connector-evcc/internal/evcc"
)

// FilterFinished keeps only sessions that have completed (Finished != nil).
func FilterFinished(sessions []evcc.Session) []evcc.Session {
	result := make([]evcc.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Finished != nil {
			result = append(result, s)
		}
	}
	return result
}

// FilterAfterWatermark keeps only sessions whose Finished timestamp is
// strictly after watermark. Sessions without a Finished timestamp are
// dropped; call FilterFinished first if that matters to the caller.
func FilterAfterWatermark(sessions []evcc.Session, watermark time.Time) []evcc.Session {
	result := make([]evcc.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Finished != nil && s.Finished.After(watermark) {
			result = append(result, s)
		}
	}
	return result
}

// FilterIgnored drops sessions whose Vehicle or Loadpoint matches one of the
// configured ignore lists, exactly and case-insensitively.
func FilterIgnored(sessions []evcc.Session, ignoreVehicles, ignoreLoadpoints []string) []evcc.Session {
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

// SortByFinished returns sessions sorted ascending by Finished timestamp.
// Callers must filter out sessions with a nil Finished first.
func SortByFinished(sessions []evcc.Session) []evcc.Session {
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
