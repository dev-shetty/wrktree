package git

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Calls with one key that come during a call share that call.
func TestFlightSharesOneCall(t *testing.T) {
	var f flight
	var runs atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	want := errors.New("result")
	fn := func() error {
		if runs.Add(1) == 1 {
			close(started)
		}
		<-release
		return want
	}
	var wg sync.WaitGroup
	errs := make([]error, 5)
	wg.Add(1)
	go func() { defer wg.Done(); errs[0] = f.do(context.Background(), "k", fn) }()
	<-started
	for i := 1; i < len(errs); i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = f.do(context.Background(), "k", fn) }()
	}
	// Give the other callers time to find the call that runs.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := runs.Load(); n != 1 {
		t.Errorf("fn ran %d times, want 1", n)
	}
	for i, err := range errs {
		if err != want {
			t.Errorf("caller %d got %v, want %v", i, err, want)
		}
	}
	// The key is free again.
	if err := f.do(context.Background(), "k", func() error { return nil }); err != nil || runs.Load() != 1 {
		t.Errorf("call after the end: err %v, runs %d", err, runs.Load())
	}
}

// A different key does not wait.
func TestFlightOtherKey(t *testing.T) {
	var f flight
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.do(context.Background(), "a", func() error { close(started); <-release; return nil })
	}()
	<-started
	if err := f.do(context.Background(), "b", func() error { return nil }); err != nil {
		t.Error(err)
	}
	close(release)
	<-done
}

// A caller that waits stops at the end of its context.
func TestFlightWaiterContext(t *testing.T) {
	var f flight
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.do(context.Background(), "k", func() error { close(started); <-release; return nil })
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.do(ctx, "k", func() error { t.Error("fn must not run"); return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	close(release)
	<-done
}
