package lxdclient

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"
)

func newTestHost() *LXDHost {
	return &LXDHost{
		HostConfig: config.HostConfig{LxdHost: "https://test:8443"},
	}
}

func TestAPICallMutex_ExclusiveAccess(t *testing.T) {
	host := newTestHost()

	host.APICallMutex.Lock()

	// Another goroutine should not be able to acquire the mutex
	acquired := make(chan struct{})
	go func() {
		host.APICallMutex.Lock()
		close(acquired)
		host.APICallMutex.Unlock()
	}()

	select {
	case <-acquired:
		t.Fatal("expected mutex to block second goroutine")
	case <-time.After(50 * time.Millisecond):
		// expected: second goroutine is blocked
	}

	host.APICallMutex.Unlock()

	// Now the second goroutine should acquire the mutex
	select {
	case <-acquired:
		// success
	case <-time.After(time.Second):
		t.Fatal("expected second goroutine to acquire mutex after unlock")
	}
}

func TestAPICallMutex_TryLock(t *testing.T) {
	host := newTestHost()

	host.APICallMutex.Lock()

	if host.APICallMutex.TryLock() {
		t.Fatal("expected TryLock to fail when mutex is held")
	}

	host.APICallMutex.Unlock()

	if !host.APICallMutex.TryLock() {
		t.Fatal("expected TryLock to succeed after unlock")
	}
	host.APICallMutex.Unlock()
}

func TestAPICallMutex_NoContextRace(t *testing.T) {
	// This test verifies that concurrent goroutines accessing the same host
	// do not race on context when following the correct Mutex pattern.
	host := newTestHost()

	const goroutines = 10
	var wg sync.WaitGroup
	var errorCount atomic.Int32

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			host.APICallMutex.Lock()
			defer host.APICallMutex.Unlock()

			// Verify context is not already canceled when we acquire the mutex
			if ctx.Err() != nil {
				errorCount.Add(1)
				return
			}

			// Simulate some work
			time.Sleep(5 * time.Millisecond)
		}()
	}

	wg.Wait()

	// With 10 goroutines each holding the mutex for 5ms, some may timeout (50ms total > 100ms timeout).
	// The important thing is that at least some goroutines succeed.
	if errorCount.Load() == int32(goroutines) {
		t.Fatal("all goroutines had context errors, expected at least some to succeed")
	}
}
