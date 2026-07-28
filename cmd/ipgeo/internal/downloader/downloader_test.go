package downloader

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

func TestNewDownloader_UsesProjectDefaults(t *testing.T) {
	d := New(nil)

	if d.httpClient.Timeout != 30*time.Minute {
		t.Fatalf("client timeout = %v, want 30m", d.httpClient.Timeout)
	}
	transport, ok := d.httpClient.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("client transport = %T, want *retryablehttp.RoundTripper", d.httpClient.Transport)
	}
	if transport.Client == nil {
		t.Fatal("retryable transport client is nil")
	}
	if transport.Client.RetryWaitMin != 0*time.Second {
		t.Fatalf("retry wait min = %v, want 0s", transport.Client.RetryWaitMin)
	}
	if transport.Client.RetryWaitMax != 3*time.Second {
		t.Fatalf("retry wait max = %v, want 3s", transport.Client.RetryWaitMax)
	}
	if transport.Client.RetryMax != 1 {
		t.Fatalf("retry max = %d, want 1", transport.Client.RetryMax)
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

func TestNewDownloader_TimeoutZeroHonored(t *testing.T) {
	cfg := &Config{
		Timeout: 0,
	}
	d := New(cfg)

	if d.httpClient.Timeout != 0 {
		t.Fatalf("client timeout = %v, want 0s", d.httpClient.Timeout)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer badServer.Close()

	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestDownloadFiles_EmptyURLsInvalid(t *testing.T) {
	d := &Downloader{httpClient: &http.Client{}}
	destPath := filepath.Join(t.TempDir(), "db.mmdb")

	err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name: "missing-urls",
		Path: destPath,
	}})
	if err == nil {
		t.Fatal("DownloadFiles() error = nil, want validation error")
	}
	var fileErrs FileErrors
	if !errors.As(err, &fileErrs) {
		t.Fatalf("DownloadFiles() error type = %T, want FileErrors", err)
	}
	if len(fileErrs) != 1 {
		t.Fatalf("len(FileErrors) = %d, want 1", len(fileErrs))
	}
	if !strings.Contains(err.Error(), "no URLs provided") {
		t.Fatalf("DownloadFiles() error = %v, want no URLs provided", err)
	}
}

func TestDownloadFiles_EmptyPathInvalid(t *testing.T) {
	d := &Downloader{httpClient: &http.Client{}}

	err := d.DownloadFiles(context.Background(), []FileSpec{{
		Name: "missing-path",
		URLs: []string{"https://example.com/db.mmdb"},
	}})
	if err == nil {
		t.Fatal("DownloadFiles() error = nil, want validation error")
	}
	var fileErrs FileErrors
	if !errors.As(err, &fileErrs) {
		t.Fatalf("DownloadFiles() error type = %T, want FileErrors", err)
	}
	if len(fileErrs) != 1 {
		t.Fatalf("len(FileErrors) = %d, want 1", len(fileErrs))
	}
	if !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("DownloadFiles() error = %v, want path is required", err)
	}
}

func TestDownloadFiles_InvalidSpecsAggregated(t *testing.T) {
	d := &Downloader{httpClient: &http.Client{}}

	err := d.DownloadFiles(context.Background(), []FileSpec{
		{Name: "missing-path", URLs: []string{"https://example.com/db.mmdb"}},
		{Name: "missing-urls", Path: filepath.Join(t.TempDir(), "db.mmdb")},
	})
	if err == nil {
		t.Fatal("DownloadFiles() error = nil, want validation errors")
	}
	var fileErrs FileErrors
	if !errors.As(err, &fileErrs) {
		t.Fatalf("DownloadFiles() error type = %T, want FileErrors", err)
	}
	if len(fileErrs) != 2 {
		t.Fatalf("len(FileErrors) = %d, want 2", len(fileErrs))
	}
	if !strings.Contains(err.Error(), "path is required") || !strings.Contains(err.Error(), "no URLs provided") {
		t.Fatalf("DownloadFiles() error = %v, want both validation errors", err)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestDownloadFiles_CtxCancellationTTYProgressReturns(t *testing.T) {
	oldTTY := isTTY
	isTTY = true
	t.Cleanup(func() { isTTY = oldTTY })

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "db.mmdb")
	d := &Downloader{httpClient: server.Client()}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- d.DownloadFiles(ctx, []FileSpec{{
			Name: "cancel-test",
			URLs: []string{server.URL},
			Path: destPath,
		}})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("DownloadFiles() error = nil, want context error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DownloadFiles() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DownloadFiles() did not return after context cancellation")
	}
}

func TestDownloadFiles_CtxCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(compressed)))
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
	bigPayload := bytes.Repeat([]byte("A"), int(maxDecompressedSize)+100)
	compressed := gzCompress(t, bigPayload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	payload := []byte("hello plain mmdb")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
