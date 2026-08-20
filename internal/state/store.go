// Package state persists the connector's sync watermark to a local
// state.json file, co-located with the resolved config file.
package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// CurrentVersion is the state.json envelope version this build writes and
// understands. A file with any other version is treated as corrupted, so a
// future format change fails safe (empty watermark) instead of misreading.
const CurrentVersion = 1

// State is the connector's persisted sync progress.
type State struct {
	LastSyncedFinishedAt time.Time
}

type envelope struct {
	Version              int       `json:"version"`
	LastSyncedFinishedAt time.Time `json:"last_synced_finished_at"`
}

// Store reads/writes state.json next to the resolved config file.
type Store struct {
	statePath string
	lockPath  string
}

// NewStore returns a Store whose state.json lives in the same directory as
// configPath (the resolved .env file, e.g. from --config or the default
// location).
func NewStore(configPath string) *Store {
	dir := filepath.Dir(configPath)
	return &Store{
		statePath: filepath.Join(dir, "state.json"),
		lockPath:  filepath.Join(dir, "state.json.lock"),
	}
}

// Lock acquires an exclusive file lock guarding state.json for the duration
// of a sync cycle, so two connector instances pointed at the same config
// can't corrupt each other's state. The returned func releases the lock and
// must be called (typically via defer).
func (s *Store) Lock(ctx context.Context) (unlock func(), err error) {
	fl := flock.New(s.lockPath)
	locked, err := fl.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("state: acquiring lock: %w", err)
	}
	if !locked {
		return nil, errors.New("state: could not acquire lock, another instance may be running")
	}
	return func() { _ = fl.Unlock() }, nil
}

// Load reads state.json. A missing file is the expected first-run case:
// it returns a zero State with corrupted=false. An unreadable/unparseable
// file (or one written by an unsupported version) returns a zero State with
// corrupted=true, so the caller can log a warning - Load itself never fails
// for a bad file, only for unexpected I/O errors.
func (s *Store) Load() (state State, corrupted bool, err error) {
	raw, err := os.ReadFile(s.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("state: reading %s: %w", s.statePath, err)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return State{}, true, nil
	}
	if env.Version != CurrentVersion {
		return State{}, true, nil
	}

	return State{LastSyncedFinishedAt: env.LastSyncedFinishedAt}, false, nil
}

// Save writes state atomically: it writes to a temp file in the same
// directory, then renames it over state.json, so a crash mid-write never
// leaves a corrupt file in place.
func (s *Store) Save(state State) error {
	env := envelope{
		Version:              CurrentVersion,
		LastSyncedFinishedAt: state.LastSyncedFinishedAt,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("state: encoding: %w", err)
	}

	dir := filepath.Dir(s.statePath)
	tmp, err := os.CreateTemp(dir, "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("state: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("state: writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("state: closing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.statePath); err != nil {
		return fmt.Errorf("state: renaming into place: %w", err)
	}
	return nil
}
