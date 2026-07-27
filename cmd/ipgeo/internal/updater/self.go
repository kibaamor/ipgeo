//go:build !windows

package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/downloader"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

const maxBinarySize int64 = 100 << 20 // 100 MiB

func SelfUpdate(ctx context.Context, cfg *config.Config, currentVersion string) error {
	d := newDownloader(cfg)

	tagName, assetURL, assetName, checksumURL, err := resolveRelease(ctx, d, cfg.Updater.ReleaseURLs)
	if err != nil {
		return err
	}

	if strings.TrimPrefix(tagName, "v") == currentVersion {
		fmt.Printf("Already at the latest version (%s).\n", currentVersion)
		return nil
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	destDir := filepath.Dir(execPath)

	extractedPath, cleanup, err := downloadBinary(ctx, d, assetURL, assetName, checksumURL, destDir)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Chmod(extractedPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(extractedPath, execPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	fmt.Printf("Successfully updated to %s. Please restart ipgeo.\n", tagName)
	return nil
}

func resolveRelease(ctx context.Context, d *downloader.Downloader, urls []string) (tagName, assetURL, assetName, checksumURL string, err error) {
	data, err := d.Fetch(ctx, urls)
	if err != nil {
		return "", "", "", "", err
	}

	var release githubRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return "", "", "", "", err
	}

	assetKey := fmt.Sprintf("_%s_%s.", runtime.GOOS, runtime.GOARCH)
	for _, a := range release.Assets {
		switch a.Name {
		case "checksums.txt":
			checksumURL = a.BrowserDownloadURL
		default:
			if assetURL == "" && strings.Contains(strings.ToLower(a.Name), assetKey) {
				assetURL, assetName = a.BrowserDownloadURL, a.Name
			}
		}
	}

	if assetURL == "" {
		return "", "", "", "", fmt.Errorf("no asset found for %s in release %s", assetKey, release.TagName)
	}
	if checksumURL == "" {
		return "", "", "", "", fmt.Errorf("checksums.txt not found in release %s", release.TagName)
	}

	return release.TagName, assetURL, assetName, checksumURL, nil
}

func downloadBinary(ctx context.Context, d *downloader.Downloader, assetURL, assetName, checksumURL, destDir string) (string, func(), error) {
	archiveDir, err := os.MkdirTemp("", "ipgeo-upgrade-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(archiveDir) }()

	archivePath := filepath.Join(archiveDir, "archive")
	checksumPath := filepath.Join(archiveDir, "checksums.txt")
	if err := d.DownloadFiles(ctx, []downloader.FileSpec{
		{
			Name: "archive",
			URLs: []string{assetURL},
			Path: archivePath,
		},
		{
			Name: "checksums",
			URLs: []string{checksumURL},
			Path: checksumPath,
		},
	}); err != nil {
		return "", nil, err
	}

	fmt.Fprintln(os.Stderr, "Verifying checksum...")
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		return "", nil, fmt.Errorf("read checksums: %w", err)
	}
	var expectedHex string
	for line := range strings.SplitSeq(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == assetName {
			expectedHex = fields[0]
			break
		}
	}
	if expectedHex == "" {
		return "", nil, fmt.Errorf("no checksum found for %s in checksums.txt", assetName)
	}

	archive, err := os.Open(archivePath)
	if err != nil {
		return "", nil, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, archive); err != nil {
		_ = archive.Close()
		return "", nil, err
	}
	if err := archive.Close(); err != nil {
		return "", nil, fmt.Errorf("close archive: %w", err)
	}
	if actualHex := hex.EncodeToString(h.Sum(nil)); actualHex != expectedHex {
		return "", nil, fmt.Errorf("sha256 mismatch for %s: expected %s got %s", assetName, expectedHex, actualHex)
	}

	extractedPath, err := extractBinary(archivePath, assetName, "ipgeo", destDir)
	if err != nil {
		return "", nil, fmt.Errorf("extract binary: %w", err)
	}
	cleanup := func() { _ = os.Remove(extractedPath) }

	return extractedPath, cleanup, nil
}

func extractBinary(srcPath, assetName, binaryName, destDir string) (string, error) {
	lower := strings.ToLower(assetName)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		f, err := os.Open(srcPath)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()

		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		defer func() { _ = gz.Close() }()

		tr := tar.NewReader(gz)
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", err
			}
			if filepath.Base(hdr.Name) == binaryName {
				return writeNewBinary(destDir, tr)
			}
		}
		return "", fmt.Errorf("binary %s not found in archive", binaryName)
	}

	if strings.HasSuffix(lower, ".zip") {
		r, err := zip.OpenReader(srcPath)
		if err != nil {
			return "", err
		}
		defer func() { _ = r.Close() }()

		for _, f := range r.File {
			if filepath.Base(f.Name) == binaryName {
				rc, err := f.Open()
				if err != nil {
					return "", err
				}
				defer func() { _ = rc.Close() }()
				return writeNewBinary(destDir, rc)
			}
		}
		return "", fmt.Errorf("binary %s not found in zip", binaryName)
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return writeNewBinary(destDir, f)
}

func writeNewBinary(destDir string, src io.Reader) (string, error) {
	out, err := os.CreateTemp(destDir, "ipgeo-new-binary-*")
	if err != nil {
		return "", err
	}

	destPath := out.Name()
	keep := false
	defer func() {
		_ = out.Close()
		if !keep {
			_ = os.Remove(destPath)
		}
	}()

	written, err := io.CopyN(out, src, maxBinarySize+1)
	if err == nil || written > maxBinarySize {
		return "", fmt.Errorf("extracted binary exceeds %d bytes", maxBinarySize)
	}
	if !errors.Is(err, io.EOF) {
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close extracted binary: %w", err)
	}

	keep = true
	return destPath, nil
}
