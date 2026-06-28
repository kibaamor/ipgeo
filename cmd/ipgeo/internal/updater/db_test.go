package updater

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFile_ReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")
	newPath := filepath.Join(dir, "download.tmp")

	if err := os.WriteFile(destPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write destination: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}

	if err := os.Rename(newPath, destPath); err != nil {
		t.Fatalf("os.Rename() error: %v", err)
	}
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(data) != "new" {
		t.Fatalf("destination content = %q, want new", data)
	}
	if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement file still exists or stat failed with different error: %v", err)
	}
}
