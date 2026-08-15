package loop

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTicker struct {
	ch chan time.Time
}

func newFakeTicker() *fakeTicker          { return &fakeTicker{ch: make(chan time.Time, 1)} }
func (f *fakeTicker) C() <-chan time.Time { return f.ch }
func (f *fakeTicker) Stop()               {}
func (f *fakeTicker) tick()               { f.ch <- time.Now() }

var errFatalForTest = errors.New("fatal for test")

func TestRun_RunsImmediatelyOnStartWithoutWaitingForFirstTick(t *testing.T) {
	var calls int32
	ticker := newFakeTicker()

	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		Interval:  time.Hour,
		NewTicker: func(time.Duration) Ticker { return ticker },
		RunCycle: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
		IsFatal: func(error) bool { return false },
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	require.Eventually(t, func() bool { return atomic.LoadInt32(&calls) == 1 }, time.Second, time.Millisecond)

	cancel()
	require.NoError(t, <-done)
}

func TestRun_RunsAgainOnEachTick(t *testing.T) {
	var calls int32
	ticker := newFakeTicker()

	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		Interval:  time.Hour,
		NewTicker: func(time.Duration) Ticker { return ticker },
		RunCycle: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
		IsFatal: func(error) bool { return false },
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	require.Eventually(t, func() bool { return atomic.LoadInt32(&calls) == 1 }, time.Second, time.Millisecond)
	ticker.tick()
	require.Eventually(t, func() bool { return atomic.LoadInt32(&calls) == 2 }, time.Second, time.Millisecond)
	ticker.tick()
	require.Eventually(t, func() bool { return atomic.LoadInt32(&calls) == 3 }, time.Second, time.Millisecond)

	cancel()
	require.NoError(t, <-done)
}

func TestRun_FatalErrorStopsLoopAndIsReturned(t *testing.T) {
	ticker := newFakeTicker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &Runner{
		Interval:  time.Hour,
		NewTicker: func(time.Duration) Ticker { return ticker },
		RunCycle: func(ctx context.Context) error {
			return errFatalForTest
		},
		IsFatal: func(err error) bool { return errors.Is(err, errFatalForTest) },
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, errFatalForTest)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after a fatal error")
	}
}

func TestRun_NonFatalErrorContinuesLoopAndCallsOnError(t *testing.T) {
	errNonFatal := errors.New("transient")
	ticker := newFakeTicker()
	ctx, cancel := context.WithCancel(context.Background())

	var calls int32
	var onErrorCalls int32
	r := &Runner{
		Interval:  time.Hour,
		NewTicker: func(time.Duration) Ticker { return ticker },
		RunCycle: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return errNonFatal
		},
		IsFatal: func(error) bool { return false },
		OnError: func(err error) { atomic.AddInt32(&onErrorCalls, 1) },
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	require.Eventually(t, func() bool { return atomic.LoadInt32(&calls) == 1 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return atomic.LoadInt32(&onErrorCalls) == 1 }, time.Second, time.Millisecond)

	ticker.tick()
	require.Eventually(t, func() bool { return atomic.LoadInt32(&calls) == 2 }, time.Second, time.Millisecond)

	cancel()
	require.NoError(t, <-done)
}

func TestRun_ContextCancelBetweenTicksStopsLoopCleanly(t *testing.T) {
	ticker := newFakeTicker()
	ctx, cancel := context.WithCancel(context.Background())

	r := &Runner{
		Interval:  time.Hour,
		NewTicker: func(time.Duration) Ticker { return ticker },
		RunCycle:  func(ctx context.Context) error { return nil },
		IsFatal:   func(error) bool { return false },
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	time.Sleep(20 * time.Millisecond) // let the immediate first cycle finish, loop now waiting on ticker
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func TestRun_GracefulShutdown_LetsInFlightCycleFinishWithinTimeout(t *testing.T) {
	ticker := newFakeTicker()
	ctx, cancel := context.WithCancel(context.Background())

	cycleStarted := make(chan struct{})
	releaseCycle := make(chan struct{})
	var cycleFinished int32

	r := &Runner{
		Interval:        time.Hour,
		ShutdownTimeout: time.Second,
		NewTicker:       func(time.Duration) Ticker { return ticker },
		RunCycle: func(ctx context.Context) error {
			close(cycleStarted)
			<-releaseCycle
			atomic.StoreInt32(&cycleFinished, 1)
			return nil
		},
		IsFatal: func(error) bool { return false },
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	<-cycleStarted
	cancel() // shutdown requested while the first (immediate) cycle is still running

	// Run must NOT return yet - the cycle hasn't finished and we're well
	// within ShutdownTimeout.
	select {
	case <-done:
		t.Fatal("Run returned before the in-flight cycle finished")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseCycle)

	select {
	case err := <-done:
		require.NoError(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&cycleFinished))
	case <-time.After(time.Second):
		t.Fatal("Run did not return after the in-flight cycle finished")
	}
}

func TestRun_GracefulShutdown_GivesUpAfterTimeout(t *testing.T) {
	ticker := newFakeTicker()
	ctx, cancel := context.WithCancel(context.Background())

	cycleStarted := make(chan struct{})
	neverRelease := make(chan struct{})

	r := &Runner{
		Interval:        time.Hour,
		ShutdownTimeout: 50 * time.Millisecond,
		NewTicker:       func(time.Duration) Ticker { return ticker },
		RunCycle: func(ctx context.Context) error {
			close(cycleStarted)
			<-neverRelease
			return nil
		},
		IsFatal: func(error) bool { return false },
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	<-cycleStarted
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not give up after ShutdownTimeout")
	}
}
