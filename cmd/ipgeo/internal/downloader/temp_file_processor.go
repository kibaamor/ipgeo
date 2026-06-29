package downloader

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type tempFileProcessor interface {
	Process(tmpPath, destPath string) error
}

type rawProcessor struct{}

func (rawProcessor) Process(tmpPath, destPath string) error {
	return replaceFile(tmpPath, destPath)
}

type gzipProcessor struct{}

func (gzipProcessor) Process(tmpPath, destPath string) error {
	f, err := os.Open(tmpPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	return writeDecompressedFile(gz, destPath)
}

type tarGzipProcessor struct{}

func (tarGzipProcessor) Process(tmpPath, destPath string) error {
	f, err := os.Open(tmpPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
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
			return writeDecompressedFile(tr, destPath)
		}
	}
	return fmt.Errorf("%s not found in targz", want)
}

type zipProcessor struct{}

func (zipProcessor) Process(tmpPath, destPath string) error {
	r, err := zip.OpenReader(tmpPath)
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
			return writeDecompressedFile(rc, destPath)
		}
	}
	return fmt.Errorf("%s not found in zip", want)
}

func newTempFileProcessor(url string, autoDecompress bool) tempFileProcessor {
	if !autoDecompress {
		return rawProcessor{}
	}

	lower := strings.ToLower(url)
	if strings.HasSuffix(lower, ".tar.gz") {
		return tarGzipProcessor{}
	}
	if strings.HasSuffix(lower, ".tgz") {
		return tarGzipProcessor{}
	}
	if strings.HasSuffix(lower, ".gz") {
		return gzipProcessor{}
	}
	if strings.HasSuffix(lower, ".zip") {
		return zipProcessor{}
	}
	return rawProcessor{}
}

const maxDecompressedSize int64 = 5 << 30 // 5 GiB

func writeDecompressedFile(src io.Reader, destPath string) error {
	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), "ipgeo-decompress-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	keep := false
	defer func() {
		_ = tmpFile.Close()
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()

	written, err := io.CopyN(tmpFile, src, maxDecompressedSize+1)
	if err == nil || written > maxDecompressedSize {
		return fmt.Errorf("decompressed file exceeds %d bytes", maxDecompressedSize)
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	if err := tmpFile.Chmod(0o644); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close decompressed file: %w", err)
	}
	if err := replaceFile(tmpPath, destPath); err != nil {
		return err
	}
	keep = true
	return nil
}
