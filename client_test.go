package ipgeo

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

var testAddr = netip.MustParseAddr("1.2.3.4")

type mockSource struct {
	name    string
	results map[netip.Addr]Result
	err     map[netip.Addr]error
	closed  bool
}

func newMockSource(name string) *mockSource {
	return &mockSource{
		name:    name,
		results: make(map[netip.Addr]Result),
		err:     make(map[netip.Addr]error),
	}
}

func (m *mockSource) Name() string { return m.name }
func (m *mockSource) Close() error { m.closed = true; return nil }

func (m *mockSource) Lookup(_ context.Context, addr netip.Addr) (Result, error) {
	if err, ok := m.err[addr]; ok {
		return Result{}, err
	}
	if r, ok := m.results[addr]; ok {
		return r, nil
	}
	return Result{}, ErrNotFound
}

func (m *mockSource) add(ip string, r Result) { //nolint:unparam
	addr := netip.MustParseAddr(ip)
	m.results[addr] = r
}

func (m *mockSource) addErr(err error) {
	m.err[testAddr] = err
}

func mustOpen(t *testing.T, creators ...SourceCreator) *Client {
	t.Helper()
	c, err := Open(creators...)
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestOpen_NoSources(t *testing.T) {
	_, err := Open()
	if err == nil {
		t.Fatal("expected error with no sources")
	}
}

func TestOpen_NilSource(t *testing.T) {
	_, err := Open(Wrap(nil))
	if err == nil {
		t.Fatal("expected error with nil source")
	}
}

func TestOpen_DuplicateName(t *testing.T) {
	src := newMockSource("db")
	_, err := Open(Wrap(src), Wrap(src))
	if err == nil {
		t.Fatal("expected error for duplicate source name")
	}
}

func TestOpen_DuplicateNameClosesAllSources(t *testing.T) {
	src1 := newMockSource("dup")
	src2 := newMockSource("dup")
	_, err := Open(Wrap(src1), Wrap(src2))
	if !errors.Is(err, ErrDuplicateSource) {
		t.Fatalf("Open() error = %v, want ErrDuplicateSource", err)
	}
	if !src1.closed {
		t.Fatal("src1 was not closed on duplicate detection")
	}
	if !src2.closed {
		t.Fatal("src2 was not closed on duplicate detection")
	}
}

func TestOpen_Success(t *testing.T) {
	mustOpen(t, Wrap(newMockSource("db")))
}

func TestLookup_Found(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", Result{IP: testAddr, Source: "db", Country: "China"})
	c := mustOpen(t, Wrap(src))

	got, err := c.Lookup(context.Background(), netip.MustParseAddr("1.2.3.4"))
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if got.Country != "China" {
		t.Errorf("Country = %q, want %q", got.Country, "China")
	}
}

func TestLookup_NotFound(t *testing.T) {
	c := mustOpen(t, Wrap(newMockSource("db")))

	got, err := c.Lookup(context.Background(), netip.MustParseAddr("1.2.3.4"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup() error = %v, want ErrNotFound", err)
	}
	if !got.IsEmpty() {
		t.Errorf("Lookup() result = %#v, want zero Result", got)
	}
}

func TestLookup_SourceError(t *testing.T) {
	src := newMockSource("db")
	sentinelErr := errors.New("db broken")
	src.addErr(sentinelErr)
	c := mustOpen(t, Wrap(src))

	_, err := c.Lookup(context.Background(), netip.MustParseAddr("1.2.3.4"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("err = %v, want to wrap sentinelErr", err)
	}
}

func TestLookup_FallsThrough(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src2.add("1.2.3.4", Result{IP: testAddr, Source: "db2", Country: "US"})
	c := mustOpen(t, Wrap(src1), Wrap(src2))

	got, err := c.Lookup(context.Background(), netip.MustParseAddr("1.2.3.4"))
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if got.Source != "db2" {
		t.Errorf("Source = %q, want %q", got.Source, "db2")
	}
}

func TestLookup_IPv4MappedIPv6(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", Result{IP: testAddr, Country: "China"})
	c := mustOpen(t, Wrap(src))

	mapped := netip.MustParseAddr("::ffff:1.2.3.4")
	got, err := c.Lookup(context.Background(), mapped)
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if got.Country != "China" {
		t.Errorf("Country = %q, want %q", got.Country, "China")
	}
}

func TestLookup_CacheHit(t *testing.T) {
	src := newMockSource("db")
	callCount := 0
	result := Result{IP: testAddr, Country: "China"}
	src.results[testAddr] = result

	counting := &countingSource{Source: src, counter: &callCount}
	c := mustOpen(t, Wrap(counting).Decorate(Cache(10, 0, 0)))

	addr := netip.MustParseAddr("1.2.3.4")
	_, _ = c.Lookup(context.Background(), addr)
	_, _ = c.Lookup(context.Background(), addr)
	_, _ = c.Lookup(context.Background(), addr)

	if callCount != 1 {
		t.Errorf("source called %d times, want 1 (cache should hit)", callCount)
	}
}

func TestLookup_CachesNilMissFallthrough(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src2.add("1.2.3.4", Result{IP: testAddr, Source: "db2", Country: "US"})
	src1Count := 0
	src2Count := 0

	c := mustOpen(t,
		Wrap(&countingSource{Source: src1, counter: &src1Count}).Decorate(Cache(10, 0, time.Second)),
		Wrap(&countingSource{Source: src2, counter: &src2Count}).Decorate(Cache(10, 0, time.Second)),
	)

	for range 3 {
		got, err := c.Lookup(context.Background(), testAddr)
		if err != nil {
			t.Fatalf("Lookup() error: %v", err)
		}
		if got.Source != "db2" {
			t.Fatalf("Source = %q, want db2", got.Source)
		}
	}

	if src1Count != 1 {
		t.Errorf("db1 called %d times, want 1 (cached nil miss)", src1Count)
	}
	if src2Count != 1 {
		t.Errorf("db2 called %d times, want 1 (cached success)", src2Count)
	}
}

type countingSource struct {
	Source
	counter *int
}

func (c *countingSource) Lookup(ctx context.Context, addr netip.Addr) (Result, error) {
	*c.counter++
	return c.Source.Lookup(ctx, addr)
}

func TestLookupAll_MultiSource(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src1.add("1.2.3.4", Result{IP: testAddr, Country: "China"})
	src2.add("1.2.3.4", Result{IP: testAddr, Country: "China2"})
	c := mustOpen(t, Wrap(src1), Wrap(src2))

	results, err := c.LookupAll(context.Background(), testAddr)
	if err != nil {
		t.Fatalf("LookupAll() error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("len(results) = %d, want 2", len(results))
	}
}

func TestLookupAll_NoneFound(t *testing.T) {
	c := mustOpen(t, Wrap(newMockSource("db")))

	results, err := c.LookupAll(context.Background(), testAddr)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("LookupAll() error = %v, want ErrNotFound", err)
	}
	if results != nil {
		t.Errorf("LookupAll() results = %#v, want nil", results)
	}
}

func TestLookupAll_PartialErrors(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src1.addErr(errors.New("broken"))
	src2.add("1.2.3.4", Result{IP: testAddr, Country: "China"})
	c := mustOpen(t, Wrap(src1), Wrap(src2))

	results, err := c.LookupAll(context.Background(), testAddr)
	if err == nil {
		t.Fatal("expected non-nil error from src1")
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Country != "China" {
		t.Errorf("results[0].Country = %q, want %q", results[0].Country, "China")
	}
}

func TestLookupAll_CacheHitPerSource(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src1.add("1.2.3.4", Result{IP: testAddr, Source: "db1", Country: "China"})
	src2.add("1.2.3.4", Result{IP: testAddr, Source: "db2", Country: "US"})
	src1Count := 0
	src2Count := 0

	c := mustOpen(t,
		Wrap(&countingSource{Source: src1, counter: &src1Count}).Decorate(Cache(10, 0, time.Second)),
		Wrap(&countingSource{Source: src2, counter: &src2Count}).Decorate(Cache(10, 0, time.Second)),
	)

	for range 2 {
		results, err := c.LookupAll(context.Background(), testAddr)
		if err != nil {
			t.Fatalf("LookupAll() error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("len(results) = %d, want 2", len(results))
		}
	}

	if src1Count != 1 {
		t.Errorf("db1 called %d times, want 1", src1Count)
	}
	if src2Count != 1 {
		t.Errorf("db2 called %d times, want 1", src2Count)
	}
}

func TestLookupAll_CachesErrors(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	sentinelErr := errors.New("broken")
	src1.addErr(sentinelErr)
	src1Count := 0
	src2Count := 0

	c := mustOpen(t,
		Wrap(&countingSource{Source: src1, counter: &src1Count}).Decorate(Cache(10, 0, time.Second)),
		Wrap(&countingSource{Source: src2, counter: &src2Count}).Decorate(Cache(10, 0, time.Second)),
	)

	for range 2 {
		results, err := c.LookupAll(context.Background(), testAddr)
		if !errors.Is(err, sentinelErr) {
			t.Fatalf("err = %v, want sentinel error", err)
		}
		if len(results) != 0 {
			t.Fatalf("len(results) = %d, want 0", len(results))
		}
	}

	if src1Count != 1 {
		t.Errorf("db1 called %d times, want 1 (cached error)", src1Count)
	}
	if src2Count != 1 {
		t.Errorf("db2 called %d times, want 1 (cached not-found result)", src2Count)
	}
}

func TestLookupFrom_Found(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src2.add("1.2.3.4", Result{IP: testAddr, Country: "US"})
	c := mustOpen(t, Wrap(src1), Wrap(src2))

	got, err := c.LookupFrom(context.Background(), "db2", testAddr)
	if err != nil {
		t.Fatalf("LookupFrom() error: %v", err)
	}
	if got.Country != "US" {
		t.Errorf("Country = %q, want US", got.Country)
	}
}

func TestLookupFrom_UnknownSource(t *testing.T) {
	c := mustOpen(t, Wrap(newMockSource("db")))

	_, err := c.LookupFrom(context.Background(), "nonexistent", testAddr)
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestLookupFrom_NotFound(t *testing.T) {
	c := mustOpen(t, Wrap(newMockSource("db")))

	got, err := c.LookupFrom(context.Background(), "db", testAddr)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("LookupFrom() error = %v, want ErrNotFound", err)
	}
	if !got.IsEmpty() {
		t.Fatalf("LookupFrom() result = %#v, want zero Result", got)
	}
}

func TestLookupFrom_CacheHit(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src2.add("1.2.3.4", Result{IP: testAddr, Country: "US"})
	src1Count := 0
	src2Count := 0

	c := mustOpen(t,
		Wrap(&countingSource{Source: src1, counter: &src1Count}).Decorate(Cache(10, 0, 0)),
		Wrap(&countingSource{Source: src2, counter: &src2Count}).Decorate(Cache(10, 0, 0)),
	)

	for range 3 {
		got, err := c.LookupFrom(context.Background(), "db2", testAddr)
		if err != nil {
			t.Fatalf("LookupFrom() error: %v", err)
		}
		if got.Country != "US" {
			t.Fatalf("Country = %q, want US", got.Country)
		}
	}

	if src1Count != 0 {
		t.Errorf("db1 called %d times, want 0", src1Count)
	}
	if src2Count != 1 {
		t.Errorf("db2 called %d times, want 1", src2Count)
	}
}

func TestLookupFrom_DoesNotCacheErrorWithoutTTL(t *testing.T) {
	src := newMockSource("db")
	sentinelErr := errors.New("broken")
	src.addErr(sentinelErr)
	callCount := 0

	c := mustOpen(t, Wrap(&countingSource{Source: src, counter: &callCount}).Decorate(Cache(10, 0, 0)))

	for range 2 {
		_, err := c.LookupFrom(context.Background(), "db", testAddr)
		if !errors.Is(err, sentinelErr) {
			t.Fatalf("err = %v, want sentinel error", err)
		}
	}

	if callCount != 2 {
		t.Errorf("source called %d times, want 2 without error TTL", callCount)
	}
}

func TestLookupFrom_CachesErrorWithTTL(t *testing.T) {
	src := newMockSource("db")
	sentinelErr := errors.New("broken")
	src.addErr(sentinelErr)
	callCount := 0

	c := mustOpen(t, Wrap(&countingSource{Source: src, counter: &callCount}).Decorate(Cache(10, 0, time.Second)))

	for range 2 {
		_, err := c.LookupFrom(context.Background(), "db", testAddr)
		if !errors.Is(err, sentinelErr) {
			t.Fatalf("err = %v, want sentinel error", err)
		}
	}

	if callCount != 1 {
		t.Errorf("source called %d times, want 1 with error TTL", callCount)
	}
}

func TestLookupFrom_CacheErrorExpires(t *testing.T) {
	src := newMockSource("db")
	sentinelErr := errors.New("broken")
	src.addErr(sentinelErr)
	callCount := 0

	c := mustOpen(t,
		Wrap(&countingSource{Source: src, counter: &callCount}).Decorate(Cache(10, 0, 20*time.Millisecond)),
	)

	_, _ = c.LookupFrom(context.Background(), "db", testAddr)
	_, _ = c.LookupFrom(context.Background(), "db", testAddr)
	if callCount != 1 {
		t.Fatalf("source called %d times, want 1 before TTL expiry", callCount)
	}

	time.Sleep(30 * time.Millisecond)
	_, err := c.LookupFrom(context.Background(), "db", testAddr)
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("err = %v, want sentinel error", err)
	}
	if callCount != 2 {
		t.Errorf("source called %d times, want 2 after TTL expiry", callCount)
	}
}

func TestLookupFrom_CacheErrorCanBeDisabled(t *testing.T) {
	src := newMockSource("db")
	sentinelErr := errors.New("broken")
	src.addErr(sentinelErr)
	callCount := 0

	c := mustOpen(t,
		Wrap(&countingSource{Source: src, counter: &callCount}).Decorate(Cache(10, 0, 0)),
	)

	for range 2 {
		_, err := c.LookupFrom(context.Background(), "db", testAddr)
		if !errors.Is(err, sentinelErr) {
			t.Fatalf("err = %v, want sentinel error", err)
		}
	}

	if callCount != 2 {
		t.Errorf("source called %d times, want 2 when error cache is disabled", callCount)
	}
}

func TestLookupFrom_SingleflightWithoutCache(t *testing.T) {
	src := newBlockingSource("db", Result{IP: testAddr, Country: "US"}, nil)
	c := mustOpen(t, Wrap(src).Decorate(Singleflight()))

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
			got, err := c.LookupFrom(context.Background(), "db", testAddr)
			if err != nil {
				errs <- err
				return
			}
			if got.Country != "US" {
				errs <- errors.New("unexpected country")
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

	got, err := c.LookupFrom(context.Background(), "db", testAddr)
	if err != nil {
		t.Fatalf("LookupFrom() after singleflight error: %v", err)
	}
	if got.Country != "US" {
		t.Fatalf("Country = %q, want US", got.Country)
	}
	if got := src.calls.Load(); got != 2 {
		t.Errorf("source called %d times, want 2 after sequential no-cache retry", got)
	}
}

func TestLookupFrom_SingleflightCacheMiss(t *testing.T) {
	src := newBlockingSource("db", Result{IP: testAddr, Country: "US"}, nil)
	c := mustOpen(t, Wrap(src).Decorate(Singleflight()).Decorate(Cache(10, 0, 0)))

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
			got, err := c.LookupFrom(context.Background(), "db", testAddr)
			if err != nil {
				errs <- err
				return
			}
			if got.Country != "US" {
				errs <- errors.New("unexpected country")
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

	_, _ = c.LookupFrom(context.Background(), "db", testAddr)
	if got := src.calls.Load(); got != 1 {
		t.Errorf("source called %d times, want 1 after cache hit", got)
	}
}

func TestLookupFrom_SingleflightErrorMiss(t *testing.T) {
	sentinelErr := errors.New("broken")
	src := newBlockingSource("db", Result{}, sentinelErr)
	c := mustOpen(t, Wrap(src).Decorate(Singleflight()).Decorate(Cache(10, 0, 0)))

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
			_, err := c.LookupFrom(context.Background(), "db", testAddr)
			if !errors.Is(err, sentinelErr) {
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
		t.Errorf("concurrent lookup error = %v, want sentinel error", err)
	}
	if got := src.calls.Load(); got != 1 {
		t.Fatalf("source called %d times, want 1 for concurrent misses", got)
	}

	_, err := c.LookupFrom(context.Background(), "db", testAddr)
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("err = %v, want sentinel error", err)
	}
	if got := src.calls.Load(); got != 2 {
		t.Errorf("source called %d times, want 2 after disabled error cache retry", got)
	}
}

type blockingSource struct {
	name    string
	result  Result
	err     error
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func newBlockingSource(name string, result Result, err error) *blockingSource { //nolint:unparam
	return &blockingSource{
		name:    name,
		result:  result,
		err:     err,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *blockingSource) Lookup(ctx context.Context, _ netip.Addr) (Result, error) {
	s.calls.Add(1)
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
		return s.result, s.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func (s *blockingSource) Name() string { return s.name }

func (s *blockingSource) Close() error { return nil }

func TestSourceNames(t *testing.T) {
	c := mustOpen(t, Wrap(newMockSource("alpha")), Wrap(newMockSource("beta")))

	names := c.SourceNames()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("SourceNames() = %v, want [alpha beta]", names)
	}
}

func TestWithCache_ValidSize(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", Result{IP: testAddr, Country: "China"})
	c := mustOpen(t, Wrap(src).Decorate(Cache(100, 0, 0)))

	_, err := c.Lookup(context.Background(), netip.MustParseAddr("1.2.3.4"))
	if err != nil {
		t.Errorf("Lookup() error: %v", err)
	}
}

func TestWithCache_ZeroSizeDisablesCache(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", Result{IP: testAddr, Country: "China"})
	callCount := 0
	counting := &countingSource{Source: src, counter: &callCount}

	c := mustOpen(t, Wrap(counting))

	for range 3 {
		_, _ = c.Lookup(context.Background(), testAddr)
	}
	if callCount != 3 {
		t.Errorf("source called %d times, want 3 (maxEntries=0 disables cache)", callCount)
	}
}

func TestWithCache_NegativeErrorTTL(t *testing.T) {
	src := newMockSource("db")
	_, err := Open(Wrap(src).Decorate(Cache(10, 0, -time.Second)))
	if err == nil {
		t.Fatal("expected error for negative error cache TTL")
	}
}

func TestWithCache_NegativeResultTTL(t *testing.T) {
	src := newMockSource("db")
	_, err := Open(Wrap(src).Decorate(Cache(10, -time.Second, 0)))
	if err == nil {
		t.Fatal("expected error for negative result cache TTL")
	}
}

func TestWithCache_ZeroErrorTTL(t *testing.T) {
	src := newMockSource("db")
	c, err := Open(Wrap(src).Decorate(Cache(10, 0, 0)))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
}

func TestWithCache_EvictsCachedEntry(t *testing.T) {
	src := newMockSource("db")
	callCount := 0
	counting := &countingSource{Source: src, counter: &callCount}

	c := mustOpen(t, Wrap(counting).Decorate(Cache(1, 0, 0)))

	addr1 := netip.MustParseAddr("1.1.1.1")
	addr2 := netip.MustParseAddr("2.2.2.2")
	src.add("1.1.1.1", Result{IP: addr1, Country: "A"})
	src.add("2.2.2.2", Result{IP: addr2, Country: "B"})

	_, _ = c.Lookup(context.Background(), addr1) // Cache addr1
	_, _ = c.Lookup(context.Background(), addr2) // Cache addr2, evicts addr1
	_, _ = c.Lookup(context.Background(), addr1) // Miss, calls source again

	if callCount != 3 {
		t.Errorf("source called %d times, want 3 (cache size 1 should evict)", callCount)
	}
}

func TestWithCache_ResultTTLExpires(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", Result{IP: testAddr, Country: "China"})
	callCount := 0

	c := mustOpen(t,
		Wrap(&countingSource{Source: src, counter: &callCount}).Decorate(Cache(10, 20*time.Millisecond, 0)),
	)

	_, _ = c.Lookup(context.Background(), testAddr)
	_, _ = c.Lookup(context.Background(), testAddr)
	if callCount != 1 {
		t.Fatalf("source called %d times before TTL expiry, want 1", callCount)
	}

	time.Sleep(30 * time.Millisecond)
	_, _ = c.Lookup(context.Background(), testAddr)
	if callCount != 2 {
		t.Errorf("source called %d times after TTL expiry, want 2", callCount)
	}
}

func TestWithCache_ResultTTLZeroIsPermanent(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", Result{IP: testAddr, Country: "China"})
	callCount := 0

	c := mustOpen(t,
		Wrap(&countingSource{Source: src, counter: &callCount}).Decorate(Cache(10, 0, 0)),
	)

	for range 3 {
		_, _ = c.Lookup(context.Background(), testAddr)
	}
	if callCount != 1 {
		t.Errorf("source called %d times, want 1 (ResultTTL=0 is permanent)", callCount)
	}
}

func TestWithCache_ResultTTLIsSlidingWindow(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", Result{IP: testAddr, Country: "China"})
	callCount := 0

	c := mustOpen(t,
		Wrap(&countingSource{Source: src, counter: &callCount}).Decorate(Cache(10, 20*time.Millisecond, 0)),
	)

	_, _ = c.Lookup(context.Background(), testAddr) // t=0, caches with 20ms TTL
	if callCount != 1 {
		t.Fatalf("source called %d times, want 1", callCount)
	}

	time.Sleep(10 * time.Millisecond)
	_, _ = c.Lookup(context.Background(), testAddr) // t=10ms, cache hit resets TTL to 30ms
	if callCount != 1 {
		t.Fatalf("source called %d times after hit within TTL, want 1", callCount)
	}

	time.Sleep(15 * time.Millisecond) // t=25ms, past original 20ms deadline but before 30ms
	_, _ = c.Lookup(context.Background(), testAddr)
	if callCount != 1 {
		t.Errorf("source called %d times at t=25ms, want 1 (sliding window extended TTL past original deadline)", callCount)
	}

	time.Sleep(20 * time.Millisecond) // t=45ms, well past 30ms deadline
	_, _ = c.Lookup(context.Background(), testAddr)
	if callCount != 2 {
		t.Errorf("source called %d times after TTL expiry, want 2", callCount)
	}
}

func TestClose_ClosesAllSources(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	c, err := Open(Wrap(src1), Wrap(src2))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if !src1.closed {
		t.Error("src1 was not closed")
	}
	if !src2.closed {
		t.Error("src2 was not closed")
	}
}

func TestClose_PurgesCache(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", Result{IP: testAddr, Country: "China"})
	// Don't use mustOpen here — we call Close() manually to inspect state.
	c, err := Open(Wrap(src).Decorate(Singleflight()).Decorate(Cache(10, 0, time.Second)))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	cached, ok := c.sources[0].(*cachedSource)
	if !ok {
		t.Fatalf("source type = %T, want *cachedSource", c.sources[0])
	}
	if _, ok := cached.source.(*singleflightSource); !ok {
		t.Fatalf("cached source wraps %T, want *singleflightSource", cached.source)
	}

	_, _ = c.Lookup(context.Background(), netip.MustParseAddr("1.2.3.4"))
	if cached.results.Len() != 1 {
		t.Fatalf("cache len = %d, want 1 before Close()", cached.results.Len())
	}
	cached.errors.Set(netip.MustParseAddr("2.2.2.2"), errors.New("broken"), ttlcache.DefaultTTL)
	if cached.errors.Len() != 1 {
		t.Fatalf("error cache len = %d, want 1 before Close()", cached.errors.Len())
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if cached.results.Len() != 0 {
		t.Errorf("cache len = %d, want 0 after Close()", cached.results.Len())
	}
	if cached.errors.Len() != 0 {
		t.Errorf("error cache len = %d, want 0 after Close()", cached.errors.Len())
	}
	if !src.closed {
		t.Error("wrapped source was not closed")
	}
}

func TestErrorCacheExpiresLazily(t *testing.T) {
	src := newMockSource("db")
	lookupErr := errors.New("temporary failure")
	src.addErr(lookupErr)
	var callCount int
	c := mustOpen(t, Wrap(&countingSource{Source: src, counter: &callCount}).Decorate(Cache(10, 0, 20*time.Millisecond)))

	if _, err := c.Lookup(context.Background(), testAddr); !errors.Is(err, lookupErr) {
		t.Fatalf("first Lookup() error = %v, want lookupErr", err)
	}
	if _, err := c.Lookup(context.Background(), testAddr); !errors.Is(err, lookupErr) {
		t.Fatalf("cached Lookup() error = %v, want lookupErr", err)
	}
	if callCount != 1 {
		t.Fatalf("source called %d times before TTL expiry, want 1", callCount)
	}

	time.Sleep(30 * time.Millisecond)
	if _, err := c.Lookup(context.Background(), testAddr); !errors.Is(err, lookupErr) {
		t.Fatalf("post-expiry Lookup() error = %v, want lookupErr", err)
	}
	if callCount != 2 {
		t.Fatalf("source called %d times after TTL expiry, want 2", callCount)
	}
}

type countCloseSource struct {
	*mockSource
	closes int
	err    error
}

func (s *countCloseSource) Close() error {
	s.closes++
	return s.err
}

func TestClose_Idempotent(t *testing.T) {
	closeErr := errors.New("close failed")
	src := &countCloseSource{mockSource: newMockSource("db"), err: closeErr}
	c, err := Open(Wrap(src))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}

	err1 := c.Close()
	err2 := c.Close()
	if !errors.Is(err1, closeErr) {
		t.Fatalf("first Close() = %v, want closeErr", err1)
	}
	if !errors.Is(err2, closeErr) {
		t.Errorf("second Close() = %v, want same closeErr (idempotent)", err2)
	}
	if src.closes != 1 {
		t.Errorf("source closed %d times, want 1", src.closes)
	}
}

func TestClose_ConcurrentSafe(t *testing.T) {
	src := &countCloseSource{mockSource: newMockSource("db")}
	c, err := Open(Wrap(src))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() { _ = c.Close() })
	}
	wg.Wait()

	if src.closes != 1 {
		t.Errorf("source closed %d times, want 1 under concurrent Close", src.closes)
	}
}
