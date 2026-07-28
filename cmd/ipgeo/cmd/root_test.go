package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
)

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
		t.Fatal("update command should not expose --self")
	}
	if updateCmd.Short != "Update source database files" {
		t.Fatalf("update Short = %q, want \"Update source database files\"", updateCmd.Short)
	}
}

func TestRootCmd_UpgradeCommandNotRegistered(t *testing.T) {
	root := buildRootCmd(context.Background(), &config.Config{})

	for _, cmd := range root.Commands() {
		if cmd.Name() == "upgrade" {
			t.Fatal("upgrade command should not be registered")
		}
	}
}

func TestRootCmd_InvalidSourceNameReturnsSourceNameError(t *testing.T) {
	root := buildRootCmd(context.Background(), &config.Config{
		Sources: []config.SourceEntry{
			{
				Name:     "Configured",
				Type:     "mmdb",
				Filename: "configured.mmdb",
				URLs:     []string{"https://example.com/configured.mmdb"},
			},
		},
	})
	root.SetArgs([]string{"--source", "missing", "203.0.113.1"})

	var err error
	out := captureStdout(t, func() {
		err = root.ExecuteContext(context.Background())
	})
	if err == nil {
		t.Fatal("Execute() error = nil")
	}
	wantErr := `source "missing" not found; run 'ipgeo info' to list available sources`
	if err.Error() != wantErr {
		t.Fatalf("Execute() error = %q, want %q", err.Error(), wantErr)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty output", out)
	}
}

func TestInfoCmd_PrintsCompanionFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("IPGEO_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(`
sources:
  - name: Test
    type: xdb
    filename: test.xdb
    urls:
      - https://example.com/test.xdb
    companion_filename: test-v6.xdb
    companion_urls:
      - https://example.com/test-v6.xdb
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "test-v6.xdb"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		root := buildRootCmd(context.Background(), cfg)
		root.SetArgs([]string{"info"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	for _, want := range []string{
		"    companion file:   test-v6.xdb\n",
		"    companion path:   " + filepath.Join(home, "test-v6.xdb") + "\n",
		"    companion exists: yes\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("info output missing %q in:\n%s", want, out)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = original
		_ = r.Close()
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
