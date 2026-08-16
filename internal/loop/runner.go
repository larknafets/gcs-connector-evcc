// Package loop drives the connector's daemon mode: an immediate first sync
// on start, then one sync per configured interval, with a graceful shutdown
// on SIGTERM/SIGINT that lets an in-flight cycle finish instead of hard-
// cancelling its HTTP requests.
package loop

import (
	"context"
	"errors"
	"time"
)

// ErrShutdownTimeout is returned by Run when a graceful shutdown was
// requested but the in-flight cycle did not finish within ShutdownTimeout.
// Run always stops on this error, regardless of Runner.IsFatal.
var ErrShutdownTimeout = errors.New("loop: in-flight cycle did not finish within shutdown timeout")

// Ticker is the subset of time.Ticker the Runner needs, so tests can supply
// a fake instead of waiting on real wall-clock time.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct{ t *time.Ticker }

func (r *realTicker) C() <-chan time.Time { return r.t.C }
func (r *realTicker) Stop()               { r.t.Stop() }

// NewRealTicker wraps a real time.Ticker as a Ticker.
func NewRealTicker(d time.Duration) Ticker {
	return &realTicker{t: time.NewTicker(d)}
}

// defaultShutdownTimeout bounds how long Run waits for an in-flight cycle to
// finish once shutdown has been requested, if Runner.ShutdownTimeout is unset.
const defaultShutdownTimeout = 30 * time.Second

// Runner drives RunCycle on an immediate-then-interval schedule.
type Runner struct {
	Interval        time.Duration
	ShutdownTimeout time.Duration
	// NewTicker defaults to NewRealTicker if nil.
	NewTicker func(time.Duration) Ticker
	// RunCycle performs one sync cycle. It always receives a background
	// context (not the outer, cancelable one) so a graceful shutdown never
	// hard-cancels its in-flight HTTP requests.
	RunCycle func(ctx context.Context) error
	// IsFatal classifies a RunCycle error as fatal (Run stops and returns
	// it) versus transient (Run logs via OnError and keeps going).
	IsFatal func(error) bool
	// OnError is called for every non-fatal RunCycle error. Optional.
	OnError func(error)
	// Trigger, if set, lets external code (e.g. the optional webhook
	// listener) request an out-of-band cycle immediately, without waiting
	// for the next tick. A nil Trigger simply never fires, so leaving it
	// unset keeps the interval-only behavior.
	Trigger <-chan struct{}
}

// Run blocks until ctx is canceled (returns nil) or RunCycle returns a fatal
// error (returns that error).
func (r *Runner) Run(ctx context.Context) error {
	newTicker := r.NewTicker
	if newTicker == nil {
		newTicker = NewRealTicker
	}
	ticker := newTicker(r.Interval)
	defer ticker.Stop()

	if fatalErr, stop := r.runOnceAndClassify(ctx); stop {
		return fatalErr
	}
	if ctx.Err() != nil {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C():
			if fatalErr, stop := r.runOnceAndClassify(ctx); stop {
				return fatalErr
			}
			if ctx.Err() != nil {
				return nil
			}
		case <-r.Trigger:
			if fatalErr, stop := r.runOnceAndClassify(ctx); stop {
				return fatalErr
			}
			if ctx.Err() != nil {
				return nil
			}
		}
	}
}

// runOnceAndClassify runs one cycle (respecting graceful shutdown) and
// reports whether Run should stop because the result was fatal or because
// shutdown gave up waiting on an in-flight cycle.
func (r *Runner) runOnceAndClassify(ctx context.Context) (stopErr error, stop bool) {
	err := r.runOnce(ctx)
	if err == nil {
		return nil, false
	}
	if errors.Is(err, ErrShutdownTimeout) {
		return err, true
	}
	if r.IsFatal != nil && r.IsFatal(err) {
		return err, true
	}
	if r.OnError != nil {
		r.OnError(err)
	}
	return nil, false
}

// runOnce executes RunCycle against a background context, so cancelling ctx
// (a shutdown signal) never aborts in-flight HTTP requests mid-cycle. If a
// shutdown is requested while the cycle is running, runOnce waits up to
// ShutdownTimeout for it to finish before giving up and returning ctx.Err().
func (r *Runner) runOnce(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- r.RunCycle(context.Background())
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		timeout := r.ShutdownTimeout
		if timeout <= 0 {
			timeout = defaultShutdownTimeout
		}
		select {
		case err := <-done:
			return err
		case <-time.After(timeout):
			return ErrShutdownTimeout
		}
	}
}
