package ipgeo

import (
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

var testAddr = netip.MustParseAddr("1.2.3.4")

// mockSource is a test double for Source.
type mockSource struct {
	name    string
	results map[netip.Addr]*Result
	err     map[netip.Addr]error
	closed  bool
}

func newMockSource(name string) *mockSource {
	return &mockSource{
		name:    name,
		results: make(map[netip.Addr]*Result),
		err:     make(map[netip.Addr]error),
	}
}

func (m *mockSource) Name() string { return m.name }
func (m *mockSource) Close() error { m.closed = true; return nil }

func (m *mockSource) Lookup(addr netip.Addr) (*Result, error) {
	if err, ok := m.err[addr]; ok {
		return nil, err
	}
	if r, ok := m.results[addr]; ok {
		return r, nil
	}
	return nil, nil
}

func (m *mockSource) add(ip string, r *Result) { //nolint:unparam
	addr := netip.MustParseAddr(ip)
	m.results[addr] = r
}

func (m *mockSource) addErr(err error) {
	m.err[testAddr] = err
}

// mustOpen calls Open and fatals the test on error.
func mustOpen(t *testing.T, opts ...Option) *Client {
	t.Helper()
	c, err := Open(opts...)
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// ---- Open ----

func TestOpen_NoSources(t *testing.T) {
	_, err := Open()
	if err == nil {
		t.Fatal("expected error with no sources")
	}
}

func TestOpen_NilSource(t *testing.T) {
	_, err := Open(WithSource(nil))
	if err == nil {
		t.Fatal("expected error with nil source")
	}
}

func TestOpen_DuplicateName(t *testing.T) {
	src := newMockSource("db")
	_, err := Open(WithSource(src), WithSource(src))
	if err == nil {
		t.Fatal("expected error for duplicate source name")
	}
}

func TestOpen_Success(t *testing.T) {
	mustOpen(t, WithSource(newMockSource("db")))
}

// ---- Lookup ----

func TestLookup_Found(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", &Result{ip: testAddr, source: "db", country: "China"})
	c := mustOpen(t, WithSource(src))

	got, err := c.Lookup(netip.MustParseAddr("1.2.3.4"))
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if got.Country() != "China" {
		t.Errorf("Country = %q, want %q", got.Country(), "China")
	}
}

func TestLookup_NotFound(t *testing.T) {
	c := mustOpen(t, WithSource(newMockSource("db")))

	got, err := c.Lookup(netip.MustParseAddr("1.2.3.4"))
	if err != nil {
		t.Fatalf("Lookup() error = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("Lookup() result = %#v, want nil", got)
	}
}

func TestLookup_SourceError(t *testing.T) {
	src := newMockSource("db")
	sentinelErr := errors.New("db broken")
	src.addErr(sentinelErr)
	c := mustOpen(t, WithSource(src))

	_, err := c.Lookup(netip.MustParseAddr("1.2.3.4"))
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
	src2.add("1.2.3.4", &Result{ip: testAddr, source: "db2", country: "US"})
	c := mustOpen(t, WithSource(src1), WithSource(src2))

	got, err := c.Lookup(netip.MustParseAddr("1.2.3.4"))
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if got.Source() != "db2" {
		t.Errorf("Source = %q, want %q", got.Source(), "db2")
	}
}

// IPv4-mapped IPv6 should be unmapped before lookup
func TestLookup_IPv4MappedIPv6(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", &Result{ip: testAddr, country: "China"})
	c := mustOpen(t, WithSource(src))

	mapped := netip.MustParseAddr("::ffff:1.2.3.4")
	got, err := c.Lookup(mapped)
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if got.Country() != "China" {
		t.Errorf("Country = %q, want %q", got.Country(), "China")
	}
}

// ---- Lookup with cache ----

func TestLookup_CacheHit(t *testing.T) {
	src := newMockSource("db")
	callCount := 0
	result := &Result{ip: testAddr, country: "China"}
	src.results[testAddr] = result

	// Wrap to count calls
	counting := &countingSource{Source: src, counter: &callCount}
	c := mustOpen(t, WithSource(counting), WithCache(10, 0))

	addr := netip.MustParseAddr("1.2.3.4")
	_, _ = c.Lookup(addr)
	_, _ = c.Lookup(addr)
	_, _ = c.Lookup(addr)

	if callCount != 1 {
		t.Errorf("source called %d times, want 1 (cache should hit)", callCount)
	}
}

func TestLookup_CachesNilMissFallthrough(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src2.add("1.2.3.4", &Result{ip: testAddr, source: "db2", country: "US"})
	src1Count := 0
	src2Count := 0

	c := mustOpen(t,
		WithSource(&countingSource{Source: src1, counter: &src1Count}),
		WithSource(&countingSource{Source: src2, counter: &src2Count}),
		WithCache(10, time.Second),
	)

	for range 3 {
		got, err := c.Lookup(testAddr)
		if err != nil {
			t.Fatalf("Lookup() error: %v", err)
		}
		if got.Source() != "db2" {
			t.Fatalf("Source = %q, want db2", got.Source())
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

func (c *countingSource) Lookup(addr netip.Addr) (*Result, error) {
	*c.counter++
	return c.Source.Lookup(addr)
}

func TestWithCache_InvalidEntries(t *testing.T) {
	src := newMockSource("db")
	_, err := Open(WithSource(src), WithCache(0, 0))
	if err == nil {
		t.Fatal("expected error for maxEntries=0")
	}
	_, err = Open(WithSource(src), WithCache(-1, 0))
	if err == nil {
		t.Fatal("expected error for maxEntries=-1")
	}
}

// ---- LookupAll ----

func TestLookupAll_MultiSource(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src1.add("1.2.3.4", &Result{ip: testAddr, country: "China"})
	src2.add("1.2.3.4", &Result{ip: testAddr, country: "China2"})
	c := mustOpen(t, WithSource(src1), WithSource(src2))

	results, err := c.LookupAll(testAddr)
	if err != nil {
		t.Fatalf("LookupAll() error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("len(results) = %d, want 2", len(results))
	}
}

func TestLookupAll_NoneFound(t *testing.T) {
	c := mustOpen(t, WithSource(newMockSource("db")))

	results, err := c.LookupAll(testAddr)
	if err != nil {
		t.Fatalf("LookupAll() error = %v, want nil", err)
	}
	if results != nil {
		t.Errorf("LookupAll() results = %#v, want nil", results)
	}
}

func TestLookupAll_PartialErrors(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src1.addErr(errors.New("broken"))
	src2.add("1.2.3.4", &Result{ip: testAddr, country: "China"})
	c := mustOpen(t, WithSource(src1), WithSource(src2))

	results, err := c.LookupAll(testAddr)
	if err == nil {
		t.Fatal("expected non-nil error from src1")
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Country() != "China" {
		t.Errorf("results[0].Country() = %q, want %q", results[0].Country(), "China")
	}
}

func TestLookupAll_CacheHitPerSource(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src1.add("1.2.3.4", &Result{ip: testAddr, source: "db1", country: "China"})
	src2.add("1.2.3.4", &Result{ip: testAddr, source: "db2", country: "US"})
	src1Count := 0
	src2Count := 0

	c := mustOpen(t,
		WithSource(&countingSource{Source: src1, counter: &src1Count}),
		WithSource(&countingSource{Source: src2, counter: &src2Count}),
		WithCache(10, time.Second),
	)

	for range 2 {
		results, err := c.LookupAll(testAddr)
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
		WithSource(&countingSource{Source: src1, counter: &src1Count}),
		WithSource(&countingSource{Source: src2, counter: &src2Count}),
		WithCache(10, time.Second),
	)

	for range 2 {
		results, err := c.LookupAll(testAddr)
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

// ---- LookupFrom ----

func TestLookupFrom_Found(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src2.add("1.2.3.4", &Result{ip: testAddr, country: "US"})
	c := mustOpen(t, WithSource(src1), WithSource(src2))

	got, err := c.LookupFrom("db2", testAddr)
	if err != nil {
		t.Fatalf("LookupFrom() error: %v", err)
	}
	if got.Country() != "US" {
		t.Errorf("Country = %q, want US", got.Country())
	}
}

func TestLookupFrom_UnknownSource(t *testing.T) {
	c := mustOpen(t, WithSource(newMockSource("db")))

	_, err := c.LookupFrom("nonexistent", testAddr)
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestLookupFrom_NotFound(t *testing.T) {
	c := mustOpen(t, WithSource(newMockSource("db")))

	got, err := c.LookupFrom("db", testAddr)
	if err != nil {
		t.Fatalf("LookupFrom() error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("LookupFrom() result = %#v, want nil", got)
	}
}

func TestLookupFrom_CacheHit(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	src2.add("1.2.3.4", &Result{ip: testAddr, country: "US"})
	src1Count := 0
	src2Count := 0

	c := mustOpen(t,
		WithSource(&countingSource{Source: src1, counter: &src1Count}),
		WithSource(&countingSource{Source: src2, counter: &src2Count}),
		WithCache(10, 0),
	)

	for range 3 {
		got, err := c.LookupFrom("db2", testAddr)
		if err != nil {
			t.Fatalf("LookupFrom() error: %v", err)
		}
		if got.Country() != "US" {
			t.Fatalf("Country = %q, want US", got.Country())
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

	c := mustOpen(t, WithSource(&countingSource{Source: src, counter: &callCount}), WithCache(10, 0))

	for range 2 {
		_, err := c.LookupFrom("db", testAddr)
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

	c := mustOpen(t, WithSource(&countingSource{Source: src, counter: &callCount}), WithCache(10, time.Second))

	for range 2 {
		_, err := c.LookupFrom("db", testAddr)
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
		WithSource(&countingSource{Source: src, counter: &callCount}),
		WithCache(10, 20*time.Millisecond),
	)

	_, _ = c.LookupFrom("db", testAddr)
	_, _ = c.LookupFrom("db", testAddr)
	if callCount != 1 {
		t.Fatalf("source called %d times, want 1 before TTL expiry", callCount)
	}

	time.Sleep(30 * time.Millisecond)
	_, err := c.LookupFrom("db", testAddr)
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
		WithSource(&countingSource{Source: src, counter: &callCount}),
		WithCache(10, 0),
	)

	for range 2 {
		_, err := c.LookupFrom("db", testAddr)
		if !errors.Is(err, sentinelErr) {
			t.Fatalf("err = %v, want sentinel error", err)
		}
	}

	if callCount != 2 {
		t.Errorf("source called %d times, want 2 when error cache is disabled", callCount)
	}
}

func TestLookupFrom_SingleflightWithoutCache(t *testing.T) {
	src := newBlockingSource("db", &Result{ip: testAddr, country: "US"}, nil)
	c := mustOpen(t, WithSource(src))

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
			got, err := c.LookupFrom("db", testAddr)
			if err != nil {
				errs <- err
				return
			}
			if got.Country() != "US" {
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

	got, err := c.LookupFrom("db", testAddr)
	if err != nil {
		t.Fatalf("LookupFrom() after singleflight error: %v", err)
	}
	if got.Country() != "US" {
		t.Fatalf("Country = %q, want US", got.Country())
	}
	if got := src.calls.Load(); got != 2 {
		t.Errorf("source called %d times, want 2 after sequential no-cache retry", got)
	}
}

func TestLookupFrom_SingleflightCacheMiss(t *testing.T) {
	src := newBlockingSource("db", &Result{ip: testAddr, country: "US"}, nil)
	c := mustOpen(t, WithSource(src), WithCache(10, 0))

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
			got, err := c.LookupFrom("db", testAddr)
			if err != nil {
				errs <- err
				return
			}
			if got.Country() != "US" {
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

	_, _ = c.LookupFrom("db", testAddr)
	if got := src.calls.Load(); got != 1 {
		t.Errorf("source called %d times, want 1 after cache hit", got)
	}
}

func TestLookupFrom_SingleflightErrorMiss(t *testing.T) {
	sentinelErr := errors.New("broken")
	src := newBlockingSource("db", nil, sentinelErr)
	c := mustOpen(t, WithSource(src), WithCache(10, 0))

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
			_, err := c.LookupFrom("db", testAddr)
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

	_, err := c.LookupFrom("db", testAddr)
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("err = %v, want sentinel error", err)
	}
	if got := src.calls.Load(); got != 2 {
		t.Errorf("source called %d times, want 2 after disabled error cache retry", got)
	}
}

type blockingSource struct {
	name    string
	result  *Result
	err     error
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func newBlockingSource(name string, result *Result, err error) *blockingSource {
	return &blockingSource{
		name:    name,
		result:  result,
		err:     err,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *blockingSource) Lookup(netip.Addr) (*Result, error) {
	s.calls.Add(1)
	s.once.Do(func() { close(s.started) })
	<-s.release
	return s.result, s.err
}

func (s *blockingSource) Name() string { return s.name }

func (s *blockingSource) Close() error { return nil }

// ---- SourceNames ----

func TestSourceNames(t *testing.T) {
	c := mustOpen(t, WithSource(newMockSource("alpha")), WithSource(newMockSource("beta")))

	names := c.SourceNames()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("SourceNames() = %v, want [alpha beta]", names)
	}
}

// ---- Options: WithCache ----

func TestWithCache_ValidSize(t *testing.T) {
	src := newMockSource("db")
	src.add("1.2.3.4", &Result{ip: testAddr, country: "China"})
	c := mustOpen(t, WithCache(100, 0), WithSource(src))

	_, err := c.Lookup(netip.MustParseAddr("1.2.3.4"))
	if err != nil {
		t.Errorf("Lookup() error: %v", err)
	}
}

func TestWithCache_ZeroSize(t *testing.T) {
	src := newMockSource("db")
	_, err := Open(WithSource(src), WithCache(0, 0))
	if err == nil {
		t.Fatal("expected error for cache size 0")
	}
}

func TestWithCache_NegativeSize(t *testing.T) {
	src := newMockSource("db")
	_, err := Open(WithSource(src), WithCache(-1, 0))
	if err == nil {
		t.Fatal("expected error for negative cache size")
	}
	_, err = Open(WithSource(src), WithCache(-100, 0))
	if err == nil {
		t.Fatal("expected error for negative cache size -100")
	}
}

func TestWithCache_NegativeErrorTTL(t *testing.T) {
	src := newMockSource("db")
	_, err := Open(WithSource(src), WithCache(10, -time.Second))
	if err == nil {
		t.Fatal("expected error for negative error cache TTL")
	}
}

func TestWithCache_ZeroErrorTTL(t *testing.T) {
	src := newMockSource("db")
	c, err := Open(WithSource(src), WithCache(10, 0))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
}

func TestWithCache_EvictsCachedEntry(t *testing.T) {
	src := newMockSource("db")
	callCount := 0
	counting := &countingSource{Source: src, counter: &callCount}

	c := mustOpen(t, WithSource(counting), WithCache(1, 0))

	addr1 := netip.MustParseAddr("1.1.1.1")
	addr2 := netip.MustParseAddr("2.2.2.2")
	src.add("1.1.1.1", &Result{ip: addr1, country: "A"})
	src.add("2.2.2.2", &Result{ip: addr2, country: "B"})

	_, _ = c.Lookup(addr1) // Cache addr1
	_, _ = c.Lookup(addr2) // Cache addr2, evicts addr1
	_, _ = c.Lookup(addr1) // Miss, calls source again

	if callCount != 3 {
		t.Errorf("source called %d times, want 3 (cache size 1 should evict)", callCount)
	}
}

// ---- Close ----

func TestClose_ClosesAllSources(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	c, err := Open(WithSource(src1), WithSource(src2))
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
	src.add("1.2.3.4", &Result{ip: testAddr, country: "China"})
	// Don't use mustOpen here — we call Close() manually to inspect state.
	c, err := Open(WithSource(src), WithCache(10, time.Second))
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

	_, _ = c.Lookup(netip.MustParseAddr("1.2.3.4"))
	if cached.cache.Len() != 1 {
		t.Fatalf("cache len = %d, want 1 before Close()", cached.cache.Len())
	}
	cached.errors.Set(netip.MustParseAddr("2.2.2.2"), errors.New("broken"), ttlcache.DefaultTTL)
	if cached.errors.Len() != 1 {
		t.Fatalf("error cache len = %d, want 1 before Close()", cached.errors.Len())
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if cached.cache.Len() != 0 {
		t.Errorf("cache len = %d, want 0 after Close()", cached.cache.Len())
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
	c := mustOpen(t, WithSource(&countingSource{Source: src, counter: &callCount}), WithCache(10, 20*time.Millisecond))

	if _, err := c.Lookup(testAddr); !errors.Is(err, lookupErr) {
		t.Fatalf("first Lookup() error = %v, want lookupErr", err)
	}
	if _, err := c.Lookup(testAddr); !errors.Is(err, lookupErr) {
		t.Fatalf("cached Lookup() error = %v, want lookupErr", err)
	}
	if callCount != 1 {
		t.Fatalf("source called %d times before TTL expiry, want 1", callCount)
	}

	time.Sleep(30 * time.Millisecond)
	if _, err := c.Lookup(testAddr); !errors.Is(err, lookupErr) {
		t.Fatalf("post-expiry Lookup() error = %v, want lookupErr", err)
	}
	if callCount != 2 {
		t.Fatalf("source called %d times after TTL expiry, want 2", callCount)
	}
}
