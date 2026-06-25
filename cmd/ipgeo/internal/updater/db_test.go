package updater

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
)

func TestCopyWithLimitRejectsOversizedContent(t *testing.T) {
	var dst bytes.Buffer
	err := copyWithLimit(&dst, strings.NewReader("abcdef"), 5)
	if err == nil {
		t.Fatal("copyWithLimit() error = nil, want size error")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("copyWithLimit() error = %v, want size error", err)
	}
}

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

	if err := replaceFile(newPath, destPath); err != nil {
		t.Fatalf("replaceFile() error: %v", err)
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

func TestCopyWithLimitPropagatesReadError(t *testing.T) {
	readErr := errors.New("read failed")
	err := copyWithLimit(io.Discard, errReader{err: readErr}, 5)
	if !errors.Is(err, readErr) {
		t.Fatalf("copyWithLimit() error = %v, want readErr", err)
	}
}

func TestDownloadRawToPreservesGzipBytes(t *testing.T) {
	var gzipped bytes.Buffer
	gz := gzip.NewWriter(&gzipped)
	if _, err := gz.Write([]byte("archive bytes")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(gzipped.Bytes())
	}))
	defer server.Close()

	var raw bytes.Buffer
	if err := downloadRawTo(server.URL+"/ipgeo.tar.gz", &raw, server.Client(), maxBinarySize); err != nil {
		t.Fatalf("downloadRawTo() error: %v", err)
	}
	if !bytes.Equal(raw.Bytes(), gzipped.Bytes()) {
		t.Fatal("downloadRawTo() changed gzip asset bytes")
	}

	var decompressed bytes.Buffer
	if err := downloadTo(server.URL+"/ipgeo.tar.gz", &decompressed, server.Client()); err != nil {
		t.Fatalf("downloadTo() error: %v", err)
	}
	if decompressed.String() != "archive bytes" {
		t.Fatalf("downloadTo() = %q, want decompressed payload", decompressed.String())
	}
}

type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestNewHTTPClient_UsesConfigValues(t *testing.T) {
	r3 := 3
	cfg := &config.Config{HTTP: config.HTTPConfig{
		Timeout:     "5s",
		RetryWaitMin: "1s",
		RetryWaitMax: "10s",
		RetryMax:     &r3,
	}}
	client := newHTTPClient(cfg)
	if client.Timeout != 5*time.Second {
		t.Fatalf("client timeout = %v, want 5s", client.Timeout)
	}
	transport, ok := client.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("client transport = %T, want *retryablehttp.RoundTripper", client.Transport)
	}
	if transport.Client == nil {
		t.Fatal("retryable transport client is nil")
	}
	if transport.Client.RetryWaitMin != 1*time.Second {
		t.Fatalf("retry wait min = %v, want 1s", transport.Client.RetryWaitMin)
	}
	if transport.Client.RetryWaitMax != 10*time.Second {
		t.Fatalf("retry wait max = %v, want 10s", transport.Client.RetryWaitMax)
	}
	if transport.Client.RetryMax != 3 {
		t.Fatalf("retry max = %d, want 3", transport.Client.RetryMax)
	}
}

func TestNewHTTPClient_RetryMaxZeroDisablesRetries(t *testing.T) {
	r0 := 0
	cfg := &config.Config{HTTP: config.HTTPConfig{
		RetryMax: &r0,
	}}
	client := newHTTPClient(cfg)
	transport, ok := client.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("client transport = %T, want *retryablehttp.RoundTripper", client.Transport)
	}
	if transport.Client.RetryMax != 0 {
		t.Fatalf("retry max = %d, want 0", transport.Client.RetryMax)
	}
}

func TestNewHTTPClient_RetryWaitMaxZeroHonored(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{
		RetryWaitMax: "0s",
	}}
	client := newHTTPClient(cfg)
	transport, ok := client.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("client transport = %T, want *retryablehttp.RoundTripper", client.Transport)
	}
	if transport.Client.RetryWaitMax != 0 {
		t.Fatalf("retry wait max = %v, want 0s", transport.Client.RetryWaitMax)
	}
}
