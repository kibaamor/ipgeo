package updater

import (
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
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/sources"
)

// maxDBFileSize limits the stored, decompressed database payload to 2 GiB.
const maxDBFileSize = 2 << 30

func copyWithLimit(w io.Writer, r io.Reader, limit int64) error {
	n, err := io.Copy(w, io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("content exceeds maximum size of %d bytes", limit)
	}
	return nil
}

func readAllWithLimit(r io.Reader, limit int64) ([]byte, error) {
	var buf strings.Builder
	if err := copyWithLimit(&buf, r, limit); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func newHTTPClient(cfg *config.Config) *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil
	retryClient.RetryWaitMin = cfg.HTTPRetryWaitMin()
	retryClient.RetryWaitMax = cfg.HTTPRetryWaitMax()
	retryClient.RetryMax = cfg.HTTPRetryMax()

	client := retryClient.StandardClient()
	client.Timeout = cfg.HTTPTimeout()
	return client
}

func expandURLTemplate(url string) string {
	now := time.Now().UTC()
	r := strings.NewReplacer(
		"{YEAR}", now.Format("2006"),
		"{MONTH}", now.Format("01"),
	)
	return r.Replace(url)
}

func downloadDB(name string, urls []string, destPath string, client *http.Client) error {
	if len(urls) == 0 {
		return fmt.Errorf("no download URLs configured for %s", name)
	}

	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, "ipgeo-download-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	var committed, fileClosed bool
	defer func() {
		if !fileClosed {
			_ = tmpFile.Close()
		}
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	var lastErr error
	for _, rawURL := range urls {
		url := expandURLTemplate(rawURL)
		fmt.Fprintf(os.Stderr, "Downloading %s from %s...\n", name, url)

		if err := downloadTo(url, tmpFile, client); err != nil {
			fmt.Fprintf(os.Stderr, "  failed: %v\n", err)
			lastErr = err
			if resetErr := resetTempFile(tmpFile); resetErr != nil {
				lastErr = resetErr
				break
			}
			continue
		}

		fileClosed = true
		if err := tmpFile.Close(); err != nil {
			return err
		}
		if err := replaceFile(tmpPath, destPath); err != nil {
			return err
		}
		committed = true
		return nil
	}
	return fmt.Errorf("all download URLs failed for %s: %w", name, lastErr)
}

// downloadTo writes the response body to w, decompressing URLs that end in .gz.
func downloadTo(url string, w io.Writer, client *http.Client) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	path, _, _ := strings.Cut(url, "?")
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("gzip open: %w", err)
		}
		defer func() { _ = gz.Close() }()
		return copyWithLimit(w, gz, maxDBFileSize)
	}

	return copyWithLimit(w, resp.Body, maxDBFileSize)
}

// downloadRawTo writes response bytes unchanged, up to limit.
func downloadRawTo(url string, w io.Writer, client *http.Client, limit int64) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return copyWithLimit(w, resp.Body, limit)
}

func resetTempFile(f *os.File) error {
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	return f.Truncate(0)
}

func runConcurrent(files []sources.File, fn func(sources.File) error) error {
	errs := make([]error, len(files))
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		go func(i int, f sources.File) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errs[i] = fmt.Errorf("panic downloading %s: %v", f.Name, r)
				}
			}()
			errs[i] = fn(f)
		}(i, f)
	}
	wg.Wait()
	return errors.Join(errs...)
}

func EnsureSources(cfg *config.Config, entries []config.SourceEntry) error {
	client := newHTTPClient(cfg)
	return ensureDBFiles(sources.Files(entries, cfg.SourcePath), client)
}

func ensureDBFiles(files []sources.File, client *http.Client) error {
	return runConcurrent(files, func(f sources.File) error {
		_, statErr := os.Stat(f.Path)
		if statErr == nil {
			return nil
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", f.Name, statErr)
		}
		if err := downloadDB(f.Name, f.URLs, f.Path, client); err != nil {
			return fmt.Errorf("ensure %s: %w", f.Name, err)
		}
		return nil
	})
}

func UpdateAll(cfg *config.Config) error {
	client := newHTTPClient(cfg)
	return runConcurrent(sources.Files(cfg.Sources, cfg.SourcePath), func(f sources.File) error {
		if err := downloadDB(f.Name, f.URLs, f.Path, client); err != nil {
			return fmt.Errorf("update %s: %w", f.Name, err)
		}
		return nil
	})
}
