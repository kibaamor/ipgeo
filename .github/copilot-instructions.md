# Copilot instructions for ipgeo

## Build and test commands

This repository uses a Go workspace with two modules:

- Root library module: `github.com/kibaamor/ipgeo`
- CLI module: `github.com/kibaamor/ipgeo/cmd/ipgeo`

Use module-scoped commands when changing dependencies:

- Root dependency changes: `go mod tidy`
- CLI dependency changes: `go -C ./cmd/ipgeo mod tidy`
- Add CLI dependency: `go get -C ./cmd/ipgeo <module>@<version>`

Common validation commands:

- Root tests: `go test ./...`
- CLI tests: `go test ./cmd/ipgeo/...`
- Full project tests: `make test`
- Build CLI: `make build`

Run single tests with package-scoped commands, for example:

- `go test ./cmd/ipgeo/internal/config -run TestLoadFromData_RejectsInvalidConfig`
- `go test ./cmd/ipgeo/internal/updater -run TestNewHTTPClient_UsesConfigTimeout`
- `go test . -run TestOpen_WithCache`

## Architecture notes

The root module is the reusable IP geolocation library. `Client` composes ordered `Source` implementations and wraps them with cache/singleflight behavior during `Open`.

Concrete database readers live in `source_*.go` files. Wrapper sources use different names, for example `cached_source.go` and `singleflight_source.go`; avoid naming wrappers like concrete database sources.

The CLI is a separate module under `cmd/ipgeo`. Its config package loads `$IPGEO_HOME/config.yaml`, writes the embedded default config when missing, and provides source/update settings to the CLI commands and updater.

The CLI updater uses a retryable HTTP client built with `github.com/hashicorp/go-retryablehttp` and still returns a standard `*http.Client` for existing updater helpers.

## Project-specific conventions

Use the current public config names consistently:

- CLI config uses `sources`, source `type`, and `http.timeout`.
- Use `HTTPTimeout` for the configured HTTP timeout because it applies to all CLI HTTP requests.

Before adding cache or HTTP behavior, check the existing dependencies:

- For cache behavior, prefer the existing `github.com/jellydator/ttlcache/v3` dependency.
- For CLI HTTP retries, use `github.com/hashicorp/go-retryablehttp`.

When changing the CLI config format, update these surfaces together:

- Runtime structs and validation in `cmd/ipgeo/internal/config`
- Embedded default YAML in `cmd/ipgeo/internal/config/default_config.yaml`
- Public schema in `cmd/ipgeo/doc/config.schema.json`

Keep CLI config validation aligned across runtime and schema:

- Reject unknown YAML fields at runtime, not only in JSON schema.
- Keep runtime validation and schema validation aligned, including source `type` enum values.
- Add runtime config tests and schema tests for config changes.
- Include positive tests for the embedded default config and negative tests for unknown keys.

For cross-cutting changes to config, cache, source wrapping, updater HTTP behavior, or public API options, run a code-review pass before finishing. Fix substantive findings such as module tidy drift, runtime/schema validation mismatches, naming drift, and behavior exercised by only one CLI path.
