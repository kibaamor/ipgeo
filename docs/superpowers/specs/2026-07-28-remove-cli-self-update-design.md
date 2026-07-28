# Design: Remove CLI self-update

**Date:** 2026-07-28
**Status:** Approved (Approach A)
**Author:** brainstorming session

## Problem

The `ipgeo` CLI ships a self-update feature: the `upgrade` command downloads the
latest release from GitHub, verifies its checksum, and replaces the running
binary in place. This feature is to be removed entirely — command, code,
configuration, documentation, and tests — along with regression guards so it
cannot silently reappear.

## Non-goals

- The `update` command (refreshing IP source database files) is unrelated and
  stays untouched.
- The `downloader` package, `newDownloader` (in `db.go`), `version`/`gitCommit`/
  `buildDate` variables (used by `ipgeo info`), the Makefile, and
  `.goreleaser.yaml` all remain as-is. The release `checksums.txt` is still
  produced and referenced by the README's manual checksum-verification section.

## Scope of removal

### Code

| File | Action |
|---|---|
| `cmd/ipgeo/cmd/upgrade.go` (`!windows`) | Delete file. |
| `cmd/ipgeo/cmd/upgrade_windows.go` | Delete file. |
| `cmd/ipgeo/internal/updater/self.go` (`!windows`) | Delete file (entire self-update logic: `SelfUpdate`, `resolveRelease`, `downloadBinary`, `extractBinary`, `writeNewBinary`, `githubRelease`, `maxBinarySize`). |
| `cmd/ipgeo/cmd/root.go` | Remove the `newUpgradeCmd` registration block (lines ~45-47). `newUpdateCmd` and `newInfoCmd` registrations stay. |

### Configuration

`updater.release_urls` is consumed **only** by self-update; it is removed in
full. The config parser uses `KnownFields(true)`, so deleting the `Updater`
field turns a residual `updater:` block into an unknown-field parse error — an
accepted breaking change (see *Breaking change* below).

| File | Action |
|---|---|
| `cmd/ipgeo/internal/config/config.go` | Delete `UpdaterConfig` struct; delete the `Updater UpdaterConfig` field from `Config`; delete the `release_urls` required-validation block in `validate()` (~lines 206-208). |
| `cmd/ipgeo/internal/config/default_config.yaml` | Delete the `updater:` block (~lines 50-53). |
| `cmd/ipgeo/doc/config.schema.json` | Remove `updater` from root `required`; remove the root `updater` property; remove `$defs/UpdaterConfig`; update the `HTTPConfig` description that mentions `ipgeo upgrade`. |

### Documentation

| File | Action |
|---|---|
| `README.md` | Delete the `ipgeo upgrade` example (~lines 54-55). Keep the manual download + `sha256sum -c checksums.txt` install/verification section. |

### Tests

| File | Action |
|---|---|
| `cmd/ipgeo/cmd/root_test.go` | Rewrite `TestRootCmd_UpdateAndUpgradeCommands` into an update-only test that asserts `update` exists with the right `Short` and no `--self` flag, and **asserts `upgrade` is not registered** on root (regression guard). Strip the `updater:` block from the `TestInfoCmd_PrintsCompanionFile` config fixture. |
| `cmd/ipgeo/internal/config/config_test.go` | Remove `updater:` from the `minimalConfig` constant and all ~8 inline fixtures. Remove the `"missing updater release urls"` rejection case. Remove `TestSchemaRejectsEmptyUpdaterConfig`. Fix the `"missing sources"` case: it currently uses `"updater: {}\n"` as its config — after removal that errors on `updater` being unknown rather than `sources`, so replace it with a config that has no `sources` key (and no `updater`) so it still yields the `sources` error. Add a guard test asserting `updater:` is rejected as an unknown top-level field by both `loadFromData` and the schema. |

## Approach

**Approach A — Full removal + regression guards** (approved over B, "just
delete").

Beyond deleting the feature, Approach A adds two guard tests so a future change
cannot resurrect self-update without touching a test that names it explicitly:

1. Root-command test asserts `upgrade` is absent (`root.Find` returns an error).
2. Config/schema test asserts `updater:` is an unknown top-level field.

## Breaking change

Existing user `config.yaml` files containing an `updater:` block will fail strict
`KnownFields` parsing after removal. Users must delete that block. This is
accepted per the approved decision. There is no migration path (the feature is
being deliberately removed, not relocated).

## Verification

Run the Makefile's build, test, and lint targets for both Go modules:

- `go build ./...` and `go build -C ./cmd/ipgeo ./...`
- `go test github.com/kibaamor/ipgeo/...` and `go test -C ./cmd/ipgeo ./...`
- `golangci-lint run ./...` and `golangci-lint run -C ./cmd/ipgeo --config ../../.golangci.yml ./...`

After removal there should be no dangling references to `upgrade`, `SelfUpdate`,
`UpdaterConfig`, or `release_urls` anywhere in the tree.
