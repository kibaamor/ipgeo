package ipgeo

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// recordingSource records whether Lookup was called.
type recordingSource struct {
	mockSource
	called bool
}

func (r *recordingSource) Lookup(_ context.Context, _ netip.Addr) (*Result, error) {
	r.called = true
	return &Result{ip: testAddr, source: r.name, country: "X"}, nil
}

func newRecordingSource(name string) *recordingSource {
	return &recordingSource{mockSource: mockSource{name: name}}
}

func TestLookup_RespectsCancelledContext(t *testing.T) {
	src := newRecordingSource("db")
	c := mustOpen(t, WithSource(src))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := c.Lookup(ctx, testAddr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Lookup() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Errorf("Lookup() result = %#v, want nil", got)
	}
	if src.called {
		t.Error("source was called despite cancelled context")
	}
}

func TestLookupAll_RespectsCancelledContext(t *testing.T) {
	src1 := newRecordingSource("db1")
	src2 := newRecordingSource("db2")
	c := mustOpen(t, WithSource(src1), WithSource(src2))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := c.LookupAll(ctx, testAddr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LookupAll() error = %v, want context.Canceled", err)
	}
	if results != nil {
		t.Errorf("LookupAll() results = %#v, want nil", results)
	}
	if src1.called || src2.called {
		t.Error("a source was called despite cancelled context")
	}
}

func TestLookupFrom_RespectsCancelledContext(t *testing.T) {
	src := newRecordingSource("db")
	c := mustOpen(t, WithSource(src))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := c.LookupFrom(ctx, "db", testAddr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LookupFrom() error = %v, want context.Canceled", err)
	}
	if got != nil {
		t.Errorf("LookupFrom() result = %#v, want nil", got)
	}
	if src.called {
		t.Error("source was called despite cancelled context")
	}
}

// TestCachedSource_DoesNotCacheContextError ensures a context error returned by
// a source (e.g. a custom network source that respects ctx) is not cached, so a
// later lookup with a fresh context still queries the source.
func TestCachedSource_DoesNotCacheContextError(t *testing.T) {
	src := newMockSource("db")
	src.addErr(context.Canceled)
	var callCount int
	counting := &countingSource{Source: src, counter: &callCount}

	cached, err := newCachedSource(counting, 10, 0, time.Second)
	if err != nil {
		t.Fatalf("newCachedSource() error: %v", err)
	}

	_, err = cached.Lookup(context.Background(), testAddr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first Lookup() error = %v, want context.Canceled", err)
	}
	_, err = cached.Lookup(context.Background(), testAddr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("second Lookup() error = %v, want context.Canceled", err)
	}
	if callCount != 2 {
		t.Errorf("source called %d times, want 2 (context error must not be cached)", callCount)
	}
}

// TestSingleflight_DoesNotPoisonConcurrentCaller ensures a caller whose context
// expires mid-flight does not poison the shared lookup for a concurrent caller
// with a valid context.
func TestSingleflight_DoesNotPoisonConcurrentCaller(t *testing.T) {
	src := newBlockingSource("db", &Result{ip: testAddr, source: "db", country: "US"}, nil)
	sf := newSingleflightSource(src)

	// Caller A owns the shared lookup; its context expires well before release.
	ctxA, cancelA := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelA()

	var aErr, bErr error
	var bResult *Result
	var wg sync.WaitGroup

	// Release the shared lookup after ctxA has expired.
	wg.Go(func() {
		<-src.started
		time.Sleep(80 * time.Millisecond)
		close(src.release)
	})

	// Caller A starts first and triggers the shared lookup.
	aStarted := make(chan struct{})
	wg.Go(func() {
		close(aStarted)
		_, aErr = sf.Lookup(ctxA, testAddr)
	})
	<-aStarted
	<-src.started // A's lookup is in-flight as the first caller.

	// Caller B joins the in-flight shared lookup with a valid context.
	wg.Go(func() {
		bResult, bErr = sf.Lookup(context.Background(), testAddr)
	})

	wg.Wait()

	if !errors.Is(aErr, context.DeadlineExceeded) {
		t.Errorf("caller A error = %v, want DeadlineExceeded", aErr)
	}
	if bErr != nil {
		t.Errorf("caller B error = %v, want nil (must not inherit A's deadline)", bErr)
	}
	if bResult == nil || bResult.Country() != "US" {
		t.Errorf("caller B result = %#v, want US", bResult)
	}
	if got := src.calls.Load(); got != 1 {
		t.Errorf("source called %d times, want 1", got)
	}
}
