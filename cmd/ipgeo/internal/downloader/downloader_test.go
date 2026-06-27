package downloader

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
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