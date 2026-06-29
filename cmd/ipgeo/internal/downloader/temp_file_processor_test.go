package downloader

import (
	"fmt"
	"testing"
)

func TestNewTempFileProcessor(t *testing.T) {
	tests := []struct {
		url            string
		autoDecompress bool
		want           string
	}{
		{url: "https://example.com/file.tar.gz", autoDecompress: true, want: "targz"},
		{url: "https://example.com/file.TAR.GZ", autoDecompress: true, want: "targz"},
		{url: "https://example.com/file.tgz", autoDecompress: true, want: "targz"},
		{url: "https://example.com/file.TGZ", autoDecompress: true, want: "targz"},
		{url: "https://example.com/file.gz", autoDecompress: true, want: "gz"},
		{url: "https://example.com/file.GZ", autoDecompress: true, want: "gz"},
		{url: "https://example.com/file.zip", autoDecompress: true, want: "zip"},
		{url: "https://example.com/file.ZIP", autoDecompress: true, want: "zip"},
		{url: "https://example.com/file.mmdb", autoDecompress: true, want: "raw"},
		{url: "https://example.com/file.xdb", autoDecompress: true, want: "raw"},
		{url: "https://example.com/file.tar.gz?query=1", autoDecompress: true, want: "raw"},
		{url: "https://example.com/file.gz", autoDecompress: false, want: "raw"},
	}
	for _, tt := range tests {
		got := processorName(newTempFileProcessor(tt.url, tt.autoDecompress))
		if got != tt.want {
			t.Errorf("newTempFileProcessor(%q, %v) = %q, want %q", tt.url, tt.autoDecompress, got, tt.want)
		}
	}
}

func TestNewTempFileProcessor_TarGzBeforeGz(t *testing.T) {
	got := processorName(newTempFileProcessor("https://example.com/data.tar.gz", true))
	if got != "targz" {
		t.Fatalf("newTempFileProcessor(tar.gz) = %q, want targz", got)
	}
}

func processorName(processor tempFileProcessor) string {
	switch processor.(type) {
	case rawProcessor:
		return "raw"
	case gzipProcessor:
		return "gz"
	case tarGzipProcessor:
		return "targz"
	case zipProcessor:
		return "zip"
	default:
		return fmt.Sprintf("%T", processor)
	}
}
