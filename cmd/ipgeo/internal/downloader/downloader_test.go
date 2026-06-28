package downloader

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

func TestNewDownloader_UsesConfigValues(t *testing.T) {
	cfg := &Config{
		Timeout:      5 * time.Second,
		RetryWaitMin: 1 * time.Second,
		RetryWaitMax: 10 * time.Second,
		RetryMax:     3,
	}
	d := New(cfg)

	if d.httpClient.Timeout != 5*time.Second {
		t.Fatalf("client timeout = %v, want 5s", d.httpClient.Timeout)
	}
	transport, ok := d.httpClient.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("client transport = %T, want *retryablehttp.RoundTripper", d.httpClient.Transport)
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

func TestNewDownloader_RetryMaxZeroDisablesRetries(t *testing.T) {
	cfg := &Config{
		RetryMax: 0,
	}
	d := New(cfg)
	transport, ok := d.httpClient.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("client transport = %T, want *retryablehttp.RoundTripper", d.httpClient.Transport)
	}
	if transport.Client.RetryMax != 0 {
		t.Fatalf("retry max = %d, want 0", transport.Client.RetryMax)
	}
}

func TestNewDownloader_RetryWaitMaxZeroHonored(t *testing.T) {
	cfg := &Config{
		RetryWaitMax: 0,
	}
	d := New(cfg)
	transport, ok := d.httpClient.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("client transport = %T, want *retryablehttp.RoundTripper", d.httpClient.Transport)
	}
	if transport.Client.RetryWaitMax != 0 {
		t.Fatalf("retry wait max = %v, want 0s", transport.Client.RetryWaitMax)
	}
}

func TestDownloadFiles_Direct(t *testing.T) {
	payload := []byte("hello world db data")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "19")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")

	d := &Downloader{httpClient: server.Client()}

	err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name: "test",
		URLs: []string{server.URL},
		Path: destPath,
	}})
	if err != nil {
		t.Fatalf("DownloadFiles() error: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("downloaded data = %q, want %q", data, payload)
	}
}

func TestDownloadFiles_URLFallback(t *testing.T) {
	payload := []byte("fallback data")
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer badServer.Close()

	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "13")
		_, _ = w.Write(payload)
	}))
	defer goodServer.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")

	d := &Downloader{httpClient: &http.Client{}}

	urls := []string{badServer.URL, goodServer.URL}
	err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name: "fallback-test",
		URLs: urls,
		Path: destPath,
	}})
	if err != nil {
		t.Fatalf("DownloadFiles() error: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("downloaded data = %q, want %q", data, payload)
	}
}

func TestDownloadFiles_AllURLsFail(t *testing.T) {
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer badServer.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")

	d := &Downloader{httpClient: &http.Client{}}

	err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name: "fail-test",
		URLs: []string{badServer.URL, badServer.URL},
		Path: destPath,
	}})
	if err == nil {
		t.Fatal("DownloadFiles() error = nil, want error")
	}
	var fileErrs FileErrors
	if !errors.As(err, &fileErrs) {
		t.Fatalf("DownloadFiles() error type = %T, want FileErrors", err)
	}
	if !strings.Contains(err.Error(), "HTTP 404 Not Found") {
		t.Fatalf("DownloadFiles() error = %v, want HTTP 404 Not Found", err)
	}
}

func TestDownloadFiles_Gzip_RawBytes(t *testing.T) {
	payload := []byte("hello world db data")
	var gzBuf bytes.Buffer
	gzWriter := gzip.NewWriter(&gzBuf)
	if _, err := gzWriter.Write(payload); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	compressed := gzBuf.Bytes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")

	d := &Downloader{httpClient: server.Client()}

	gzipURL := server.URL + "/db.mmdb.gz"
	err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name: "test",
		URLs: []string{gzipURL},
		Path: destPath,
	}})
	if err != nil {
		t.Fatalf("DownloadFiles() error: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(data, compressed) {
		t.Fatalf("downloaded data = %q, want raw compressed bytes", data)
	}
}

func TestProgressGroup_NotTTY(t *testing.T) {
	pg := NewProgressGroup()
	if pg.p != nil {
		t.Fatal("non-TTY ProgressGroup should have nil internal Progress")
	}

	bar := pg.AddBar("test")
	if bar.bar != nil {
		t.Fatal("non-TTY ProgressBar should have nil internal Bar")
	}

	bar.SetTotal(100)
	bar.SetCurrent(50)
	bar.MarkDone(0)
	pg.Wait()
}

func TestDownloadFiles_CtxCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")

	d := &Downloader{httpClient: server.Client()}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := d.DownloadFiles(ctx, []FileSpec{{
		Name: "cancel-test",
		URLs: []string{server.URL},
		Path: destPath,
	}})
	if err == nil {
		t.Fatal("DownloadFiles() error = nil, want context error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadFiles() error = %v, want context.Canceled", err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "ipgeo-download-") {
			t.Fatalf("temp file not cleaned up: %s", e.Name())
		}
	}
}

// gzCompress is a helper that gzip-compresses the given data.
func gzCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func TestDownloadFiles_AutoDecompress_Gz(t *testing.T) {
	payload := []byte("hello world decompressed data")
	compressed := gzCompress(t, payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(compressed)))
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")
	gzipURL := server.URL + "/db.mmdb.gz"

	d := &Downloader{httpClient: server.Client()}
	err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name:           "test",
		URLs:           []string{gzipURL},
		Path:           destPath,
		AutoDecompress: true,
	}})
	if err != nil {
		t.Fatalf("DownloadFiles() error: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("downloaded data = %q, want decompressed payload %q", data, payload)
	}
}

func TestDownloadFiles_AutoDecompress_GzSizeLimit(t *testing.T) {
	// Create a gzip that decompresses to just over maxDecompressedSize bytes.
	// Use a highly compressible repeated pattern so the compressed payload stays small.
	bigPayload := bytes.Repeat([]byte("A"), int(maxDecompressedSize)+100)
	compressed := gzCompress(t, bigPayload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "big.mmdb")
	gzipURL := server.URL + "/big.mmdb.gz"

	d := &Downloader{httpClient: server.Client()}
	err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name:           "test",
		URLs:           []string{gzipURL},
		Path:           destPath,
		AutoDecompress: true,
	}})
	if err == nil {
		t.Fatal("DownloadFiles() error = nil, want size limit error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("DownloadFiles() error = %v, want size limit error", err)
	}
}

func TestDownloadFiles_AutoDecompress_TarGz(t *testing.T) {
	payload := []byte("tar targz payload")
	dir := t.TempDir()

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	if err := tw.WriteHeader(&tar.Header{
		Name: "subdir/db.mmdb",
		Size: int64(len(payload)),
		Mode: 0o644,
	}); err != nil {
		t.Fatalf("tar write header: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	compressed := gzCompress(t, tarBuf.Bytes())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	tests := []struct{ suffix string }{
		{".tar.gz"},
		{".tgz"},
	}
	for _, tt := range tests {
		destPath := filepath.Join(dir, tt.suffix, "db.mmdb")
		d := &Downloader{httpClient: server.Client()}
		if err := d.DownloadFiles(context.Background(), []FileSpec{{
			Name:           "test",
			URLs:           []string{server.URL + "/archive" + tt.suffix},
			Path:           destPath,
			AutoDecompress: true,
		}}); err != nil {
			t.Fatalf("%s: DownloadFiles() error: %v", tt.suffix, err)
		}
		data, err := os.ReadFile(destPath)
		if err != nil {
			t.Fatalf("%s: read file: %v", tt.suffix, err)
		}
		if !bytes.Equal(data, payload) {
			t.Fatalf("%s: data = %q, want %q", tt.suffix, data, payload)
		}
	}
}

func TestDownloadFiles_AutoDecompress_Zip(t *testing.T) {
	payload := []byte("zip content")

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, err := zw.Create("prefix/db.mmdb")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	compressed := zipBuf.Bytes()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")

	d := &Downloader{httpClient: server.Client()}
	if err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name:           "test",
		URLs:           []string{server.URL + "/archive.zip"},
		Path:           destPath,
		AutoDecompress: true,
	}}); err != nil {
		t.Fatalf("DownloadFiles() error: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("downloaded data = %q, want %q", data, payload)
	}
}

func TestDownloadFiles_AutoDecompress_NonCompressedURL(t *testing.T) {
	// AutoDecompress=true on a plain .mmdb URL should still work as direct download.
	payload := []byte("hello plain mmdb")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")

	d := &Downloader{httpClient: server.Client()}
	err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name:           "test",
		URLs:           []string{server.URL + "/db.mmdb"},
		Path:           destPath,
		AutoDecompress: true,
	}})
	if err != nil {
		t.Fatalf("DownloadFiles() error: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("downloaded data = %q, want %q", data, payload)
	}
}

func TestDownloadFiles_AutoDecompress_EntryNotFound(t *testing.T) {
	// tar.gz with different filename than expected.
	payload := []byte("wrong entry")
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	hdr := &tar.Header{
		Name: "other-file.bin",
		Size: int64(len(payload)),
		Mode: 0o644,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar write header: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	if _, err := gw.Write(tarBuf.Bytes()); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	compressed := gzBuf.Bytes()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(compressed)
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")
	tgzURL := server.URL + "/archive.tar.gz"

	d := &Downloader{httpClient: server.Client()}
	err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name:           "test",
		URLs:           []string{tgzURL},
		Path:           destPath,
		AutoDecompress: true,
	}})
	if err == nil {
		t.Fatal("DownloadFiles() error = nil, want 'not found' error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("DownloadFiles() error = %v, want 'not found' error", err)
	}
}

func TestIsCompressedURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
		ok   bool
	}{
		{url: "https://example.com/file.tar.gz", want: "targz", ok: true},
		{url: "https://example.com/file.TAR.GZ", want: "targz", ok: true},
		{url: "https://example.com/file.tgz", want: "targz", ok: true},
		{url: "https://example.com/file.TGZ", want: "targz", ok: true},
		{url: "https://example.com/file.gz", want: "gz", ok: true},
		{url: "https://example.com/file.GZ", want: "gz", ok: true},
		{url: "https://example.com/file.zip", want: "zip", ok: true},
		{url: "https://example.com/file.ZIP", want: "zip", ok: true},
		{url: "https://example.com/file.mmdb", want: "", ok: false},
		{url: "https://example.com/file.xdb", want: "", ok: false},
		{url: "https://example.com/file.tar.gz?query=1", want: "", ok: false},
	}
	for _, tt := range tests {
		got, ok := isCompressedURL(tt.url)
		if got != tt.want || ok != tt.ok {
			t.Errorf("isCompressedURL(%q) = (%q, %v), want (%q, %v)", tt.url, got, ok, tt.want, tt.ok)
		}
	}
}

func TestIsCompressedURL_TarGzBeforeGz(t *testing.T) {
	// ".tar.gz" must match as "targz", not "gz"
	got, ok := isCompressedURL("https://example.com/data.tar.gz")
	if !ok || got != "targz" {
		t.Fatalf("isCompressedURL(tar.gz) = (%q, %v), want (targz, true)", got, ok)
	}
}
