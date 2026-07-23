# Decorator Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace hardcoded global source wrapping with a `SourceCreator` + `SourceDecorator` pattern so users freely compose singleflight/cache per source.

**Architecture:** New `creator.go` exports `SourceCreator` (value-type builder) and `SourceDecorator` (`func(Source)(Source,error)`). Source constructors (`MMDB`, `IPDB`, `XDB`, `IP2Location`, `Wrap`) return `SourceCreator`; `.Decorate()` appends decorators; `.Create()` opens files and applies decorators in order. `Open(creators ...SourceCreator)` builds all sources, closing on failure. Old `options.go` is deleted; `Client.wrapSources` and cache fields removed.

**Tech Stack:** Go 1.25, `golang.org/x/sync/singleflight`, `github.com/jellydator/ttlcache/v3`

## Global Constraints

- Package name: `ipgeo` (root module `github.com/kibaamor/ipgeo`)
- `cmd/ipgeo` is a separate module (`github.com/kibaamor/ipgeo/cmd/ipgeo`) that imports the root
- Test commands: `go test github.com/kibaamor/ipgeo/...` and `go test -C ./cmd/ipgeo ./...`
- Lint: `golangci-lint run ./...` and `cd ./cmd/ipgeo && golangci-lint run`
- No comments in code unless explicitly shown in the plan
- Internal functions `openMMDB`, `openIPDB`, `openXDB`, `openIP2Location`, `newCachedSource`, `newSingleflightSource` stay unexported and unchanged

---

### Task 1: Create `creator.go` with SourceCreator and decorators

**Files:**
- Create: `creator.go`
- Test: `creator_test.go`

**Interfaces:**
- Consumes: `Source` interface (from `source.go`), `openMMDB`/`openIPDB`/`openXDB`/`openIP2Location` (from `source_*.go`), `newCachedSource`/`newSingleflightSource` (from `cached_source.go`/`singleflight_source.go`)
- Produces: `SourceCreator` type, `SourceDecorator` type, `MMDB`/`IPDB`/`XDB`/`IP2Location`/`Wrap` constructors, `Decorate`/`Create` methods, `Singleflight`/`Cache` decorator constructors

- [ ] **Step 1: Write the failing tests**

Create `creator_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test github.com/kibaamor/ipgeo -run 'TestCreate_|TestDecorate_|TestSingleflight_Deduplicates|TestCache_Decorator|TestMMDB_CreatePropagates' -v`
Expected: FAIL — `creator.go` does not exist, types/functions undefined.

- [ ] **Step 3: Write `creator.go`**

```go
package ipgeo

import (
	"errors"
	"fmt"
	"time"
)

// SourceDecorator wraps a Source, returning the decorated Source.
// A nil error is returned on success; errors propagate through Create.
type SourceDecorator func(Source) (Source, error)

// SourceCreator configures and constructs a decorated Source.
// Create one via MMDB, IPDB, XDB, IP2Location, or Wrap, then apply
// decorators with Decorate. Pass the SourceCreator directly to Open,
// or call Create to build the Source manually.
type SourceCreator struct {
	name       string
	build      func() (Source, error)
	decorators []SourceDecorator
}

// MMDB returns a SourceCreator for a MaxMind DB source.
// companionPath is optional; pass "" to omit.
func MMDB(name, path, companionPath string) SourceCreator {
	return SourceCreator{
		name: name,
		build: func() (Source, error) {
			src, err := openMMDB(name, path, companionPath)
			if err != nil {
				return nil, fmt.Errorf("open mmdb %s: %w", name, err)
			}
			return src, nil
		},
	}
}

// IPDB returns a SourceCreator for an IPIP.net IPDB source.
func IPDB(name, path string) SourceCreator {
	return SourceCreator{
		name: name,
		build: func() (Source, error) {
			src, err := openIPDB(name, path)
			if err != nil {
				return nil, fmt.Errorf("open ipdb %s: %w", name, err)
			}
			return src, nil
		},
	}
}

// XDB returns a SourceCreator for an ip2region XDB source.
// v4Path and v6Path may each be empty, but at least one must be provided.
func XDB(name, v4Path, v6Path string) SourceCreator {
	return SourceCreator{
		name: name,
		build: func() (Source, error) {
			src, err := openXDB(name, v4Path, v6Path)
			if err != nil {
				return nil, fmt.Errorf("open xdb %s: %w", name, err)
			}
			return src, nil
		},
	}
}

// IP2Location returns a SourceCreator for an IP2Location BIN database source.
func IP2Location(name, path string) SourceCreator {
	return SourceCreator{
		name: name,
		build: func() (Source, error) {
			src, err := openIP2Location(name, path)
			if err != nil {
				return nil, fmt.Errorf("open ip2location %s: %w", name, err)
			}
			return src, nil
		},
	}
}

// Wrap returns a SourceCreator for an existing Source, allowing it to be
// decorated and included in a Client.
func Wrap(src Source) SourceCreator {
	var name string
	if src != nil {
		name = src.Name()
	}
	return SourceCreator{
		name: name,
		build: func() (Source, error) {
			if src == nil {
				return nil, errors.New("Wrap: src must not be nil")
			}
			return src, nil
		},
	}
}

// Decorate appends a decorator to the creator. Decorators are applied in
// the order they are added: the first added is the innermost wrapper,
// the last added is the outermost.
func (c SourceCreator) Decorate(d SourceDecorator) SourceCreator {
	c.decorators = append(c.decorators, d)
	return c
}

// Create builds the source by opening the database file (or retrieving the
// wrapped source) and applying decorators in order. If a decorator fails,
// the source built so far is closed.
func (c SourceCreator) Create() (Source, error) {
	src, err := c.build()
	if err != nil {
		return nil, err
	}
	for _, d := range c.decorators {
		decorated, err := d(src)
		if err != nil {
			_ = src.Close()
			return nil, err
		}
		src = decorated
	}
	return src, nil
}

// Singleflight returns a SourceDecorator that wraps a source with
// singleflight to deduplicate concurrent Lookup calls for the same address.
func Singleflight() SourceDecorator {
	return func(src Source) (Source, error) {
		return newSingleflightSource(src), nil
	}
}

// Cache returns a SourceDecorator that wraps a source with a TTL cache.
// maxEntries must be positive; resultTTL must not be negative (0 = permanent);
// errorTTL must not be negative (0 = errors not cached). Context errors are
// never cached.
func Cache(maxEntries uint, resultTTL, errorTTL time.Duration) SourceDecorator {
	return func(src Source) (Source, error) {
		return newCachedSource(src, maxEntries, resultTTL, errorTTL)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test github.com/kibaamor/ipgeo -run 'TestCreate_|TestDecorate_|TestSingleflight_Deduplicates|TestCache_Decorator|TestMMDB_CreatePropagates' -v`
Expected: PASS

- [ ] **Step 5: Run full test suite to verify no regressions**

Run: `go test github.com/kibaamor/ipgeo/...`
Expected: PASS — old code still compiles alongside new `creator.go`.

- [ ] **Step 6: Commit**

```bash
git add creator.go creator_test.go
git commit -m "feat: add SourceCreator and SourceDecorator for per-source decorator composition"
```

---

### Task 2: Refactor `client.go`, delete `options.go`, rewrite library tests

**Files:**
- Modify: `client.go`
- Delete: `options.go`
- Modify: `client_test.go`
- Modify: `client_cache_edge_test.go`
- Modify: `context_test.go`
- Modify: `source_options_test.go`

**Interfaces:**
- Consumes: `SourceCreator` type, `Wrap` constructor, `Singleflight`/`Cache` decorators from Task 1
- Produces: `Open(creators ...SourceCreator) (*Client, error)` — the new client constructor

**Migration rules (apply consistently across all test files):**
- `Open(WithSource(src))` → `Open(Wrap(src))`
- `Open(WithSource(src1), WithSource(src2))` → `Open(Wrap(src1), Wrap(src2))`
- `Open(WithSource(src), WithCache(N, rTTL, eTTL))` → `Open(Wrap(src).Decorate(Cache(N, rTTL, eTTL)))`
- `Open(WithSource(src), WithCache(N, rTTL, eTTL))` where the test inspects `*singleflightSource` internally → `Open(Wrap(src).Decorate(Singleflight()).Decorate(Cache(N, rTTL, eTTL)))`
- `Open(WithSource(src))` where the test name contains "Singleflight" → `Open(Wrap(src).Decorate(Singleflight()))`
- `mustOpen(t, opts ...Option)` → `mustOpen(t, creators ...SourceCreator)`
- `Open(WithSource(nil))` → `Open(Wrap(nil))` (Wrap handles nil, returns error from Create)
- `Open(WithMMDB(name, path, companion))` → `Open(MMDB(name, path, companion))`
- Tests using `newCachedSource`/`newSingleflightSource`/`openMMDB`/etc. directly stay unchanged
- Tests using `c.wrapSources()` are removed (method deleted)

- [ ] **Step 1: Rewrite `client.go`**

Replace the entire file content. The `Client` struct loses cache fields; `Open` takes `...SourceCreator`; `wrapSources` is deleted; `Lookup`/`LookupAll`/`LookupFrom`/`SourceNames`/`Close` stay the same:

```go
package ipgeo

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
)

// Client queries one or more geolocation sources.
//
// A Client is safe for concurrent use by multiple goroutines: Lookup, LookupAll,
// LookupFrom, and SourceNames may be called concurrently. Close is safe to call
// multiple times and from concurrent goroutines, but it must not be called
// concurrently with any query method; the result of doing so is undefined, as
// with io.Closer.
type Client struct {
	sources      []Source
	sourceByName map[string]Source
	closeOnce    sync.Once
	closeErr     error
}

// Open creates a new Client from the provided source creators.
// Each creator is built (Create) in order; if any fails, all previously
// created sources are closed and the error is returned.
// At least one creator is required (ErrNoSources); source names must be
// unique (ErrDuplicateSource).
func Open(creators ...SourceCreator) (*Client, error) {
	if len(creators) == 0 {
		return nil, ErrNoSources
	}

	seen := make(map[string]struct{}, len(creators))
	for _, c := range creators {
		if _, exists := seen[c.name]; exists {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateSource, c.name)
		}
		seen[c.name] = struct{}{}
	}

	sources := make([]Source, len(creators))
	for i, c := range creators {
		var err error
		sources[i], err = c.Create()
		if err != nil {
			for j := 0; j < i; j++ {
				_ = sources[j].Close()
			}
			return nil, err
		}
	}

	sourceByName := make(map[string]Source, len(sources))
	for _, src := range sources {
		sourceByName[src.Name()] = src
	}

	return &Client{sources: sources, sourceByName: sourceByName}, nil
}

// SourceNames returns the names of all configured sources in order.
func (c *Client) SourceNames() []string {
	names := make([]string, len(c.sources))
	for i, src := range c.sources {
		names[i] = src.Name()
	}
	return names
}

// Lookup queries sources in order and returns the first result found.
// If no source has a matching record, it returns a nil Result with a nil error.
// IPv4-mapped IPv6 addresses are unmapped before lookup.
// If ctx is cancelled, Lookup returns the context error without querying.
func (c *Client) Lookup(ctx context.Context, addr netip.Addr) (*Result, error) {
	addr = addr.Unmap()

	for _, src := range c.sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := src.Lookup(ctx, addr)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", src.Name(), err)
		}
		if result == nil {
			continue
		}

		return result, nil
	}

	return nil, nil
}

// LookupAll queries all sources and returns every result found.
// If no source has a matching record, it returns a nil result slice and nil error.
// Nil results from individual sources are silently skipped; errors are joined.
// If ctx is cancelled, LookupAll stops querying and joins the context error.
func (c *Client) LookupAll(ctx context.Context, addr netip.Addr) ([]*Result, error) {
	addr = addr.Unmap()

	var results []*Result
	errs := make([]error, 0, len(c.sources))
	for _, src := range c.sources {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		result, err := src.Lookup(ctx, addr)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", src.Name(), err))
			continue
		}
		if result == nil {
			continue
		}

		results = append(results, result)
	}

	return results, errors.Join(errs...)
}

// LookupFrom queries a specific named source.
// If that source has no matching record, it returns a nil Result with a nil error.
// If ctx is cancelled, LookupFrom returns the context error without querying.
func (c *Client) LookupFrom(ctx context.Context, sourceName string, addr netip.Addr) (*Result, error) {
	target := c.sourceByName[sourceName]
	if target == nil {
		return nil, fmt.Errorf("%w: %q", ErrSourceNotConfigured, sourceName)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	addr = addr.Unmap()

	result, err := target.Lookup(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", target.Name(), err)
	}

	return result, nil
}

// Close closes all sources and purges any per-source caches.
// It is safe to call Close multiple times; subsequent calls return the same
// error as the first and do not close sources again. Close must not be called
// concurrently with Lookup or the other query methods.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		errs := make([]error, 0, len(c.sources))
		for _, src := range c.sources {
			errs = append(errs, src.Close())
		}

		c.sources = nil
		c.sourceByName = nil

		c.closeErr = errors.Join(errs...)
	})
	return c.closeErr
}
```

- [ ] **Step 2: Delete `options.go`**

```bash
rm options.go
```

- [ ] **Step 3: Rewrite `client_test.go`**

The `mustOpen` helper and all test functions that use `WithSource`/`WithCache` must migrate to the new API. Apply these specific changes:

1. Change `mustOpen` signature (line 56): `opts ...Option` → `creators ...SourceCreator`, and `Open(opts...)` → `Open(creators...)`.

2. `TestOpen_NilSource` (line 75): `Open(WithSource(nil))` → `Open(Wrap(nil))`.

3. `TestOpen_DuplicateName` (line 82): `Open(WithSource(src), WithSource(src))` → `Open(Wrap(src), Wrap(src))`.

4. `TestOpen_Success` (line 90): `mustOpen(t, WithSource(newMockSource("db")))` → `mustOpen(t, Wrap(newMockSource("db")))`.

5. Every `mustOpen(t, WithSource(src))` or `Open(WithSource(src))` with no cache → `mustOpen(t, Wrap(src))` or `Open(Wrap(src))`. This applies to: `TestLookup_Found`, `TestLookup_NotFound`, `TestLookup_SourceError`, `TestLookup_FallsThrough`, `TestLookup_IPv4MappedIPv6`, `TestLookupAll_MultiSource`, `TestLookupAll_NoneFound`, `TestLookupAll_PartialErrors`, `TestLookupFrom_Found`, `TestLookupFrom_UnknownSource`, `TestLookupFrom_NotFound`, `TestSourceNames`, `TestClose_ClosesAllSources`, `TestClose_Idempotent`, `TestClose_ConcurrentSafe`.

6. Cache tests — `WithSource(counting), WithCache(N, r, e)` → `Wrap(counting).Decorate(Cache(N, r, e))`. This applies to: `TestLookup_CacheHit`, `TestLookup_CachesNilMissFallthrough`, `TestLookupAll_CacheHitPerSource`, `TestLookupAll_CachesErrors`, `TestLookupFrom_CacheHit`, `TestLookupFrom_DoesNotCacheErrorWithoutTTL`, `TestLookupFrom_CachesErrorWithTTL`, `TestLookupFrom_CacheErrorExpires`, `TestLookupFrom_CacheErrorCanBeDisabled`, `TestWithCache_ValidSize`, `TestWithCache_EvictsCachedEntry`, `TestWithCache_ResultTTLExpires`, `TestWithCache_ResultTTLZeroIsPermanent`, `TestWithCache_ResultTTLIsSlidingWindow`, `TestErrorCacheExpiresLazily`.

7. `TestWithCache_ZeroSizeDisablesCache` (line 706): `WithSource(counting), WithCache(0, 0, 0)` → `Wrap(counting)` (maxEntries=0 means no cache decorator).

8. `TestWithCache_NegativeErrorTTL` (line 722): `Open(WithSource(src), WithCache(10, 0, -time.Second))` → `Open(Wrap(src).Decorate(Cache(10, 0, -time.Second)))`.

9. `TestWithCache_NegativeResultTTL` (line 730): `Open(WithSource(src), WithCache(10, -time.Second, 0))` → `Open(Wrap(src).Decorate(Cache(10, -time.Second, 0)))`.

10. `TestWithCache_ZeroErrorTTL` (line 738): `Open(WithSource(src), WithCache(10, 0, 0))` → `Open(Wrap(src).Decorate(Cache(10, 0, 0)))`.

11. `TestLookupFrom_SingleflightWithoutCache` (line 503): `WithSource(src)` → `Wrap(src).Decorate(Singleflight())`.

12. `TestLookupFrom_SingleflightCacheMiss` (line 555): `WithSource(src), WithCache(10, 0, 0)` → `Wrap(src).Decorate(Singleflight()).Decorate(Cache(10, 0, 0))`.

13. `TestLookupFrom_SingleflightErrorMiss` (line 601): `WithSource(src), WithCache(10, 0, 0)` → `Wrap(src).Decorate(Singleflight()).Decorate(Cache(10, 0, 0))`.

14. `TestClose_PurgesCache` (line 864): `Open(WithSource(src), WithCache(10, 0, time.Second))` → `Open(Wrap(src).Decorate(Singleflight()).Decorate(Cache(10, 0, time.Second)))`. This test inspects `c.sources[0].(*cachedSource).source.(*singleflightSource)`, so Singleflight must be present.

15. Remove the `ttlcache` import (line 12) if it's no longer used. Check: `TestClose_PurgesCache` uses `ttlcache.DefaultTTL` on line 884. Keep the import.

16. Remove the `"time"` import only if no test uses it. Many cache tests use `time.Second`/`time.Millisecond`, so keep it.

- [ ] **Step 4: Rewrite `client_cache_edge_test.go`**

1. `TestNewCachedSourceRejectsZeroSize` (line 12): unchanged — uses `newCachedSource` directly.

2. `TestWrapSourcesWithoutCachePreservesOrderAndSkipsCache` (line 22): **delete entirely**. Tests `c.wrapSources()` which no longer exists.

3. `TestWrapSourcesWithCachePreservesOrder` (line 49): **delete entirely**. Same reason.

4. `TestSingleflightSourceUnexpectedSharedType` (line 77): unchanged — uses `newSingleflightSource` directly.

5. `TestSingleflightSourcePropagatesLookupAndCloseErrors` (line 110): unchanged — uses `newSingleflightSource` directly.

6. `TestClientCloseReturnsJoinedErrorsAndClearsSources` (line 128): `Open(WithSource(src1), WithSource(src2))` → `Open(Wrap(src1), Wrap(src2))`.

7. `TestOpenOptionErrorClosesPreviouslyAddedSources` (line 153): **rewrite**. The old test uses `func(*Client) error` as a raw Option. Replace with a SourceCreator whose Create fails:

```go
func TestOpenCreatorErrorClosesPreviouslyCreatedSources(t *testing.T) {
	sentinelErr := errors.New("creator broken")
	src := newMockSource("db")
	failing := SourceCreator{
		name:  "failing",
		build: func() (Source, error) { return nil, sentinelErr },
	}

	_, err := Open(Wrap(src), failing)
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("Open() error = %v, want sentinelErr", err)
	}
	if !src.closed {
		t.Fatal("source created before failing creator was not closed")
	}
}
```

8. `closeErrorSource` type (line 166): unchanged.

9. Check imports: remove `"time"` if no longer used after deleting the wrapSources tests. The wrapSources tests used `time.Second` on line 52. After deletion, check if any remaining test uses `time` — `TestNewCachedSourceRejectsZeroSize` doesn't, `TestSingleflightSourceUnexpectedSharedType` doesn't (uses `time.AfterFunc`). Keep `"time"` because `TestSingleflightSourceUnexpectedSharedType` uses `time.AfterFunc` on line 94.

- [ ] **Step 5: Rewrite `context_test.go`**

1. `TestLookup_RespectsCancelledContext` (line 27): `mustOpen(t, WithSource(src))` → `mustOpen(t, Wrap(src))`.

2. `TestLookupAll_RespectsCancelledContext` (line 46): `mustOpen(t, WithSource(src1), WithSource(src2))` → `mustOpen(t, Wrap(src1), Wrap(src2))`.

3. `TestLookupFrom_RespectsCancelledContext` (line 66): `mustOpen(t, WithSource(src))` → `mustOpen(t, Wrap(src))`.

4. `TestCachedSource_DoesNotCacheContextError` (line 88): unchanged — uses `newCachedSource` directly.

5. `TestSingleflight_DoesNotPoisonConcurrentCaller` (line 115): unchanged — uses `newSingleflightSource` directly.

- [ ] **Step 6: Rewrite `source_options_test.go`**

1. `TestOpenDatabaseOptionsFailWithInvalidPaths` (line 11): Change the test table type from `opt Option` to a `creator SourceCreator`, and change `Open(tt.opt)` to `Open(tt.creator)`. The test cases become:

```go
func TestOpenDatabaseOptionsFailWithInvalidPaths(t *testing.T) {
	missing := "testdata/missing-db"
	tests := []struct {
		name    string
		creator SourceCreator
		wantErr string
	}{
		{
			name:    "mmdb empty path",
			creator: MMDB("mmdb", "", ""),
			wantErr: "open mmdb mmdb: path must be non-empty",
		},
		{
			name:    "mmdb missing path",
			creator: MMDB("mmdb", missing, ""),
			wantErr: "open mmdb mmdb:",
		},
		{
			name:    "ipdb missing path",
			creator: IPDB("ipdb", missing),
			wantErr: "open ipdb ipdb:",
		},
		{
			name:    "xdb empty paths",
			creator: XDB("xdb", "", ""),
			wantErr: "open xdb xdb: at least one of v4Path or v6Path must be non-empty",
		},
		{
			name:    "xdb missing v4 path",
			creator: XDB("xdb", missing, ""),
			wantErr: "open xdb xdb:",
		},
		{
			name:    "ip2location missing path",
			creator: IP2Location("ip2location", missing),
			wantErr: "open ip2location ip2location:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Open(tt.creator)
			if err == nil {
				t.Fatal("Open() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Open() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}
```

2. `TestDatabaseSourceConstructorsFailWithInvalidFiles` (line 63): unchanged — uses `openMMDB`/`openIPDB`/etc. directly.

3. `TestSourceNamesWithoutDatabaseFiles` (line 129): unchanged — uses `&mmdbSource{}` etc. directly.

4. `TestIPDBSourceCloseWithoutDatabaseFile` (line 147): unchanged.

5. `TestAddrToNetIP` (line 153): unchanged.

6. `TestDatabaseSourceConstructorsWithInjectedOpeners` (line 173): unchanged — uses `openMMDB`/etc. directly.

7. `TestDatabaseOptionsWithInjectedOpeners` (line 271): Change from `opt Option` to `creator SourceCreator`, and `Open(tt.opt)` to `Open(tt.creator)`:

```go
func TestDatabaseOptionsWithInjectedOpeners(t *testing.T) {
	restoreMMDBOpener(t, func(path string) (mmdbReader, error) { return &fakeMMDBReader{}, nil })
	restoreIPDBOpener(t, func(path string) (ipdbReader, error) {
		return &fakeIPDBReader{langs: []string{"EN"}}, nil
	})
	restoreIP2LocationOpener(t, func(path string) (ip2locationReader, error) {
		return &fakeIP2LocationReader{}, nil
	})
	restoreXDBOpener(t, func(v4Path, v6Path string) (xdbSearcher, error) {
		return &fakeXDBSearcher{}, nil
	})

	tests := []struct {
		name    string
		creator SourceCreator
	}{
		{name: "mmdb", creator: MMDB("mmdb", "city.mmdb", "asn.mmdb")},
		{name: "ipdb", creator: IPDB("ipdb", "city.ipdb")},
		{name: "xdb", creator: XDB("xdb", "v4.xdb", "v6.xdb")},
		{name: "ip2location", creator: IP2Location("ip2location", "db.bin")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := Open(tt.creator)
			if err != nil {
				t.Fatalf("Open() error: %v", err)
			}
			if err := client.Close(); err != nil {
				t.Fatalf("Close() error: %v", err)
			}
		})
	}
}
```

8. The `restore*Opener` helper functions (line 305-331): unchanged.

- [ ] **Step 7: Run full library test suite**

Run: `go test github.com/kibaamor/ipgeo/...`
Expected: PASS — all library tests pass with the new API.

- [ ] **Step 8: Run lint**

Run: `golangci-lint run ./...`
Expected: PASS — no lint errors.

- [ ] **Step 9: Commit**

```bash
git add client.go client_test.go client_cache_edge_test.go context_test.go source_options_test.go
git rm options.go
git commit -m "refactor!: replace Option/WithSource/WithCache with SourceCreator/Decorate; remove global wrapping

BREAKING CHANGE: Open now accepts ...SourceCreator instead of ...Option.
WithSource, WithCache, WithMMDB, WithIPDB, WithXDB, WithIP2Location are removed.
Use MMDB/IPDB/XDB/IP2Location/Wrap constructors with Decorate(Singleflight())/Decorate(Cache()) instead."
```

---

### Task 3: Adapt CLI to new API

**Files:**
- Modify: `cmd/ipgeo/internal/sources/sources.go`
- Modify: `cmd/ipgeo/internal/clirun/run.go`
- Modify: `cmd/ipgeo/internal/sources/sources_test.go`

**Interfaces:**
- Consumes: `ipgeo.SourceCreator`, `ipgeo.MMDB`/`ipgeo.IPDB`/`ipgeo.XDB`/`ipgeo.IP2Location`/`ipgeo.Singleflight` from the root package
- Produces: `sources.Creator`/`sources.Creators` returning `ipgeo.SourceCreator` instead of `ipgeo.Option`

- [ ] **Step 1: Rewrite `sources.go` functions `Option` and `Options`**

In `cmd/ipgeo/internal/sources/sources.go`, rename `Options` → `Creators` and `Option` → `Creator`, change return types from `ipgeo.Option` to `ipgeo.SourceCreator`, and wrap each source with `.Decorate(ipgeo.Singleflight())` to preserve current CLI behavior (singleflight always-on, no cache):

Replace the `Options` function (line 61) and `Option` function (line 73) with:

```go
func Creators(entries []Entry, sourcePath func(string) string) ([]ipgeo.SourceCreator, error) {
	creators := make([]ipgeo.SourceCreator, 0, len(entries))
	for _, entry := range entries {
		creator, err := Creator(entry, sourcePath)
		if err != nil {
			return nil, err
		}
		creators = append(creators, creator)
	}
	return creators, nil
}

func Creator(entry Entry, sourcePath func(string) string) (ipgeo.SourceCreator, error) {
	path := sourcePath(entry.Filename)
	companionPath := ""
	if entry.CompanionFilename != "" {
		companionPath = sourcePath(entry.CompanionFilename)
	}
	switch entry.Type {
	case "mmdb":
		return ipgeo.MMDB(entry.Name, path, companionPath).Decorate(ipgeo.Singleflight()), nil
	case "ipdb":
		return ipgeo.IPDB(entry.Name, path).Decorate(ipgeo.Singleflight()), nil
	case "xdb":
		return ipgeo.XDB(entry.Name, path, companionPath).Decorate(ipgeo.Singleflight()), nil
	case "ip2location":
		return ipgeo.IP2Location(entry.Name, path).Decorate(ipgeo.Singleflight()), nil
	default:
		return ipgeo.SourceCreator{}, fmt.Errorf("configure source %s: unknown source type: %s", entry.Name, entry.Type)
	}
}
```

- [ ] **Step 2: Rewrite `clirun/run.go` `loadSources`**

In `cmd/ipgeo/internal/clirun/run.go`, change `loadSources` (line 105) to call `sources.Creators` and `ipgeo.Open(creators...)`:

```go
func loadSources(ctx context.Context, cfg *config.Config, sourceName string) (*ipgeo.Client, error) {
	selected, err := sources.Select(cfg.Sources, sourceName)
	if err != nil {
		return nil, err
	}
	if err := updater.EnsureSources(ctx, cfg, selected); err != nil {
		return nil, err
	}

	creators, err := sources.Creators(selected, cfg.SourcePath)
	if err != nil {
		return nil, err
	}
	return ipgeo.Open(creators...)
}
```

- [ ] **Step 3: Rewrite `sources_test.go`**

In `cmd/ipgeo/internal/sources/sources_test.go`, rename test functions and update calls:

```go
package sources

import (
	"path/filepath"
	"testing"
)

func TestCreator_KnownTypes(t *testing.T) {
	for _, sourceType := range []string{"mmdb", "ipdb", "xdb", "ip2location"} {
		t.Run(sourceType, func(t *testing.T) {
			_, err := Creator(Entry{Type: sourceType, Name: "test", Filename: "test.db"}, func(filename string) string {
				return filename
			})
			if err != nil {
				t.Fatalf("Creator() error: %v", err)
			}
		})
	}
}

func TestCreator_UnknownType(t *testing.T) {
	_, err := Creator(Entry{Type: "unknown", Name: "test", Filename: "test.db"}, func(filename string) string {
		return filename
	})
	if err == nil {
		t.Fatal("Creator() error = nil")
	}
}

func TestSelect_FiltersByName(t *testing.T) {
	entries := []Entry{
		{Name: "First", Type: "mmdb", Filename: "first.mmdb"},
		{Name: "Second", Type: "xdb", Filename: "second.xdb"},
	}

	sources, err := Select(entries, "Second")
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if len(sources) != 1 || sources[0].Name != "Second" {
		t.Fatalf("Select() = %#v, want only Second", sources)
	}
}

func TestFiles_ExpandsPrimaryAndCompanionFiles(t *testing.T) {
	home := t.TempDir()
	entries := []Entry{
		{
			Name:              "GeoLite2",
			Filename:          "GeoLite2-City.mmdb",
			URLs:              []string{"https://example.com/city.mmdb"},
			CompanionFilename: "GeoLite2-ASN.mmdb",
			CompanionURLs:     []string{"https://example.com/asn.mmdb"},
		},
		{
			Name:     "DBIPCityLite",
			Filename: "dbip.mmdb",
			URLs:     []string{"https://example.com/dbip.mmdb"},
		},
	}

	files := Files(entries, func(filename string) string {
		return filepath.Join(home, filename)
	})
	if len(files) != 3 {
		t.Fatalf("files len = %d, want 3", len(files))
	}
	if files[0].Name != "GeoLite2" || files[0].Path != filepath.Join(home, "GeoLite2-City.mmdb") || files[0].URLs[0] != "https://example.com/city.mmdb" {
		t.Fatalf("primary file = %#v", files[0])
	}
	if files[1].Name != "GeoLite2 (companion)" || files[1].Path != filepath.Join(home, "GeoLite2-ASN.mmdb") || files[1].URLs[0] != "https://example.com/asn.mmdb" {
		t.Fatalf("companion file = %#v", files[1])
	}
	if files[2].Name != "DBIPCityLite" || files[2].Path != filepath.Join(home, "dbip.mmdb") || files[2].URLs[0] != "https://example.com/dbip.mmdb" {
		t.Fatalf("second source file = %#v", files[2])
	}
}
```

- [ ] **Step 4: Run CLI test suite**

Run: `go test -C ./cmd/ipgeo ./...`
Expected: PASS

- [ ] **Step 5: Run CLI lint**

Run: `cd ./cmd/ipgeo && golangci-lint run`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/ipgeo/internal/sources/sources.go cmd/ipgeo/internal/sources/sources_test.go cmd/ipgeo/internal/clirun/run.go
git commit -m "refactor(cli): migrate to SourceCreator API; wrap sources with Singleflight"
```

---

### Task 4: Update documentation

**Files:**
- Modify: `doc.go`
- Modify: `README.md`
- Modify: `errors.go` (comments only)

- [ ] **Step 1: Update `doc.go`**

Replace the entire file:

```go
// Package ipgeo resolves IP addresses with one or more geolocation sources.
//
// Build a source with MMDB, IPDB, XDB, IP2Location, or Wrap, optionally
// decorate it with Singleflight and Cache, then open a [Client] with one or
// more source creators. Query the client with Lookup, LookupAll, or
// LookupFrom. Each query method accepts a [context.Context]; a cancelled
// context short-circuits the query.
//
// A Client is safe for concurrent use. Close is idempotent and safe from
// concurrent goroutines, but must not be called concurrently with a query.
//
// Open and LookupFrom return sentinel errors (ErrNoSources,
// ErrDuplicateSource, ErrSourceNotConfigured) for use with errors.Is.
package ipgeo
```

- [ ] **Step 2: Update `errors.go` comments**

Change the comment on `ErrNoSources` from "returned by Open when no source option is provided" to "returned by Open when no source creator is provided":

```go
// ErrNoSources is returned by Open when no source creator is provided.
ErrNoSources = errors.New("ipgeo: at least one source creator is required")
```

The other two error sentinels stay the same.

- [ ] **Step 3: Update `README.md` library usage section**

Replace the Go library usage example (lines 84-128) with:

````markdown
### Usage

Build a source with `MMDB`, `IPDB`, `XDB`, `IP2Location`, or `Wrap`, decorate
it with `Singleflight` and/or `Cache`, then open a client.

```go
package main

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/kibaamor/ipgeo"
)

func main() {
	client, err := ipgeo.Open(
		ipgeo.MMDB("GeoLite2", "GeoLite2-City.mmdb", "GeoLite2-ASN.mmdb").
			Decorate(ipgeo.Singleflight()).
			Decorate(ipgeo.Cache(1024, 0, 0)),
	)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	result, err := client.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1"))
	if err != nil {
		panic(err)
	}
	if result == nil {
		fmt.Println("not found")
		return
	}

	fmt.Println(result)
}
```

Supported built-in source constructors:

- `MMDB` for MaxMind DB files, with an optional companion MMDB.
- `IPDB` for IPIP.net IPDB files.
- `XDB` for ip2region XDB files.
- `IP2Location` for IP2Location BIN files.
- `Wrap` for custom `Source` implementations.

Built-in decorators (applied in call order; first added is innermost):

- `Singleflight` deduplicates concurrent lookups for the same address.
- `Cache` caches results (sliding TTL) and optionally errors (fixed TTL).

Lookup methods (each accepts a `context.Context` for cancellation/timeout):

- `Lookup` queries sources in order and returns the first result.
- `LookupAll` returns every result found.
- `LookupFrom` queries one named source.

A `Client` is safe for concurrent use. `Close` is idempotent. Common failures
are exported as sentinel errors (`ErrNoSources`, `ErrDuplicateSource`,
`ErrSourceNotConfigured`).
````

- [ ] **Step 4: Run full test suite and lint**

Run: `go test github.com/kibaamor/ipgeo/... && go test -C ./cmd/ipgeo ./...`
Expected: PASS

Run: `golangci-lint run ./... && cd ./cmd/ipgeo && golangci-lint run`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add doc.go errors.go README.md
git commit -m "docs: update package docs, README, and error comments for SourceCreator API"
```
