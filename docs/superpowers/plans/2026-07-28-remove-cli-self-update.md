# Remove CLI Self-Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the `ipgeo upgrade` self-update command, its implementation, its `updater` configuration, and related docs/tests — and add regression guards preventing its return.

**Architecture:** Pure removal across four surfaces (command registration, self-update implementation, config struct/validation/schema/default, docs) plus test updates and two guard tests. The `update` command (DB source refresh) and `downloader` package are untouched. Breaking change: existing user configs with an `updater:` block will fail strict `KnownFields` parsing.

**Tech Stack:** Go 1.24, cobra, yaml.v3, JSON Schema (santhosh-tekuri/v6), golangci-lint. Two Go modules: root (`github.com/kibaamor/ipgeo`) and `cmd/ipgeo`.

## Global Constraints

- Config parser uses `yaml.NewDecoder(...).KnownFields(true)` — deleting a struct field turns a residual YAML key into an unknown-field parse error. This is the accepted breaking change.
- Two Go modules exist: build/test/lint must run for both the root module and `cmd/ipgeo`.
- Verify commands referenced throughout:
  - `go build ./...` (root module, run from repo root)
  - `go build -C ./cmd/ipgeo ./...`
  - `go test ./...` (root) and `go test -C ./cmd/ipgeo ./...`
  - `golangci-lint run ./...` and `golangci-lint run -C ./cmd/ipgeo --config ../../.golangci.yml ./...`
- Commit message style (from `git log`): lowercase conventional prefixes — `refactor:`, `docs:`, `fix:`, `feat:`. No body unless needed.
- Do NOT touch: `cmd/ipgeo/cmd/update.go`, `cmd/ipgeo/internal/updater/db.go`, `cmd/ipgeo/internal/downloader/`, `cmd/ipgeo/cmd/info.go`, `Makefile`, `.goreleaser.yaml`, the README manual checksum-verification section.
- Do NOT add comments to code (repo convention: no comments unless asked).

---

## File Structure

**Deleted:**
- `cmd/ipgeo/cmd/upgrade.go` — the `upgrade` cobra command (`!windows`).
- `cmd/ipgeo/cmd/upgrade_windows.go` — Windows stub returning nil.
- `cmd/ipgeo/internal/updater/self.go` — `SelfUpdate` + release resolve/download/verify/extract logic (`!windows`).

**Modified:**
- `cmd/ipgeo/cmd/root.go` — drop `newUpgradeCmd` registration.
- `cmd/ipgeo/internal/config/config.go` — drop `UpdaterConfig`, `Config.Updater`, and the `release_urls` validation.
- `cmd/ipgeo/internal/config/default_config.yaml` — drop `updater:` block.
- `cmd/ipgeo/doc/config.schema.json` — drop `updater` required key + property + `$defs/UpdaterConfig`; fix `HTTPConfig` description.
- `README.md` — drop `ipgeo upgrade` example.
- `cmd/ipgeo/cmd/root_test.go` — rewrite upgrade test into update-only + absence guard; strip `updater:` from info fixture.
- `cmd/ipgeo/internal/config/config_test.go` — strip `updater:` from all fixtures; remove obsolete cases; fix `missing sources` case; add unknown-field guard.

---

## Task 1: Remove the upgrade command and its registration

**Files:**
- Delete: `cmd/ipgeo/cmd/upgrade.go`
- Delete: `cmd/ipgeo/cmd/upgrade_windows.go`
- Modify: `cmd/ipgeo/cmd/root.go` (lines 44-47 — the registration block)
- Test: `cmd/ipgeo/cmd/root_test.go`

**Interfaces:**
- Consumes: none (terminal deletion + registration edit).
- Produces: a root command that no longer has an `upgrade` subcommand; `newUpgradeCmd` no longer exists.

- [ ] **Step 1: Delete the two upgrade command files**

```bash
rm cmd/ipgeo/cmd/upgrade.go cmd/ipgeo/cmd/upgrade_windows.go
```

- [ ] **Step 2: Remove the upgrade registration from root.go**

In `cmd/ipgeo/cmd/root.go`, the current tail of `buildRootCmd` is:

```go
	root.AddCommand(newInfoCmd(cfg))
	root.AddCommand(newUpdateCmd(ctx, cfg))
	if cmd := newUpgradeCmd(ctx, cfg); cmd != nil {
		root.AddCommand(cmd)
	}

	return root
```

Replace those lines with:

```go
	root.AddCommand(newInfoCmd(cfg))
	root.AddCommand(newUpdateCmd(ctx, cfg))

	return root
```

- [ ] **Step 3: Verify the build compiles (expect a test failure in root_test.go, which is fine for now — it still references `upgrade`)**

Run: `go build -C ./cmd/ipgeo ./...`
Expected: BUILD SUCCEEDS (the package builds; only the test file still references `upgrade`).

Do NOT run the full test suite yet — `root_test.go` references the `upgrade` command and will fail. That is fixed in Task 5.

- [ ] **Step 4: Commit**

```bash
git add cmd/ipgeo/cmd/upgrade.go cmd/ipgeo/cmd/upgrade_windows.go cmd/ipgeo/cmd/root.go
git commit -m "refactor: remove upgrade command and registration"
```

---

## Task 2: Delete the self-update implementation

**Files:**
- Delete: `cmd/ipgeo/internal/updater/self.go`

**Interfaces:**
- Consumes: none. After Task 1, nothing calls `updater.SelfUpdate`.
- Produces: an `updater` package containing only `db.go` (DB-source refresh). `newDownloader` (defined in `db.go`) remains available to that package.

- [ ] **Step 1: Delete self.go**

```bash
rm cmd/ipgeo/internal/updater/self.go
```

- [ ] **Step 2: Verify the updater package and cmd build**

Run: `go build -C ./cmd/ipgeo ./...`
Expected: BUILD SUCCEEDS. (`self.go` was the only consumer of `maxBinarySize`/`githubRelease`/etc.; `db.go` stands alone.)

- [ ] **Step 3: Commit**

```bash
git add cmd/ipgeo/internal/updater/self.go
git commit -m "refactor: delete self-update implementation"
```

---

## Task 3: Remove the updater configuration (struct, validation, default, schema)

**Files:**
- Modify: `cmd/ipgeo/internal/config/config.go`
- Modify: `cmd/ipgeo/internal/config/default_config.yaml`
- Modify: `cmd/ipgeo/doc/config.schema.json`

**Interfaces:**
- Consumes: none.
- Produces: a `Config` with no `Updater` field; `validate()` no longer checks `release_urls`; the default config and JSON schema no longer mention `updater`.

- [ ] **Step 1: Remove UpdaterConfig and the Updater field from config.go**

In `cmd/ipgeo/internal/config/config.go`:

(a) Delete the `UpdaterConfig` struct entirely (lines ~33-35):

```go
type UpdaterConfig struct {
	ReleaseURLs []string `yaml:"release_urls"`
}
```

(b) In the `Config` struct, delete the `Updater UpdaterConfig \`yaml:"updater"\`` field (line ~40). The struct becomes:

```go
type Config struct {
	HTTP    HTTPConfig    `yaml:"http"`
	Sources []SourceEntry `yaml:"sources"`
	homeDir string
}
```

- [ ] **Step 2: Remove the release_urls validation from config.go**

In `validate()`, delete this block (lines ~206-208, at the end of the method, just before `return nil`):

```go
	if len(c.Updater.ReleaseURLs) == 0 {
		return errors.New("config updater.release_urls must contain at least one URL")
	}
	return nil
```

Replace with:

```go
	return nil
```

- [ ] **Step 3: Remove the updater block from default_config.yaml**

In `cmd/ipgeo/internal/config/default_config.yaml`, delete the trailing block (lines ~49-53, including the blank line before it):

```yaml

updater:
  # GitHub API URLs used by `ipgeo upgrade` to check for the latest release.
  release_urls:
    - https://api.github.com/repos/kibaamor/ipgeo/releases/latest
```

The file should end after the `DBIPCityLite` source's `urls:` list (the `...dbip-city-lite-{YEAR}-{MONTH}.mmdb.gz` line) with a single trailing newline.

- [ ] **Step 4: Remove updater from the JSON schema**

In `cmd/ipgeo/doc/config.schema.json`:

(a) In the root object's `required` array, remove `"updater"`. It becomes:

```json
    "required": [
        "sources"
    ],
```

(b) In root `properties`, remove the `updater` property (lines ~24-26):

```json
        "updater": {
            "$ref": "#/$defs/UpdaterConfig"
        }
```

(c) In the `HTTPConfig` description (line ~31), remove the `and running \`ipgeo upgrade\`` clause. Change:

```json
            "description": "HTTP settings used when ensuring missing source files, running `ipgeo update`, and running `ipgeo upgrade`.",
```

to:

```json
            "description": "HTTP settings used when ensuring missing source files and running `ipgeo update`.",
```

(d) Delete the entire `UpdaterConfig` definition under `$defs` (lines ~172-197), including its preceding comma handling. The `$defs` object's last entry is now `SourceEntry`; ensure the JSON remains valid (no trailing comma).

- [ ] **Step 5: Verify the config package builds and the schema is valid JSON**

Run: `go build -C ./cmd/ipgeo ./...`
Expected: BUILD SUCCEEDS.

Validate the schema is still well-formed JSON (the `TestDefaultConfigMatchesSchema` test in Task 5 will also exercise this, but catch syntax errors early):

```bash
go run -C ./cmd/ipgeo encoding/json <<'EOF' 2>/dev/null || python3 -c "import json,sys; json.load(open('cmd/ipgeo/doc/config.schema.json')); print('valid')"
EOF
```

If the Go one-liner is awkward, run:
```bash
python3 -c "import json; json.load(open('cmd/ipgeo/doc/config.schema.json')); print('valid JSON')"
```
Expected: `valid JSON`

- [ ] **Step 6: Commit**

```bash
git add cmd/ipgeo/internal/config/config.go cmd/ipgeo/internal/config/default_config.yaml cmd/ipgeo/doc/config.schema.json
git commit -m "refactor: remove updater configuration and schema"
```

---

## Task 4: Remove the upgrade example from README

**Files:**
- Modify: `README.md` (lines ~51-56)

**Interfaces:**
- Consumes: none.
- Produces: README documents only `ipgeo update`; no mention of `ipgeo upgrade`.

- [ ] **Step 1: Delete the upgrade example block from README.md**

In `README.md`, the examples list currently contains (lines ~48-56):

```bash
# Show version, config, and source file status.
ipgeo info

# Refresh configured source database files.
ipgeo update

# Upgrade the ipgeo CLI binary from GitHub Releases.
ipgeo upgrade
```

Replace with:

```bash
# Show version, config, and source file status.
ipgeo info

# Refresh configured source database files.
ipgeo update
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: remove upgrade command example"
```

---

## Task 5: Update config tests and add the unknown-field guard

**Files:**
- Modify: `cmd/ipgeo/internal/config/config_test.go`

**Interfaces:**
- Consumes: Task 3's config changes (no `Updater` field; `updater:` is now an unknown field).
- Produces: config tests with no `updater:` fixtures, plus a guard test `TestLoadFromData_RejectsUpdaterAsUnknownField`.

- [ ] **Step 1: Strip `updater:` from the `minimalConfig` constant**

In `cmd/ipgeo/internal/config/config_test.go`, the `minimalConfig` const (lines ~15-25) is:

```go
const minimalConfig = `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
updater:
  release_urls:
    - https://example.com/releases/latest
`
```

Replace with:

```go
const minimalConfig = `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
`
```

- [ ] **Step 2: Strip the `updater:` block from every inline fixture**

Remove these `updater:` blocks (each is a 3-line `updater:` / `  release_urls:` / `    - https://example.com/releases/latest` group) from the following tests. The surrounding `sources:` block of each fixture stays; only the `updater:` block is deleted.

Tests to edit (search for `updater:` in the file; every remaining match must be removed except the one inside the new guard test added in Step 5):

1. `TestLoadFromData_ParsesHTTPTimeout` (lines ~90-92)
2. `TestLoadFromData_ParsesRetryConfig` (lines ~125-127)
3. `TestLoadFromData_NormalizesSourceIdentityFields` (lines ~214-216)
4. `TestLoadFromData_RejectsInvalidConfig` → `"rooted filename resolved under home"` case (lines ~462-464)
5. `TestLoadFromData_RejectsInvalidConfig` → `"normalized parent directory filename"` case (lines ~489-491)
6. `TestSchemaAcceptsRuntimeResolvedSourceFilenames` (lines ~699-701)

Note: cases #4 and #5 are in the `wantErr: ""` group — they must still parse successfully, which they will once `updater:` is gone.

- [ ] **Step 3: Remove the obsolete `"missing updater release urls"` rejection case**

In the `TestLoadFromData_RejectsInvalidConfig` table, delete this case entirely (lines ~351-362):

```go
		{
			name: "missing updater release urls",
			config: `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
`,
			wantErr: "updater.release_urls",
		},
```

- [ ] **Step 4: Fix the `"missing sources"` case**

The `"missing sources"` case currently uses `"updater: {}\n"` as its config so that the *only* error is the missing `sources`. Now that `updater:` is unknown, that config would error on `updater` instead. Change the case to a config with neither `sources` nor `updater`:

```go
		{
			name:    "missing sources",
			config:  "http: {timeout: 30m}\n",
			wantErr: "sources",
		},
```

(`http` alone is valid and known; the missing `sources` is the error surfaced.)

- [ ] **Step 5: Replace `TestSchemaRejectsEmptyUpdaterConfig` with an unknown-field guard**

Delete the entire `TestSchemaRejectsEmptyUpdaterConfig` function (lines ~710-724) and replace it with two guard tests — one at the `loadFromData` level and one at the schema level — asserting `updater:` is rejected as unknown:

```go
func TestLoadFromData_RejectsUpdaterAsUnknownField(t *testing.T) {
	_, err := loadFromData([]byte(`
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
updater:
  release_urls:
    - https://example.com/releases/latest
`), t.TempDir())
	if err == nil {
		t.Fatal("loadFromData() error = nil, want unknown-field error for updater")
	}
}

func TestSchemaRejectsUpdaterAsUnknownField(t *testing.T) {
	schema := loadConfigSchema(t)
	doc := yamlToJSONCompatible(t, []byte(`
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
updater:
  release_urls:
    - https://example.com/releases/latest
`))
	if err := schema.Validate(doc); err == nil {
		t.Fatal("schema accepted unknown top-level field updater")
	}
}
```

- [ ] **Step 6: Run the config tests**

Run: `go test -C ./cmd/ipgeo ./internal/config/`
Expected: PASS. If a fixture still references `updater:` (parse error) or a removed case is still invoked, fix the remaining occurrence.

- [ ] **Step 7: Commit**

```bash
git add cmd/ipgeo/internal/config/config_test.go
git commit -m "test: drop updater fixtures, reject updater as unknown field"
```

---

## Task 6: Rewrite the root-command test with an upgrade-absence guard

**Files:**
- Modify: `cmd/ipgeo/cmd/root_test.go`

**Interfaces:**
- Consumes: Task 1 (no `upgrade` subcommand) and Task 3 (no `updater` config — affects the info fixture).
- Produces: `TestRootCmd_UpdateAndUpgradeCommands` replaced by `TestRootCmd_UpdateCommand` + absence assertion.

- [ ] **Step 1: Replace the upgrade test with an update-only + absence guard**

In `cmd/ipgeo/cmd/root_test.go`, replace the entire `TestRootCmd_UpdateAndUpgradeCommands` function (lines ~14-41) with:

```go
func TestRootCmd_UpdateCommand(t *testing.T) {
	root := buildRootCmd(context.Background(), &config.Config{})

	updateCmd, _, err := root.Find([]string{"update"})
	if err != nil {
		t.Fatalf("Find(update) error: %v", err)
	}
	if updateCmd == nil || updateCmd.Use != "update" {
		t.Fatalf("Find(update) = %v, want update command", updateCmd)
	}
	if updateCmd.Flags().Lookup("self") != nil {
		t.Fatal("update command still exposes --self; use upgrade for CLI self-update")
	}
	if updateCmd.Short != "Update source database files" {
		t.Fatalf("update Short = %q, want \"Update source database files\"", updateCmd.Short)
	}
}

func TestRootCmd_UpgradeCommandNotRegistered(t *testing.T) {
	root := buildRootCmd(context.Background(), &config.Config{})

	if _, _, err := root.Find([]string{"upgrade"}); err == nil {
		t.Fatal("upgrade command should not be registered")
	}
}
```

- [ ] **Step 2: Strip the `updater:` block from the info test fixture**

In `TestInfoCmd_PrintsCompanionFile`, the config written to disk (lines ~75-88) ends with:

```yaml
    companion_filename: test-v6.xdb
    companion_urls:
      - https://example.com/test-v6.xdb
updater:
  release_urls:
    - https://example.com/releases/latest
```

Remove the `updater:` block so the config ends after the companion URLs:

```yaml
    companion_filename: test-v6.xdb
    companion_urls:
      - https://example.com/test-v6.xdb
```

- [ ] **Step 3: Run the root-command tests**

Run: `go test -C ./cmd/ipgeo ./cmd/`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/ipgeo/cmd/root_test.go
git commit -m "test: assert upgrade command is not registered"
```

---

## Task 7: Full verification

**Files:** none (verification only).

**Interfaces:** none.

- [ ] **Step 1: Confirm no dangling references remain**

Run a search across the whole tree for any leftover self-update identifiers:

```bash
rg -n 'upgrade|SelfUpdate|UpdaterConfig|release_urls|ReleaseURLs|newUpgradeCmd' --type go --type yaml --type md --type json || echo "none found"
```

Expected: `none found` (the README's "checksums.txt" manual-verification section and `.goreleaser.yaml` `checksum` block intentionally remain and do not match these terms).

If matches appear in generated/irrelevant spots, inspect each; the only acceptable remaining occurrences would be unrelated uses of the word "upgrade" outside the CLI context (there should be none in this repo).

- [ ] **Step 2: Build both modules**

Run:
```bash
go build ./...
go build -C ./cmd/ipgeo ./...
```
Expected: both succeed.

- [ ] **Step 3: Test both modules**

Run:
```bash
go test ./...
go test -C ./cmd/ipgeo ./...
```
Expected: all PASS.

- [ ] **Step 4: Lint both modules**

Run:
```bash
golangci-lint run ./...
golangci-lint run -C ./cmd/ipgeo --config ../../.golangci.yml ./...
```
Expected: no issues.

- [ ] **Step 5: Run go mod tidy (sanity, no commit unless changed)**

Run:
```bash
go mod tidy
go mod tidy -C ./cmd/ipgeo
git diff --exit-code go.mod go.sum cmd/ipgeo/go.mod cmd/ipgeo/go.sum && echo "no dep changes"
```
Expected: `no dep changes` (removing `self.go` should not change dependencies, since `archive/tar`, `archive/zip`, `compress/gzip`, `crypto/sha256`, `encoding/hex`, `encoding/json` are all stdlib; the `downloader` import stays via `db.go`).

If deps did change, stage and commit with `go mod tidy` after reviewing.

---

## Notes for the implementer

- Tasks 1-4 are pure removals/edits with no new tests; their verification is the build. Tasks 5-6 carry the test updates and regression guards. Task 7 is the final gate.
- If a verify step fails, stop and fix before moving on — do not batch broken states across tasks.
- The `version`/`gitCommit`/`buildDate` variables in `info.go` are NOT removed — `ipgeo info` and the ldflags still use them.
