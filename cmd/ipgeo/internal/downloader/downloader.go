package downloader

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

type Config struct {
	Timeout      time.Duration
	RetryWaitMin time.Duration
	RetryWaitMax time.Duration
	RetryMax     int
}

type Downloader struct {
	httpClient *http.Client
}

func New(cfg *Config) *Downloader {
	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil

	if cfg != nil {
		retryClient.RetryWaitMin = cfg.RetryWaitMin
		retryClient.RetryWaitMax = cfg.RetryWaitMax
		retryClient.RetryMax = cfg.RetryMax
	}

	client := retryClient.StandardClient()
	if cfg != nil && cfg.Timeout > 0 {
		client.Timeout = cfg.Timeout
	} else {
		client.Timeout = 30 * time.Minute
	}

	return &Downloader{httpClient: client}
}

func (d *Downloader) Fetch(ctx context.Context, urls []string) ([]byte, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("no URLs provided")
	}

	var lastErr error
	for _, u := range urls {
		data, err := d.fetchOne(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}

	return nil, fmt.Errorf("all URLs failed: %w", lastErr)
}

func (d *Downloader) fetchOne(ctx context.Context, url string) ([]byte, error) {
	resp, err := d.doRequest(ctx, url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return data, nil
}

func (d *Downloader) doRequest(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}

	return resp, nil
}

type FileSpec struct {
	Name           string
	URLs           []string
	Path           string
	AutoDecompress bool
}

type FileError struct {
	Name string
	Path string
	Err  error
}

func (e FileError) Error() string {
	return fmt.Sprintf("%s: %v", e.Name, e.Err)
}

type FileErrors []FileError

func (e FileErrors) Error() string {
	if len(e) == 0 {
		return "no file errors"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d file(s) failed:", len(e))
	for _, fe := range e {
		fmt.Fprintf(&b, "\n  %s: %v", fe.Name, fe.Err)
	}
	return b.String()
}

func (e FileErrors) Unwrap() []error {
	errs := make([]error, len(e))
	for i, fe := range e {
		errs[i] = fe.Err
	}
	return errs
}

func (d *Downloader) DownloadFiles(ctx context.Context, files []FileSpec) error {
	if len(files) == 0 {
		return FileErrors{}
	}

	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			return err
		}
	}

	pg := NewProgressGroup()
	defer pg.Wait()

	var wg sync.WaitGroup
	errs := make([]error, len(files))
	for i := range files {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f := files[i]
			errs[i] = d.downloadFile(ctx, pg, f)
		}(i)
	}
	wg.Wait()

	var fe FileErrors
	for i, err := range errs {
		if err != nil {
			fe = append(fe, FileError{Name: files[i].Name, Path: files[i].Path, Err: err})
		}
	}
	if len(fe) > 0 {
		return fe
	}
	return nil
}

func (d *Downloader) downloadFile(ctx context.Context, pg *ProgressGroup, f FileSpec) error {
	bar := pg.AddBar(f.Name)
	var lastErr error
	//nolint:bodyclose // callees close resp.Body.
	for _, url := range f.URLs {
		resp, err := d.doRequest(ctx, url)
		if err != nil {
			lastErr = err
			continue
		}

		if dtype, ok := isCompressedURL(url); f.AutoDecompress && ok {
			lastErr = d.downloadAndDecompress(ctx, resp, f.Path, dtype, bar)
		} else {
			lastErr = d.downloadOne(ctx, resp, f.Path, bar)
		}

		if lastErr == nil {
			return nil
		}

		bar.Abort()
	}
	return lastErr
}

func isCompressedURL(url string) (string, bool) {
	lower := strings.ToLower(url)
	if strings.HasSuffix(lower, ".tar.gz") {
		return "targz", true
	}
	if strings.HasSuffix(lower, ".tgz") {
		return "targz", true
	}
	if strings.HasSuffix(lower, ".gz") {
		return "gz", true
	}
	if strings.HasSuffix(lower, ".zip") {
		return "zip", true
	}
	return "", false
}

func (d *Downloader) downloadOne(ctx context.Context, resp *http.Response, destPath string, bar *ProgressBar) error {
	defer func() { _ = resp.Body.Close() }()

	tmpPath, written, err := d.downloadToTemp(ctx, resp, destPath, bar)
	if err != nil {
		return err
	}

	err = os.Rename(tmpPath, destPath)
	_ = os.Remove(tmpPath)

	if err != nil {
		return err
	}

	bar.MarkDone(written)
	return nil
}

func (d *Downloader) downloadToTemp(ctx context.Context, resp *http.Response, destDir string, bar *ProgressBar) (string, int64, error) {
	tmpFile, err := os.CreateTemp(filepath.Dir(destDir), "ipgeo-download-*")
	if err != nil {
		return "", 0, err
	}
	tmpPath := tmpFile.Name()

	var keep bool
	defer func() {
		_ = tmpFile.Close()
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()

	if contentLen := resp.ContentLength; contentLen > 0 {
		bar.SetTotal(contentLen)
	}

	var written, count int64
	buf := make([]byte, 256*1024)
	for {
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			wn, writeErr := tmpFile.Write(buf[:n])
			if writeErr != nil {
				return "", 0, writeErr
			}

			written += int64(wn)
			count++
			if count%64 == 0 {
				bar.SetCurrent(written)
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return "", 0, readErr
			}
			break
		}
	}

	keep = true
	return tmpPath, written, nil
}

func (d *Downloader) downloadAndDecompress(ctx context.Context, resp *http.Response, destPath string, dtype string, bar *ProgressBar) error {
	defer func() { _ = resp.Body.Close() }()

	tmpPath, written, err := d.downloadToTemp(ctx, resp, destPath, bar)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()

	f, err := os.Open(tmpPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	switch dtype {
	case "gz":
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()
		if err := writeFile(gz, destPath); err != nil {
			return err
		}
	case "targz":
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()
		if err := extractTarGz(gz, destPath); err != nil {
			return err
		}
	case "zip":
		if err := extractZip(tmpPath, destPath); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown decompression type: %s", dtype)
	}
	bar.MarkDone(written)
	return nil
}

const maxDecompressedSize int64 = 5 << 30 // 5 GiB

func writeFile(src io.Reader, destPath string) error {
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.CopyN(f, src, maxDecompressedSize+1)
	if err == nil {
		return fmt.Errorf("decompressed file exceeds %d bytes", maxDecompressedSize)
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func extractTarGz(src io.Reader, destPath string) error {
	tr := tar.NewReader(src)
	want := filepath.Base(destPath)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) == want {
			return writeFile(tr, destPath)
		}
	}
	return fmt.Errorf("%s not found in targz", want)
}

func extractZip(zipPath, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	want := filepath.Base(destPath)
	for _, f := range r.File {
		if filepath.Base(f.Name) == want {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer func() { _ = rc.Close() }()
			return writeFile(rc, destPath)
		}
	}
	return fmt.Errorf("%s not found in zip", want)
}
