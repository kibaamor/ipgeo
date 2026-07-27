package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

const (
	defaultTimeout      = 30 * time.Minute
	defaultRetryWaitMin = 0 * time.Second
	defaultRetryWaitMax = 3 * time.Second
	defaultRetryMax     = 1
)

func New(cfg *Config) *Downloader {
	if cfg == nil {
		cfg = &Config{
			Timeout:      defaultTimeout,
			RetryWaitMin: defaultRetryWaitMin,
			RetryWaitMax: defaultRetryWaitMax,
			RetryMax:     defaultRetryMax,
		}
	}

	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil
	retryClient.RetryWaitMin = cfg.RetryWaitMin
	retryClient.RetryWaitMax = cfg.RetryWaitMax
	retryClient.RetryMax = cfg.RetryMax

	client := retryClient.StandardClient()
	client.Timeout = cfg.Timeout

	return &Downloader{httpClient: client}
}

func (d *Downloader) Fetch(ctx context.Context, urls []string) ([]byte, error) {
	if len(urls) == 0 {
		return nil, errors.New("no URLs provided")
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

func (f FileSpec) validate() error {
	if f.Path == "" {
		return errors.New("path is required")
	}
	if len(f.URLs) == 0 {
		return errors.New("no URLs provided")
	}
	return nil
}

func (d *Downloader) DownloadFiles(ctx context.Context, files []FileSpec) error {
	if len(files) == 0 {
		return FileErrors{}
	}

	var validationErrs FileErrors
	for _, f := range files {
		if err := f.validate(); err != nil {
			validationErrs = append(validationErrs, FileError{Name: f.Name, Path: f.Path, Err: err})
		}
	}
	if len(validationErrs) > 0 {
		return validationErrs
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
	var lastErr error
	for _, url := range f.URLs {
		err := d.downloadFileFromURL(ctx, pg, f, url)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func (d *Downloader) downloadFileFromURL(ctx context.Context, pg *ProgressGroup, f FileSpec, url string) error {
	resp, err := d.doRequest(ctx, url)
	if err != nil {
		return err
	}

	bar := pg.AddBar(f.Name)
	tmpPath, written, err := d.downloadToTemp(ctx, resp, f.Path, bar)
	_ = resp.Body.Close()
	if err != nil {
		bar.Abort()
		return err
	}

	processor := newTempFileProcessor(url, f.AutoDecompress)
	err = processor.Process(tmpPath, f.Path)
	_ = os.Remove(tmpPath)

	if err != nil {
		bar.Abort()
		return err
	}

	bar.MarkDone(written)
	return nil
}

func (d *Downloader) downloadToTemp(ctx context.Context, resp *http.Response, destPath string, bar *ProgressBar) (string, int64, error) {
	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), "ipgeo-download-*")
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
