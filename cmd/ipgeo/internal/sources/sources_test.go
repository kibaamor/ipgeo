package sources

import (
	"path/filepath"
	"testing"
)

func TestOption_KnownTypes(t *testing.T) {
	for _, sourceType := range []string{"mmdb", "ipdb", "xdb", "ip2location"} {
		t.Run(sourceType, func(t *testing.T) {
			opt, err := Option(Entry{Type: sourceType, Name: "test", Filename: "test.db"}, func(filename string) string {
				return filename
			})
			if err != nil {
				t.Fatalf("Option() error: %v", err)
			}
			if opt == nil {
				t.Fatal("Option() returned nil option")
			}
		})
	}
}

func TestOption_UnknownType(t *testing.T) {
	_, err := Option(Entry{Type: "unknown", Name: "test", Filename: "test.db"}, func(filename string) string {
		return filename
	})
	if err == nil {
		t.Fatal("Option() error = nil")
	}
}

func TestSelect_FiltersByName(t *testing.T) {
	entries := []Entry{
		{Name: "First", Type: "mmdb", Filename: "first.mmdb"},
		{Name: "Second", Type: "xdb", Filename: "second.xdb"},
	}

	sources, err := Select(entries, "Second")
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if len(sources) != 1 || sources[0].Name != "Second" {
		t.Fatalf("Select() = %#v, want only Second", sources)
	}
}

func TestFiles_ExpandsPrimaryAndCompanionFiles(t *testing.T) {
	home := t.TempDir()
	entries := []Entry{
		{
			Name:              "GeoLite2",
			Filename:          "GeoLite2-City.mmdb",
			URLs:              []string{"https://example.com/city.mmdb"},
			CompanionFilename: "GeoLite2-ASN.mmdb",
			CompanionURLs:     []string{"https://example.com/asn.mmdb"},
		},
		{
			Name:     "DBIPCityLite",
			Filename: "dbip.mmdb",
			URLs:     []string{"https://example.com/dbip.mmdb"},
		},
	}

	files := Files(entries, func(filename string) string {
		return filepath.Join(home, filename)
	})
	if len(files) != 3 {
		t.Fatalf("files len = %d, want 3", len(files))
	}
	if files[0].Name != "GeoLite2" || files[0].Path != filepath.Join(home, "GeoLite2-City.mmdb") || files[0].URLs[0] != "https://example.com/city.mmdb" {
		t.Fatalf("primary file = %#v", files[0])
	}
	if files[1].Name != "GeoLite2 (companion)" || files[1].Path != filepath.Join(home, "GeoLite2-ASN.mmdb") || files[1].URLs[0] != "https://example.com/asn.mmdb" {
		t.Fatalf("companion file = %#v", files[1])
	}
	if files[2].Name != "DBIPCityLite" || files[2].Path != filepath.Join(home, "dbip.mmdb") || files[2].URLs[0] != "https://example.com/dbip.mmdb" {
		t.Fatalf("second source file = %#v", files[2])
	}
}
