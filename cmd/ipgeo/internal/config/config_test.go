package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

const minimalConfig = `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
`

func TestLoad_CreatesDefaultConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("IPGEO_HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if got := cfg.HTTPTimeout(); got != 30*time.Minute {
		t.Fatalf("HTTPTimeout() = %v, want 30m", got)
	}
	if len(cfg.Sources) != 3 {
		t.Fatalf("sources len = %d, want 3", len(cfg.Sources))
	}
	if cfg.Sources[0].Type != "xdb" {
		t.Fatalf("first source type = %q, want xdb", cfg.Sources[0].Type)
	}

	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	content := string(data)
	for _, want := range []string{"http:", "timeout: 30m", "sources:", "type: xdb", "companion_filename:", "companion_urls:"} {
		if !strings.Contains(content, want) {
			t.Fatalf("generated config missing %q:\n%s", want, content)
		}
	}
}

func TestLoad_ReplacesEmptyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("IPGEO_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(" \n\t"), 0o644); err != nil {
		t.Fatalf("write empty config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cfg.Sources) != 3 {
		t.Fatalf("sources len = %d, want 3", len(cfg.Sources))
	}
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read rewritten config: %v", err)
	}
	if !strings.Contains(string(data), "sources:") {
		t.Fatalf("rewritten config missing sources:\n%s", data)
	}
}

func TestLoadFromData_ParsesHTTPTimeout(t *testing.T) {
	cfg, err := loadFromData([]byte(`
http:
  timeout: 5s
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
`), t.TempDir())
	if err != nil {
		t.Fatalf("loadFromData() error: %v", err)
	}
	if got := cfg.HTTPTimeout(); got != 5*time.Second {
		t.Fatalf("HTTPTimeout() = %v, want 5s", got)
	}
}

func TestLoadFromData_DefaultHTTPTimeout(t *testing.T) {
	cfg, err := loadFromData([]byte(minimalConfig), t.TempDir())
	if err != nil {
		t.Fatalf("loadFromData() error: %v", err)
	}
	if got := cfg.HTTPTimeout(); got != 30*time.Minute {
		t.Fatalf("HTTPTimeout() = %v, want 30m", got)
	}
}

func TestLoadFromData_NormalizesSourceIdentityFields(t *testing.T) {
	cfg, err := loadFromData([]byte(`
sources:
  - type: " mmdb "
    name: " test "
    filename: " test.mmdb "
    urls:
      - https://example.com/test.mmdb
    companion_filename: " asn.mmdb "
    companion_urls:
      - https://example.com/asn.mmdb
`), t.TempDir())
	if err != nil {
		t.Fatalf("loadFromData() error: %v", err)
	}
	source := cfg.Sources[0]
	if source.Name != "test" || source.Type != "mmdb" || source.Filename != "test.mmdb" || source.CompanionFilename != "asn.mmdb" {
		t.Fatalf("source was not normalized: %#v", source)
	}
}

func TestLoadFromData_RejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "invalid duration",
			config: `
http:
  timeout: nope
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
`,
			wantErr: "http.timeout",
		},
		{
			name: "non-positive duration",
			config: `
http:
  timeout: 0s
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
`,
			wantErr: "greater than 0",
		},
		{
			name:    "missing sources",
			config:  "updater: {}\n",
			wantErr: "sources",
		},
		{
			name: "missing urls",
			config: `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
`,
			wantErr: "urls",
		},
		{
			name: "unsupported type",
			config: `
sources:
  - type: mddb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
`,
			wantErr: "not supported",
		},
		{
			name: "duplicate source name",
			config: `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
  - type: xdb
    name: test
    filename: test.xdb
    urls:
      - https://example.com/test.xdb
`,
			wantErr: "name \"test\" conflicts",
		},
		{
			name: "duplicate source name after trim",
			config: `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
  - type: xdb
    name: " test "
    filename: test.xdb
    urls:
      - https://example.com/test.xdb
`,
			wantErr: "name \"test\" conflicts",
		},
		{
			name: "duplicate filename",
			config: `
sources:
  - type: mmdb
    name: test-mmdb
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
  - type: xdb
    name: test-xdb
    filename: test.mmdb
    urls:
      - https://example.com/test.xdb
`,
			wantErr: "filename \"test.mmdb\" conflicts",
		},
		{
			name: "path-equivalent duplicate filename",
			config: `
sources:
  - type: mmdb
    name: test-mmdb
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
  - type: xdb
    name: test-xdb
    filename: ./test.mmdb
    urls:
      - https://example.com/test.xdb
`,
			wantErr: "filename \"./test.mmdb\" conflicts",
		},
		{
			name: "rooted filename resolved under home",
			config: `
sources:
  - type: xdb
    name: test
    filename: /test.mmdb
    urls:
      - https://example.com/test.xdb
`,
			wantErr: "",
		},
		{
			name: "parent directory filename",
			config: `
sources:
  - type: xdb
    name: test
    filename: ../test.mmdb
    urls:
      - https://example.com/test.xdb
`,
			wantErr: "under IPGEO_HOME",
		},
		{
			name: "normalized parent directory filename",
			config: `
sources:
  - type: xdb
    name: test
    filename: nested/../test.mmdb
    urls:
      - https://example.com/test.xdb
`,
			wantErr: "",
		},
		{
			name: "parent directory companion filename",
			config: `
sources:
  - type: xdb
    name: test
    filename: test.xdb
    urls:
      - https://example.com/test.xdb
    companion_filename: ../test-v6.xdb
    companion_urls:
      - https://example.com/test-v6.xdb
`,
			wantErr: "under IPGEO_HOME",
		},
		{
			name: "duplicate companion filename",
			config: `
sources:
  - type: mmdb
    name: test-mmdb
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
    companion_filename: asn.mmdb
    companion_urls:
      - https://example.com/asn.mmdb
  - type: xdb
    name: test-xdb
    filename: asn.mmdb
    urls:
      - https://example.com/test.xdb
`,
			wantErr: "filename \"asn.mmdb\" conflicts",
		},
		{
			name: "filename conflicts with same source companion filename",
			config: `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
    companion_filename: test.mmdb
    companion_urls:
      - https://example.com/asn.mmdb
`,
			wantErr: "companion_filename \"test.mmdb\" conflicts",
		},
		{
			name: "companion filename without urls",
			config: `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
    companion_filename: asn.mmdb
`,
			wantErr: "companion_filename and companion_urls",
		},
		{
			name: "companion urls without filename",
			config: `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
    companion_urls:
      - https://example.com/asn.mmdb
`,
			wantErr: "companion_filename and companion_urls",
		},
		{
			name: "unknown source key",
			config: `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    unknown: value
    urls:
      - https://example.com/test.mmdb
`,
			wantErr: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFromData([]byte(tt.config), t.TempDir())
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("loadFromData() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("loadFromData() error = nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestSourceFilenameConflictKey_FoldsCaseInsensitivePlatforms(t *testing.T) {
	home := t.TempDir()

	for _, goos := range []string{"windows", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			upper := sourceFilenameConflictKey(home, "Geo.mmdb", goos)
			lower := sourceFilenameConflictKey(home, "geo.mmdb", goos)
			if upper != lower {
				t.Fatalf("%s keys differ: %q != %q", goos, upper, lower)
			}
		})
	}

	upper := sourceFilenameConflictKey(home, "Geo.mmdb", "linux")
	lower := sourceFilenameConflictKey(home, "geo.mmdb", "linux")
	if upper == lower {
		t.Fatalf("linux keys should preserve case: %q == %q", upper, lower)
	}
}

func TestDefaultConfigMatchesSchema(t *testing.T) {
	schema := loadConfigSchema(t)
	doc := yamlToJSONCompatible(t, []byte(defaultConfig))
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("default config does not match schema: %v", err)
	}
}

func TestSchemaRejectsUnknownFields(t *testing.T) {
	schema := loadConfigSchema(t)
	tests := []struct {
		name   string
		config string
	}{
		{
			name: "unknown top-level key",
			config: `
unknown: value
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
`,
		},
		{
			name: "unknown source key",
			config: `
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    unknown: value
    urls:
      - https://example.com/test.mmdb
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := yamlToJSONCompatible(t, []byte(tt.config))
			if err := schema.Validate(doc); err == nil {
				t.Fatal("schema accepted unknown field")
			}
		})
	}
}

func TestSchemaAcceptsRuntimeResolvedSourceFilenames(t *testing.T) {
	schema := loadConfigSchema(t)
	tests := []struct {
		name     string
		filename string
	}{
		{name: "absolute unix path", filename: "/test.mmdb"},
		{name: "absolute windows path", filename: `C:\test.mmdb`},
		{name: "windows rooted path", filename: `\test.mmdb`},
		{name: "windows unc path", filename: `\\server\share\test.mmdb`},
		{name: "parent directory path", filename: "../test.mmdb"},
		{name: "current directory path", filename: "."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := yamlToJSONCompatible(t, []byte(`
sources:
  - type: mmdb
    name: test
    filename: '`+tt.filename+`'
    urls:
      - https://example.com/test.mmdb
`))
			if err := schema.Validate(doc); err != nil {
				t.Fatalf("schema rejected filename left to runtime validation: %v", err)
			}
		})
	}
}

func TestSchemaAllowsEmptyUpdaterConfig(t *testing.T) {
	schema := loadConfigSchema(t)
	doc := yamlToJSONCompatible(t, []byte(`
sources:
  - type: mmdb
    name: test
    filename: test.mmdb
    urls:
      - https://example.com/test.mmdb
updater: {}
`))
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("schema rejected empty updater config: %v", err)
	}
}

func loadConfigSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "doc", "config.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("config.schema.json", doc); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile("config.schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return schema
}

func yamlToJSONCompatible(t *testing.T, data []byte) any {
	t.Helper()

	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse yaml: %v", err)
	}
	return normalizeYAML(doc)
}

func normalizeYAML(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = normalizeYAML(v)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k.(string)] = normalizeYAML(v)
		}
		return out
	case []any:
		for i, item := range value {
			value[i] = normalizeYAML(item)
		}
		return value
	default:
		return value
	}
}
