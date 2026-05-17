package output

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"strings"
	"testing"

	"github.com/kibaamor/ipgeo"
)

var testAddr = netip.MustParseAddr("1.2.3.4")

func TestStructuredRenderer_WriteResult(t *testing.T) {
	var buf bytes.Buffer
	w := NewStructuredRenderer(&buf)
	result := ipgeo.NewResult(testAddr, "db", "CN", "China", "", "", "", 0)

	if err := w.WriteResult(&result); err != nil {
		t.Fatalf("WriteResult() error: %v", err)
	}
	if err := w.WriteResult(&result); err != nil {
		t.Fatalf("WriteResult() error: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 JSON lines (one per IP match), got %d: %q", len(lines), out)
	}
	for _, l := range lines {
		if !strings.Contains(l, "1.2.3.4") {
			t.Errorf("expected each line to contain IP, got %q", l)
		}
	}
}

func TestStructuredRenderer_WriteRaw_NoOutput(t *testing.T) {
	var buf bytes.Buffer
	w := NewStructuredRenderer(&buf)
	if err := w.WriteRaw([]byte("no IPs here\n")); err != nil {
		t.Fatalf("WriteRaw() error: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for raw text, got %q", buf.String())
	}
}

func TestStructuredRenderer_WriteResult_PropagatesWriteError(t *testing.T) {
	writeErr := errors.New("write failed")
	w := NewStructuredRenderer(errWriter{err: writeErr})
	result := ipgeo.NewResult(testAddr, "db", "CN", "China", "", "", "", 0)

	if err := w.WriteResult(&result); err != nil {
		t.Fatalf("WriteResult() error = %v, want nil before buffered flush", err)
	}
	err := w.Flush()
	if !errors.Is(err, writeErr) {
		t.Fatalf("Flush() error = %v, want writeErr", err)
	}
}

type errWriter struct {
	err error
}

func (w errWriter) Write([]byte) (int, error) {
	return 0, w.err
}

var _ io.Writer = errWriter{}
