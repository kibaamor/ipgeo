package updater

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/downloader"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/sources"
)

func newDownloader(cfg *config.Config) *downloader.Downloader {
	d := downloader.New(&downloader.Config{
		Timeout:      cfg.HTTPTimeout(),
		RetryWaitMin: cfg.HTTPRetryWaitMin(),
		RetryWaitMax: cfg.HTTPRetryWaitMax(),
		RetryMax:     cfg.HTTPRetryMax(),
	})
	return d
}

func EnsureSources(ctx context.Context, cfg *config.Config, entries []config.SourceEntry) error {
	files := sources.Files(entries, cfg.SourcePath)

	var missing []sources.File
	for _, f := range files {
		_, err := os.Stat(f.Path)
		if err == nil {
			continue
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", f.Name, err)
		}
		missing = append(missing, f)
	}

	if len(missing) == 0 {
		return nil
	}

	fmt.Fprintf(os.Stderr, "Downloading missing source files...\n")
	return processFiles(ctx, cfg, missing)
}

func UpdateAll(ctx context.Context, cfg *config.Config) error {
	files := sources.Files(cfg.Sources, cfg.SourcePath)
	fmt.Fprintf(os.Stderr, "Updating all source files...\n")
	return processFiles(ctx, cfg, files)
}

func processFiles(ctx context.Context, cfg *config.Config, files []sources.File) error {
	now := time.Now().UTC()
	r := strings.NewReplacer("{YEAR}", now.Format("2006"), "{MONTH}", now.Format("01"))

	specs := make([]downloader.FileSpec, len(files))
	for i, f := range files {
		specs[i] = downloader.FileSpec{Name: f.Name, Path: f.Path, URLs: f.URLs, AutoDecompress: true}
		specs[i].URLs = make([]string, len(f.URLs))
		for j, u := range f.URLs {
			specs[i].URLs[j] = r.Replace(u)
		}
	}

	d := newDownloader(cfg)
	if err := d.DownloadFiles(ctx, specs); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	return nil
}
