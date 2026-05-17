package output

import (
	"bytes"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kibaamor/ipgeo"
)

func TestNewColoredFormatter(t *testing.T) {
	f := newColoredFormatter(io.Discard)
	if f == nil {
		t.Fatal("NewColoredFormatter returned nil")
	}
}

func TestNewRenderer_File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	renderer, closer, err := NewRenderer(path, false)
	if err != nil {
		t.Fatalf("NewRenderer() error: %v", err)
	}
	if err := renderer.WriteRaw([]byte("client=1.2.3.4")); err != nil {
		t.Fatalf("WriteRaw() error: %v", err)
	}
	if err := renderer.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if got := string(data); got != "client=1.2.3.4" {
		t.Fatalf("output = %q, want written bytes", got)
	}
}

func TestNewRenderer_StdoutPreservesTerminalDescriptor(t *testing.T) {
	_, closer, err := NewRenderer("", false)
	if err != nil {
		t.Fatalf("NewRenderer() error: %v", err)
	}
	if _, ok := closer.(interface{ Fd() uintptr }); !ok {
		t.Fatalf("stdout closer = %T, want terminal file descriptor access", closer)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestColoredFormatter_FormatAnnotation_Empty(t *testing.T) {
	f := newColoredFormatter(io.Discard)
	addr := netip.MustParseAddr("1.2.3.4")
	empty := ipgeo.NewResult(addr, "db", "", "", "", "", "", 0)
	if got := f.formatAnnotation(&empty); got != "" {
		t.Errorf("FormatAnnotation with empty result should return empty string, got %q", got)
	}
}

func TestColoredFormatter_FormatAnnotation_WithResults(t *testing.T) {
	f := newColoredFormatter(io.Discard)
	addr := netip.MustParseAddr("1.2.3.4")
	r := ipgeo.NewResult(addr, "db", "CN", "China", "", "", "", 0)
	if got := f.formatAnnotation(&r); !strings.Contains(got, "China") {
		t.Errorf("FormatAnnotation should contain result data, got %q", got)
	}
}

func TestInlineRenderer_HandleLine_NoIPs(t *testing.T) {
	var buf bytes.Buffer
	w := NewInlineRenderer(&buf)
	line := "this line has no IP addresses"
	if err := w.WriteRaw([]byte(line + "\n")); err != nil {
		t.Fatalf("WriteRaw() error: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}
	if !strings.Contains(buf.String(), line) {
		t.Errorf("output should contain original line: %q", buf.String())
	}
}

func TestInlineRenderer_HandleLine_WithIP(t *testing.T) {
	var buf bytes.Buffer
	w := NewInlineRenderer(&buf)
	line := "connection from 1.2.3.4 to server"
	addr := netip.MustParseAddr("1.2.3.4")
	result := ipgeo.NewResult(addr, "db", "CN", "China", "", "", "", 0)

	if err := w.WriteRaw([]byte(line + "\n")); err != nil {
		t.Fatalf("WriteRaw() error: %v", err)
	}
	if err := w.WriteResult(&result); err != nil {
		t.Fatalf("WriteResult() error: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "connection from") {
		t.Errorf("output should preserve non-IP text: %q", out)
	}
	if !strings.Contains(out, "1.2.3.4") {
		t.Errorf("output should contain IP: %q", out)
	}
	if !strings.Contains(out, "China") {
		t.Errorf("output should contain result data: %q", out)
	}
}
