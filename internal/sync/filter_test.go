package sync

import (
	"testing"
	"time"

	"github.com/larknafets/gcs-connector-evcc/internal/evcc"
	"github.com/stretchr/testify/assert"
)

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func mustTimePtr(s string) *time.Time {
	t := mustTime(s)
	return &t
}

func TestFilterFinished_DropsSessionsWithoutFinishedTimestamp(t *testing.T) {
	sessions := []evcc.Session{
		{ID: 1, Finished: mustTimePtr("2026-08-14T10:00:00Z")},
		{ID: 2, Finished: nil},
	}

	result := FilterFinished(sessions)
	assert.Len(t, result, 1)
	assert.Equal(t, 1, result[0].ID)
}

func TestFilterAfterWatermark_KeepsOnlyStrictlyAfter(t *testing.T) {
	watermark := mustTime("2026-08-14T10:00:00Z")
	sessions := []evcc.Session{
		{ID: 1, Finished: mustTimePtr("2026-08-14T09:00:00Z")}, // before
		{ID: 2, Finished: mustTimePtr("2026-08-14T10:00:00Z")}, // equal, excluded
		{ID: 3, Finished: mustTimePtr("2026-08-14T11:00:00Z")}, // after
	}

	result := FilterAfterWatermark(sessions, watermark)
	assert.Len(t, result, 1)
	assert.Equal(t, 3, result[0].ID)
}

func TestFilterAfterWatermark_ZeroWatermarkKeepsAll(t *testing.T) {
	sessions := []evcc.Session{
		{ID: 1, Finished: mustTimePtr("2026-08-14T09:00:00Z")},
	}
	result := FilterAfterWatermark(sessions, time.Time{})
	assert.Len(t, result, 1)
}

func TestFilterIgnored_MatchesVehicleAndLoadpointCaseInsensitively(t *testing.T) {
	sessions := []evcc.Session{
		{ID: 1, Vehicle: "James", Loadpoint: "Garage"},
		{ID: 2, Vehicle: "kühlschrank garage", Loadpoint: "Garage"},
		{ID: 3, Vehicle: "James", Loadpoint: "Werkstatt"},
	}

	result := FilterIgnored(sessions, []string{"Kühlschrank Garage"}, []string{"WERKSTATT"})
	assert.Len(t, result, 1)
	assert.Equal(t, 1, result[0].ID)
}

func TestFilterIgnored_EmptyListsKeepEverything(t *testing.T) {
	sessions := []evcc.Session{
		{ID: 1, Vehicle: "James", Loadpoint: "Garage"},
	}
	result := FilterIgnored(sessions, nil, nil)
	assert.Len(t, result, 1)
}

func TestSortByFinished_Ascending(t *testing.T) {
	sessions := []evcc.Session{
		{ID: 3, Finished: mustTimePtr("2026-08-14T12:00:00Z")},
		{ID: 1, Finished: mustTimePtr("2026-08-14T09:00:00Z")},
		{ID: 2, Finished: mustTimePtr("2026-08-14T10:00:00Z")},
	}

	result := SortByFinished(sessions)
	assert.Equal(t, []int{1, 2, 3}, []int{result[0].ID, result[1].ID, result[2].ID})
}
