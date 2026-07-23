package ipgeo

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreate_ReturnsBaseSource(t *testing.T) {
	src := newMockSource("db")
	creator := Wrap(src)

	got, err := creator.Create()
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if got != src {
		t.Fatalf("Create() = %p, want %p", got, src)
	}
}

func TestCreate_WrapNilReturnsError(t *testing.T) {
	_, err := Wrap(nil).Create()
	if err == nil {
		t.Fatal("Create() error = nil, want error for nil source")
	}
}

func TestDecorate_AppliesInOrder(t *testing.T) {
	src := newMockSource("db")
	var order []string

	creator := Wrap(src).
		Decorate(func(s Source) (Source, error) {
			order = append(order, "first")
			return &decoratorSpy{Source: s, name: "first"}, nil
		}).
		Decorate(func(s Source) (Source, error) {
			order = append(order, "second")
			return &decoratorSpy{Source: s, name: "second"}, nil
		})

	got, err := creator.Create()
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("decorator order = %v, want [first second]", order)
	}
	if got.Name() != "second" {
		t.Fatalf("outermost Name() = %q, want second", got.Name())
	}
}

func TestCreate_DecoratorErrorClosesSource(t *testing.T) {
	src := newMockSource("db")
	sentinelErr := errors.New("decorator broken")
	creator := Wrap(src).Decorate(func(Source) (Source, error) {
		return nil, sentinelErr
	})

	_, err := creator.Create()
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("Create() error = %v, want sentinelErr", err)
	}
	if !src.closed {
		t.Fatal("source was not closed after decorator error")
	}
}

func TestSingleflight_DeduplicatesConcurrentMisses(t *testing.T) {
	src := newBlockingSource("db", &Result{ip: testAddr, country: "US"}, nil)
	sf, err := Wrap(src).Decorate(Singleflight()).Create()
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	var ready sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, goroutines)
	ready.Add(goroutines)
	for range goroutines {
		wg.Go(func() {
			ready.Done()
			<-start
			_, err := sf.Lookup(context.Background(), testAddr)
			if err != nil {
				errs <- err
			}
		})
	}

	ready.Wait()
	close(start)
	<-src.started
	time.Sleep(20 * time.Millisecond)
	close(src.release)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent lookup error: %v", err)
	}
	if got := src.calls.Load(); got != 1 {
		t.Errorf("source called %d times, want 1", got)
	}
}

func TestCache_DecoratorValidationPropagates(t *testing.T) {
	tests := []struct {
		name       string
		maxEntries uint
		resultTTL  time.Duration
		errorTTL   time.Duration
	}{
		{name: "zero maxEntries", maxEntries: 0, resultTTL: 0, errorTTL: 0},
		{name: "negative resultTTL", maxEntries: 10, resultTTL: -time.Second, errorTTL: 0},
		{name: "negative errorTTL", maxEntries: 10, resultTTL: 0, errorTTL: -time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creator := Wrap(newMockSource("db")).Decorate(Cache(tt.maxEntries, tt.resultTTL, tt.errorTTL))
			_, err := creator.Create()
			if err == nil {
				t.Fatal("Create() error = nil, want validation error")
			}
		})
	}
}

func TestCache_DecoratorCachesResults(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", &Result{ip: testAddr, country: "China"})
	counting := &countingSource{Source: src, counter: new(int)}

	got, err := Wrap(counting).Decorate(Cache(10, 0, 0)).Create()
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	addr := netip.MustParseAddr("1.2.3.4")
	for range 3 {
		_, _ = got.Lookup(context.Background(), addr)
	}
	if *counting.counter != 1 {
		t.Errorf("source called %d times, want 1 (cache hit)", *counting.counter)
	}
}

func TestMMDB_CreatePropagatesOpenError(t *testing.T) {
	_, err := MMDB("mmdb", "testdata/missing-db", "").Create()
	if err == nil {
		t.Fatal("Create() error = nil, want error for missing file")
	}
	if !strings.Contains(err.Error(), "open mmdb mmdb:") {
		t.Fatalf("Create() error = %v, want substring 'open mmdb mmdb:'", err)
	}
}

type decoratorSpy struct {
	Source
	name string
}

func (d *decoratorSpy) Name() string { return d.name }
