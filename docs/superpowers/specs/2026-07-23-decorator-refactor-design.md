# Refactor Library to Decorator Pattern with Per-Source Singleflight & Cache Control

## 1. Overview

Replace the hardcoded, global wrapping in `Client.wrapSources` with a **Decorator pattern** exposed via `SourceCreator` + `SourceDecorator`. Users control per-source whether to apply singleflight, cache, or custom decorators, and in what order. No backward compatibility.

## 2. Goals

- Let users **freely control** whether each source uses singleflight and/or cache.
- **Decorator order** is user-controlled (call order of `Decorate`).
- **One-line** creation for the common case.
- **Extensible**: users can write custom `SourceDecorator` functions without modifying the package.
- No new types beyond `SourceCreator` and `SourceDecorator`.
- CLI adapts to new API; preserves current behavior (singleflight always-on, no cache).

## 3. Non-goals

- Backward compatibility.
- CLI config schema changes (`config.yaml` untouched).
- New decorators beyond singleflight and cache (the mechanism is open, but only those two are built-in).

## 4. New Public API

### 4.1 Types

```go
// SourceDecorator wraps a Source, returning the decorated Source.
// A nil error is returned on success; errors propagate through Create.
type SourceDecorator func(Source) (Source, error)

// SourceCreator configures and constructs a decorated Source.
// Created via MMDB, IPDB, XDB, IP2Location, or Wrap.
// It is a value type; methods return new values (immutable style).
type SourceCreator struct {
    // unexported fields
}
```

### 4.2 Source constructors (entry points)

Each returns a `SourceCreator` that, when `Create()` is called, opens the database file and applies any registered decorators.

```go
func MMDB(name, path, companionPath string) SourceCreator
func IPDB(name, path string) SourceCreator
func XDB(name, v4Path, v6Path string) SourceCreator
func IP2Location(name, path string) SourceCreator
func Wrap(src Source) SourceCreator
```

`Wrap` wraps an existing `Source` (e.g. a custom implementation) for decoration and inclusion in a Client.

### 4.3 Decorator methods

```go
func (c SourceCreator) Decorate(d SourceDecorator) SourceCreator
func (c SourceCreator) Create() (Source, error)
```

`Decorate` appends a decorator. It is the **open extension point**: users pass any `SourceDecorator`, including their own.

`Create` builds the source: opens the database file (or retrieves the wrapped source), then applies decorators in the order they were added. The first decorator added is the innermost wrapper; the last is the outermost.

### 4.4 Built-in decorators

```go
func Singleflight() SourceDecorator
func Cache(maxEntries uint, resultTTL, errorTTL time.Duration) SourceDecorator
```

`Singleflight` wraps the source with a `singleflight.Group` to deduplicate concurrent `Lookup` calls for the same address. A cancelled caller context returns `ctx.Err()` without cancelling the in-flight source call.

`Cache` wraps the source with a TTL cache. Validation of `maxEntries` (must be >0) and non-negative TTLs happens when the decorator is applied during `Create()`; errors propagate through `Create()` → `Open()`. Semantics: results use a sliding TTL (0 = permanent); errors use a fixed TTL (0 = errors disabled); context errors are never cached.

### 4.5 Client constructor

```go
func Open(creators ...SourceCreator) (*Client, error)
```

`Open` calls `Create()` on each creator in order. On any failure, all previously-created sources are closed. Validates: at least one creator (`ErrNoSources`), no duplicate source names (`ErrDuplicateSource`). Builds the `sourceByName` map from the outermost sources' `Name()`.

`Lookup`, `LookupAll`, `LookupFrom`, `SourceNames`, `Close` retain their current behavior. `Client` no longer has cache fields or `wrapSources`.

## 5. Decorator Order

Decorators are applied in the order they were added via `Decorate`. The first added is the **innermost** wrapper; the last added is the **outermost**.

```go
MMDB("GeoLite2", "city.mmdb", "asn.mmdb").
    Decorate(Singleflight()).   // innermost: wraps the raw source
    Decorate(Cache(1024, 0, 0)) // outermost: wraps the singleflight source
```

Result: `Cache(Singleflight(MMDB))` — cache short-circuits hits; singleflight deduplicates concurrent cache misses. This is the recommended order and preserves the current library behavior.

## 6. Typical Usage

```go
client, err := ipgeo.Open(
    ipgeo.MMDB("GeoLite2", "city.mmdb", "asn.mmdb").
        Decorate(ipgeo.Singleflight()).
        Decorate(ipgeo.Cache(1024, 0, 0)),
    ipgeo.MMDB("DBIP", "dbip.mmdb", "").
        Decorate(ipgeo.Singleflight()),
)
```

Custom decorator:

```go
func Logging(log *slog.Logger) ipgeo.SourceDecorator {
    return func(s ipgeo.Source) (ipgeo.Source, error) {
        return &loggedSource{Source: s, log: log}, nil
    }
}

client, err := ipgeo.Open(
    ipgeo.MMDB("GeoLite2", "city.mmdb", "asn.mmdb").
        Decorate(ipgeo.Singleflight()).
        Decorate(ipgeo.Cache(1024, 0, 0)).
        Decorate(Logging(logger)),
)
```

## 7. Internal Implementation

### 7.1 SourceCreator

```go
type SourceCreator struct {
    name       string
    build      func() (Source, error)   // constructs the base source
    decorators []SourceDecorator
}
```

- `MMDB`/`IPDB`/`XDB`/`IP2Location`: set `name` and a `build` closure that calls the existing `openMMDB`/etc.
- `Wrap`: sets `name = src.Name()` and a `build` closure that returns `src, nil`.
- `Decorate`: appends to `decorators` slice (copies the creator, returns new value).
- `Create`: calls `build()`, then iterates `decorators` applying each; returns the outermost source.

### 7.2 Built-in decorators

`Singleflight()` returns a `SourceDecorator` that calls the existing `newSingleflightSource(src)` and returns it with nil error.

`Cache(...)` returns a `SourceDecorator` that validates `maxEntries` > 0, non-negative TTLs, then calls `newCachedSource(src, ...)` and returns the result.

### 7.3 Open

```go
func Open(creators ...SourceCreator) (*Client, error) {
    if len(creators) == 0 { return nil, ErrNoSources }

    // Pre-validate names (fail-fast, no files opened)
    seen := make(map[string]struct{}, len(creators))
    for _, c := range creators {
        if _, exists := seen[c.name]; exists {
            return nil, fmt.Errorf("%w: %q", ErrDuplicateSource, c.name)
        }
        seen[c.name] = struct{}{}
    }

    // Create sources
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
```

## 8. Files

| File | Action |
|------|--------|
| `creator.go` | **New** — `SourceCreator`, `SourceDecorator`, `MMDB`, `IPDB`, `XDB`, `IP2Location`, `Wrap`, `Singleflight`, `Cache`, `Decorate`, `Create` |
| `options.go` | **Delete** — `Option`, `WithMMDB`, `WithIPDB`, `WithXDB`, `WithIP2Location`, `WithSource`, `WithCache` |
| `client.go` | **Edit** — `Open` signature, remove `wrapSources` and cache fields; `Client` struct trimmed |
| `doc.go` | **Edit** — update package doc |
| `README.md` | **Edit** — update library usage example |
| `cmd/ipgeo/internal/sources/sources.go` | **Edit** — `Option`/`Options` return `SourceCreator`; each wraps with `.Decorate(Singleflight())` |
| `cmd/ipgeo/internal/clirun/run.go` | **Edit** — `loadSources` calls `ipgeo.Open(creators...)` |
| `source.go` | **Unchanged** |
| `cached_source.go` | **Unchanged** (unexported `newCachedSource` still used by `Cache` decorator) |
| `singleflight_source.go` | **Unchanged** (unexported `newSingleflightSource` still used by `Singleflight` decorator) |
| `source_mmdb.go` | **Unchanged** (unexported `openMMDB` still used by `MMDB` creator) |
| `source_ipdb.go` | **Unchanged** |
| `source_xdb.go` | **Unchanged** |
| `source_ip2location.go` | **Unchanged** |
| `errors.go` | **Unchanged** |
| `result.go` | **Unchanged** |

## 9. Tests

All tests that use `Open(WithMMDB(...))`, `Open(WithSource(src), WithCache(...))`, `newCachedSource`, `newSingleflightSource`, `wrapSources` must be rewritten to the new API.

| Test file | Changes |
|-----------|---------|
| `client_test.go` | Replace `WithSource(src)` with `Wrap(src)`; `WithCache(10,0,0)` with `Decorate(Cache(10,0,0))`; `Open(opt)` with `Open(creator)`. Tests that relied on auto-singleflight must add explicit `Decorate(Singleflight())`. |
| `client_cache_edge_test.go` | Rewrite `wrapSources` tests to use `Open` with decorated creators. `newCachedSource` direct tests stay (unexported, still valid). |
| `context_test.go` | `newCachedSource`/`newSingleflightSource` direct tests stay. `Open(WithSource(...))` tests rewrite to `Open(Wrap(...))`. |
| `source_options_test.go` | `Open(WithMMDB(...))` error tests rewrite to `Open(MMDB(...))` or `MMDB(...).Create()`. `openMMDB` direct tests stay. `WithMMDB`→`MMDB` for opener-injection tests. |
| `source_lookup_test.go` | Check for `Open`/`With*` usage; rewrite if present. |
| `source_test.go` | Check for `Open`/`With*` usage; rewrite if present. |

New tests to add:
- `SourceCreator` name validation (dup names detected before `Create`).
- Decorator order: `Decorate(Singleflight()).Decorate(Cache(...))` collapses N concurrent misses to 1 source call.
- `Cache` decorator validation (maxEntries=0, negative TTLs) propagates error through `Create`.
- `Open` closes already-created sources on mid-list failure.

## 10. Verification

```bash
go vet ./...
go test ./...
go test ./cmd/ipgeo/...
golangci-lint run
```