package downloader

import (
	"context"
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
		resp, err := d.doRequest(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := d.readResponse(resp)
		if err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}
	return nil, fmt.Errorf("all URLs failed: %w", lastErr)
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

func (d *Downloader) readResponse(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return data, nil
}

type FileSpec struct {
	Name string
	URLs []string
	Path string
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
	b.WriteString(fmt.Sprintf("%d file(s) failed:", len(e)))
	for _, fe := range e {
		b.WriteString(fmt.Sprintf("\n  %s: %v", fe.Name, fe.Err))
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
	for _, url := range f.URLs {
		resp, err := d.doRequest(ctx, url)
		if err != nil {
			lastErr = err
			continue
		}
		lastErr = d.downloadOne(ctx, resp, f.Path, bar)
		if lastErr == nil {
			return nil
		}
		bar.Abort()
	}
	return lastErr
}

func (d *Downloader) downloadOne(ctx context.Context, resp *http.Response, destPath string, bar *ProgressBar) error {
	defer func() { _ = resp.Body.Close() }()

	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), "ipgeo-download-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	var committed bool
	defer func() {
		_ = tmpFile.Close()
		if !committed {
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
			return ctx.Err()
		default:
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			wn, writeErr := tmpFile.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}
			written += int64(wn)
			count++
			if count%64 == 0 {
				bar.SetCurrent(written)
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return readErr
			}
			break
		}
	}
	bar.MarkDone(written)

	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}
	committed = true
	return nil
}
