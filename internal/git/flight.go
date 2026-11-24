package git

import (
	"context"
	"sync"
)

// flight runs one call for each key at one time. A caller that comes
// during a call with the same key waits for that call and gets its result.
type flight struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

type flightCall struct {
	done chan struct{}
	err  error
}

var fetches flight

// do runs fn, or waits for the call of key that runs now. A caller that
// waits stops at the end of ctx.
func (f *flight) do(ctx context.Context, key string, fn func() error) error {
	f.mu.Lock()
	if c, ok := f.calls[key]; ok {
		f.mu.Unlock()
		select {
		case <-c.done:
			return c.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c := &flightCall{done: make(chan struct{})}
	if f.calls == nil {
		f.calls = map[string]*flightCall{}
	}
	f.calls[key] = c
	f.mu.Unlock()

	c.err = fn()

	f.mu.Lock()
	delete(f.calls, key)
	f.mu.Unlock()
	close(c.done)
	return c.err
}
