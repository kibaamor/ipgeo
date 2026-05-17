package clirun

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"strings"
	"testing"

	"github.com/kibaamor/ipgeo"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/output"
)

func TestStreamInput_ProcessesInputWithInlineRenderer(t *testing.T) {
	var buf bytes.Buffer
	addr := netip.MustParseAddr("1.2.3.4")
	result := ipgeo.NewResult(addr, "db", "CN", "China", "", "", "", 0)

	err := streamInput(strings.NewReader("client=1.2.3.4"), output.NewInlineRenderer(&buf), func(got netip.Addr) (*ipgeo.Result, error) {
		if got == addr {
			return &result, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("streamInput() error: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "1.2.3.4") || !strings.Contains(got, "China") {
		t.Fatalf("stream output = %q, want input with GEO annotation", got)
	}
}

func TestStreamInput_FlushesAfterEachRead(t *testing.T) {
	addr := netip.MustParseAddr("1.2.3.4")
	result := ipgeo.NewResult(addr, "db", "CN", "China", "", "", "", 0)
	renderer := &flushProbeRenderer{}
	reader := &flushProbeReader{renderer: renderer, data: []byte("client=1.2.3.4\n")}

	err := streamInput(reader, renderer, func(got netip.Addr) (*ipgeo.Result, error) {
		if got == addr {
			return &result, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("streamInput() error: %v", err)
	}
	if renderer.flushes == 0 {
		t.Fatal("renderer was not flushed")
	}
}

func TestStreamInput_PropagatesLookupError(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	var buf bytes.Buffer

	err := streamInput(strings.NewReader("before 1.2.3.4 after 2.3.4.5"), output.NewInlineRenderer(&buf), func(netip.Addr) (*ipgeo.Result, error) {
		return nil, lookupErr
	})
	if !errors.Is(err, lookupErr) {
		t.Fatalf("streamInput() error = %v, want lookupErr", err)
	}
	if strings.Contains(buf.String(), "2.3.4.5") {
		t.Fatalf("streamInput continued after lookup error: %q", buf.String())
	}
}

func TestStreamInput_FlushesBufferedOutputAfterEOFLookupError(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	var buf bytes.Buffer
	first := netip.MustParseAddr("1.2.3.4")
	second := netip.MustParseAddr("2.3.4.5")
	result := ipgeo.NewResult(first, "db", "CN", "China", "", "", "", 0)

	err := streamInput(strings.NewReader("first 1.2.3.4 second 2.3.4.5"), output.NewStructuredRenderer(&buf), func(addr netip.Addr) (*ipgeo.Result, error) {
		switch addr {
		case first:
			return &result, nil
		case second:
			return nil, lookupErr
		default:
			return nil, nil
		}
	})
	if !errors.Is(err, lookupErr) {
		t.Fatalf("streamInput() error = %v, want lookupErr", err)
	}
	if got := buf.String(); !strings.Contains(got, "1.2.3.4") {
		t.Fatalf("buffered output was not flushed after EOF lookup error: %q", got)
	}
}

func TestStreamInput_FlushesTrailingInputAfterReadError(t *testing.T) {
	readErr := errors.New("read failed")
	var buf bytes.Buffer

	err := streamInput(&errAfterBytesReader{data: []byte("client=1.2.3.4"), err: readErr}, output.NewInlineRenderer(&buf), func(netip.Addr) (*ipgeo.Result, error) {
		return nil, nil
	})
	if !errors.Is(err, readErr) {
		t.Fatalf("streamInput() error = %v, want readErr", err)
	}
	if got := buf.String(); got != "client=1.2.3.4" {
		t.Fatalf("stream output = %q, want trailing input flushed after read error", got)
	}
}

type errAfterBytesReader struct {
	data []byte
	err  error
	done bool
}

func (r *errAfterBytesReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.done = true
	return n, r.err
}

type flushProbeReader struct {
	renderer *flushProbeRenderer
	data     []byte
	read     int
}

func (r *flushProbeReader) Read(p []byte) (int, error) {
	r.read++
	if r.read == 1 {
		return copy(p, r.data), nil
	}
	if r.renderer.flushes == 0 {
		return 0, errors.New("renderer was not flushed before next read")
	}
	return 0, io.EOF
}

type flushProbeRenderer struct {
	flushes int
}

func (r *flushProbeRenderer) WriteRaw([]byte) error { return nil }

func (r *flushProbeRenderer) WriteResult(*ipgeo.Result) error { return nil }

func (r *flushProbeRenderer) Flush() error {
	r.flushes++
	return nil
}
