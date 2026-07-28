package ipgeo

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"
)

type recordingSource struct {
	mockSource
	called bool
}

func (r *recordingSource) Lookup(_ context.Context, _ netip.Addr) (Result, error) {
	r.called = true
	return Result{IP: testAddr, Source: r.name, Country: "X"}, nil
}

func newRecordingSource(name string) *recordingSource {
	return &recordingSource{mockSource: mockSource{name: name}}
}

func TestLookup_RespectsCancelledContext(t *testing.T) {
	src := newRecordingSource("db")
	c := mustOpen(t, Wrap(src))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := c.Lookup(ctx, testAddr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Lookup() error = %v, want context.Canceled", err)
	}
	if !got.IsEmpty() {
		t.Errorf("Lookup() result = %#v, want zero Result", got)
	}
	if src.called {
		t.Error("source was called despite cancelled context")
	}
}

func TestLookupAll_RespectsCancelledContext(t *testing.T) {
	src1 := newRecordingSource("db1")
	src2 := newRecordingSource("db2")
	c := mustOpen(t, Wrap(src1), Wrap(src2))

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
	c := mustOpen(t, Wrap(src))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := c.LookupFrom(ctx, "db", testAddr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LookupFrom() error = %v, want context.Canceled", err)
	}
	if !got.IsEmpty() {
		t.Errorf("LookupFrom() result = %#v, want zero Result", got)
	}
	if src.called {
		t.Error("source was called despite cancelled context")
	}
}

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

func TestSingleflight_DoesNotPoisonConcurrentCaller(t *testing.T) {
	src := newBlockingSource("db", Result{IP: testAddr, Source: "db", Country: "US"}, nil)
	sf := newSingleflightSource(src)

	ctxA, cancelA := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelA()

	var aErr, bErr error
	var bResult Result
	var wg sync.WaitGroup

	wg.Go(func() {
		<-src.started
		time.Sleep(80 * time.Millisecond)
		close(src.release)
	})

	aStarted := make(chan struct{})
	wg.Go(func() {
		close(aStarted)
		_, aErr = sf.Lookup(ctxA, testAddr)
	})
	<-aStarted
	<-src.started

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
	if bResult.Country != "US" {
		t.Errorf("caller B result = %#v, want US", bResult)
	}
	if got := src.calls.Load(); got != 1 {
		t.Errorf("source called %d times, want 1", got)
	}
}
