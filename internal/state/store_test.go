package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_MissingFileReturnsZeroStateNotCorrupted(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, ".env"))

	st, corrupted, err := store.Load()
	require.NoError(t, err)
	assert.False(t, corrupted)
	assert.True(t, st.LastSyncedFinishedAt.IsZero())
}

func TestLoad_CorruptFileReturnsZeroStateAndCorruptedFlag(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte("{not valid json"), 0o644))
	store := NewStore(filepath.Join(dir, ".env"))

	st, corrupted, err := store.Load()
	require.NoError(t, err)
	assert.True(t, corrupted)
	assert.True(t, st.LastSyncedFinishedAt.IsZero())
}

func TestLoad_UnknownVersionTreatedAsCorrupted(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"version":99,"last_synced_finished_at":"2026-08-14T10:00:00Z"}`), 0o644))
	store := NewStore(filepath.Join(dir, ".env"))

	_, corrupted, err := store.Load()
	require.NoError(t, err)
	assert.True(t, corrupted)
}

func TestSaveThenLoad_Roundtrips(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, ".env"))

	watermark := time.Date(2026, 8, 15, 8, 0, 3, 0, time.UTC)
	require.NoError(t, store.Save(State{LastSyncedFinishedAt: watermark}))

	st, corrupted, err := store.Load()
	require.NoError(t, err)
	assert.False(t, corrupted)
	assert.True(t, watermark.Equal(st.LastSyncedFinishedAt))
}

func TestSave_WritesAtomicallyNoLeftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, ".env"))

	require.NoError(t, store.Save(State{LastSyncedFinishedAt: time.Now()}))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "state.json", entries[0].Name())
}

func TestSave_WritesVersionField(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, ".env"))
	require.NoError(t, store.Save(State{LastSyncedFinishedAt: time.Now()}))

	raw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"version":1`)
}

func TestLock_SecondAcquireFailsWhileFirstHeld(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, ".env"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	unlock, err := store.Lock(ctx)
	require.NoError(t, err)
	defer unlock()

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer shortCancel()

	other := NewStore(filepath.Join(dir, ".env"))
	_, err = other.Lock(shortCtx)
	require.Error(t, err)
}

func TestLock_ReleasedAfterUnlockAllowsReacquire(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, ".env"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	unlock, err := store.Lock(ctx)
	require.NoError(t, err)
	unlock()

	other := NewStore(filepath.Join(dir, ".env"))
	unlock2, err := other.Lock(ctx)
	require.NoError(t, err)
	unlock2()
}
