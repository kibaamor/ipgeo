package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
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

func SelfUpdate(ctx context.Context, cfg *config.Config, currentVersion string) error {
	d := newDownloader(cfg)

	release, err := fetchRelease(ctx, d, cfg.Updater.ReleaseURLs)
	if err != nil {
		return err
	}

	if strings.TrimPrefix(release.TagName, "v") == currentVersion {
		fmt.Printf("Already at the latest version (%s).\n", currentVersion)
		return nil
	}

	assetURL, assetName, err := findMatchingAsset(release)
	if err != nil {
		return err
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	archiveDir, err := os.MkdirTemp("", "ipgeo-update")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(archiveDir) }()
	archivePath := filepath.Join(archiveDir, "archive")

	if err := d.DownloadFiles(ctx, []downloader.FileSpec{{
		Name: "upgrading ipgeo",
		URLs: []string{assetURL},
		Path: archivePath,
	}}); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "Verifying checksum...")
	if err := verifyAssetChecksum(ctx, d, release, assetName, archivePath); err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	if err := installBinary(archivePath, assetName, execPath); err != nil {
		return err
	}
	fmt.Printf("Successfully updated to %s. Please restart ipgeo.\n", release.TagName)
	return nil
}

func findMatchingAsset(release *githubRelease) (url, name string, err error) {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	for _, a := range release.Assets {
		if assetMatchesRuntime(a.Name, goos, goarch) {
			return a.BrowserDownloadURL, a.Name, nil
		}
	}
	return "", "", fmt.Errorf("no asset found for %s/%s in release %s", goos, goarch, release.TagName)
}

func assetMatchesRuntime(name, goos, goarch string) bool {
	base := strings.ToLower(name)
	for _, ext := range []string{".tar.gz", ".zip", ".gz"} {
		base = strings.TrimSuffix(base, ext)
	}
	tokens := strings.Split(base, "_")
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i] == goos && tokens[i+1] == goarch {
			return true
		}
	}
	return false
}

func installBinary(archivePath, assetName, execPath string) error {
	extractedPath, err := extractBinary(archivePath, assetName, "ipgeo", filepath.Dir(execPath))
	if err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}
	defer func() { _ = os.Remove(extractedPath) }()

	if err := os.Chmod(extractedPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(extractedPath, execPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

func fetchRelease(ctx context.Context, d *downloader.Downloader, urls []string) (*githubRelease, error) {
	data, err := d.Fetch(ctx, urls)
	if err != nil {
		return nil, err
	}
	var release githubRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return nil, err
	}
	return &release, nil
}

func verifyAssetChecksum(ctx context.Context, d *downloader.Downloader, release *githubRelease, assetName, archivePath string) error {
	checksumURL, err := findChecksumURL(release)
	if err != nil {
		return err
	}
	data, err := d.Fetch(ctx, []string{checksumURL})
	if err != nil {
		return err
	}
	expectedHex, err := parseChecksumEntry(data, assetName)
	if err != nil {
		return err
	}
	return verifySHA256(archivePath, assetName, expectedHex)
}

func findChecksumURL(release *githubRelease) (string, error) {
	for _, a := range release.Assets {
		if a.Name == "checksums.txt" {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("checksums.txt not found in release %s", release.TagName)
}

func parseChecksumEntry(data []byte, assetName string) (string, error) {
	for line := range strings.SplitSeq(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum found for %s in checksums.txt", assetName)
}

func verifySHA256(path, assetName, expectedHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("compute sha256: %w", err)
	}
	if actual := fmt.Sprintf("%x", h.Sum(nil)); actual != expectedHex {
		return fmt.Errorf("sha256 mismatch for %s: expected %s got %s", assetName, expectedHex, actual)
	}
	return nil
}

func extractBinary(srcPath, assetName, binaryName, destDir string) (string, error) {
	lower := strings.ToLower(assetName)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractFromTarGz(srcPath, binaryName, destDir)
	case strings.HasSuffix(lower, ".zip"):
		return extractFromZip(srcPath, binaryName, destDir)
	default:
		return srcPath, nil
	}
}

func writeExtractedBinary(src io.Reader, destDir string) (string, error) {
	out, err := os.CreateTemp(destDir, "ipgeo-new-binary-*")
	if err != nil {
		return "", err
	}
	destPath := out.Name()
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		_ = os.Remove(destPath)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(destPath)
		return "", fmt.Errorf("close extracted binary: %w", err)
	}
	return destPath, nil
}

func extractFromTarGz(srcPath, binaryName, destDir string) (string, error) {
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
			return writeExtractedBinary(tr, destDir)
		}
	}
	return "", fmt.Errorf("binary %s not found in archive", binaryName)
}

func extractFromZip(srcPath, binaryName, destDir string) (string, error) {
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
			return writeExtractedBinary(rc, destDir)
		}
	}
	return "", fmt.Errorf("binary %s not found in zip", binaryName)
}