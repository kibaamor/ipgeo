package input

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNewReader_ArgsBecomeSingleStream(t *testing.T) {
	r, err := NewReader([]string{"1.1.1.1", "2.2.2.2"}, "ignored-missing-file")
	if err != nil {
		t.Fatalf("NewReader() error: %v", err)
	}
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}
	if got, want := string(data), "1.1.1.1\n2.2.2.2\n"; got != want {
		t.Fatalf("input stream = %q, want %q", got, want)
	}
}

func TestNewReader_FileBecomesStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(path, []byte("client=1.1.1.1"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	r, err := NewReader(nil, path)
	if err != nil {
		t.Fatalf("NewReader() error: %v", err)
	}
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}
	if got, want := string(data), "client=1.1.1.1"; got != want {
		t.Fatalf("input stream = %q, want %q", got, want)
	}
}
