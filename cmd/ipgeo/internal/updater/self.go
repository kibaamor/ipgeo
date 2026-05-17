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
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
)

// maxBinarySize limits downloaded and extracted self-update assets to 200 MiB.
const maxBinarySize = 200 << 20

// maxReleaseMetaSize limits release metadata and checksum files to 1 MiB.
const maxReleaseMetaSize = 1 << 20

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func SelfUpdate(cfg *config.Config, currentVersion string) error {
	client := newHTTPClient(cfg)

	release, err := fetchRelease(cfg.Updater.ReleaseURLs, client)
	if err != nil {
		return err
	}

	if release.TagName == currentVersion {
		fmt.Printf("Already at the latest version (%s).\n", currentVersion)
		return nil
	}

	assetURL, assetName, err := findMatchingAsset(release)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Downloading %s (%s)...\n", release.TagName, assetName)

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	archivePath, err := downloadToTemp(assetURL, filepath.Dir(execPath), client)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(archivePath) }()

	fmt.Fprintln(os.Stderr, "Verifying checksum...")
	if err := verifyAssetChecksum(release, assetName, archivePath, client); err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	deferred, err := installBinary(archivePath, assetName, execPath)
	if err != nil {
		return err
	}

	if deferred {
		fmt.Printf("Successfully downloaded %s. The update is scheduled to finish after Windows reboots.\n", release.TagName)
		return nil
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

func downloadToTemp(url, dir string, client *http.Client) (string, error) {
	f, err := os.CreateTemp(dir, "ipgeo-update-*")
	if err != nil {
		return "", err
	}
	path := f.Name()

	fmt.Fprintf(os.Stderr, "Fetching asset from %s...\n", url)
	if err := downloadRawTo(url, f, client, maxBinarySize); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("download asset: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close temp file: %w", err)
	}
	return path, nil
}

func installBinary(archivePath, assetName, execPath string) (bool, error) {
	binaryName := "ipgeo"
	if runtime.GOOS == "windows" {
		binaryName = "ipgeo.exe"
	}

	extractedPath, err := extractBinary(archivePath, assetName, binaryName, filepath.Dir(execPath))
	if err != nil {
		return false, fmt.Errorf("extract binary: %w", err)
	}
	var committed bool
	defer func() {
		if !committed {
			_ = os.Remove(extractedPath)
		}
	}()

	if err := os.Chmod(extractedPath, 0o755); err != nil {
		return false, fmt.Errorf("chmod: %w", err)
	}
	deferred, err := replaceBinary(extractedPath, execPath)
	if err != nil {
		return false, fmt.Errorf("replace binary: %w", err)
	}
	committed = true
	return deferred, nil
}

func fetchRelease(urls []string, client *http.Client) (*githubRelease, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("no release URLs configured")
	}
	var lastErr error
	for _, u := range urls {
		fmt.Fprintf(os.Stderr, "Fetching release info from %s...\n", u)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  failed: %v\n", err)
			lastErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  failed: %v\n", err)
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %s", resp.Status)
			fmt.Fprintf(os.Stderr, "  failed: %v\n", lastErr)
			continue
		}
		var release githubRelease
		data, err := readAllWithLimit(resp.Body, maxReleaseMetaSize)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read release info: %w", err)
			continue
		}
		err = json.Unmarshal(data, &release)
		if err != nil {
			lastErr = fmt.Errorf("parse release info: %w", err)
			continue
		}
		return &release, nil
	}
	return nil, fmt.Errorf("all release URLs failed: %w", lastErr)
}

func verifyAssetChecksum(release *githubRelease, assetName, archivePath string, client *http.Client) error {
	checksumURL, err := findChecksumURL(release)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, checksumURL, nil)
	if err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download checksums.txt: HTTP %s", resp.Status)
	}
	data, err := readAllWithLimit(resp.Body, maxReleaseMetaSize)
	if err != nil {
		return fmt.Errorf("read checksums.txt: %w", err)
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
	if err := copyWithLimit(out, src, maxBinarySize); err != nil {
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
