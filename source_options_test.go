package ipgeo

import (
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"
)

func TestOpenDatabaseOptionsFailWithInvalidPaths(t *testing.T) {
	missing := "testdata/missing-db"
	tests := []struct {
		name    string
		creator SourceCreator
		wantErr string
	}{
		{
			name:    "mmdb empty path",
			creator: MMDB("mmdb", "", ""),
			wantErr: "open mmdb mmdb: path must be non-empty",
		},
		{
			name:    "mmdb missing path",
			creator: MMDB("mmdb", missing, ""),
			wantErr: "open mmdb mmdb:",
		},
		{
			name:    "ipdb missing path",
			creator: IPDB("ipdb", missing),
			wantErr: "open ipdb ipdb:",
		},
		{
			name:    "xdb empty paths",
			creator: XDB("xdb", "", ""),
			wantErr: "open xdb xdb: at least one of v4Path or v6Path must be non-empty",
		},
		{
			name:    "xdb missing v4 path",
			creator: XDB("xdb", missing, ""),
			wantErr: "open xdb xdb:",
		},
		{
			name:    "ip2location missing path",
			creator: IP2Location("ip2location", missing),
			wantErr: "open ip2location ip2location:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Open(tt.creator)
			if err == nil {
				t.Fatal("Open() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Open() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestDatabaseSourceConstructorsFailWithInvalidFiles(t *testing.T) {
	tests := []struct {
		name string
		open func(path string) (Source, error)
	}{
		{
			name: "mmdb invalid file",
			open: func(path string) (Source, error) { return openMMDB("mmdb", path, "") },
		},
		{
			name: "ipdb invalid file",
			open: func(path string) (Source, error) { return openIPDB("ipdb", path) },
		},
		{
			name: "xdb invalid v4 file",
			open: func(path string) (Source, error) { return openXDB("xdb", path, "") },
		},
		{
			name: "ip2location invalid file",
			open: func(path string) (Source, error) { return openIP2Location("ip2location", path) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invalidDBPath, cleanup := writeInvalidDBFile(t)
			src, err := tt.open(invalidDBPath)
			cleanup()
			if err == nil {
				if src != nil {
					_ = src.Close()
				}
				t.Fatal("constructor error = nil, want error")
			}
			if src != nil {
				t.Fatalf("constructor source = %T, want nil on error", src)
			}
		})
	}
}

func writeInvalidDBFile(t *testing.T) (string, func()) {
	t.Helper()

	f, err := os.CreateTemp("", "ipgeo-invalid-db-*")
	if err != nil {
		t.Fatalf("create invalid db temp file: %v", err)
	}
	path := f.Name()
	if _, err := f.WriteString("not a database"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		t.Fatalf("write invalid db temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		t.Fatalf("close invalid db temp file: %v", err)
	}
	cleanup := func() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("remove invalid db temp file: %v", err)
		}
	}
	return path, cleanup
}

func TestSourceNamesWithoutDatabaseFiles(t *testing.T) {
	tests := []struct {
		name string
		src  Source
	}{
		{name: "mmdb", src: &mmdbSource{name: "mmdb"}},
		{name: "ipdb", src: &ipdbSource{name: "ipdb"}},
		{name: "xdb", src: &xdbSource{name: "xdb"}},
		{name: "ip2location", src: &ip2locationSource{name: "ip2location"}},
	}

	for _, tt := range tests {
		if got := tt.src.Name(); got != tt.name {
			t.Errorf("%T.Name() = %q, want %q", tt.src, got, tt.name)
		}
	}
}

func TestIPDBSourceCloseWithoutDatabaseFile(t *testing.T) {
	if err := (&ipdbSource{}).Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}

func TestAddrToNetIP(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "ipv4", addr: "1.2.3.4", want: "1.2.3.4"},
		{name: "ipv6", addr: "2001:db8::1", want: "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := netip.MustParseAddr(tt.addr)
			if got := addrToNetIP(addr).String(); got != tt.want {
				t.Fatalf("addrToNetIP(%v) = %q, want %q", addr, got, tt.want)
			}
		})
	}
}

func TestDatabaseSourceConstructorsWithInjectedOpeners(t *testing.T) {
	t.Run("mmdb success with companion", func(t *testing.T) {
		restoreMMDBOpener(t, func(_ string) (mmdbReader, error) {
			return &fakeMMDBReader{}, nil
		})
		src, err := openMMDB("mmdb", "city.mmdb", "asn.mmdb")
		if err != nil {
			t.Fatalf("openMMDB() error: %v", err)
		}
		mmdb, ok := src.(*mmdbSource)
		if !ok || mmdb.db == nil || mmdb.companion == nil {
			t.Fatalf("openMMDB() = %#v, want primary and companion readers", src)
		}
	})

	t.Run("mmdb companion error closes primary", func(t *testing.T) {
		sentinelErr := errors.New("companion failed")
		primary := &fakeMMDBReader{}
		calls := 0
		restoreMMDBOpener(t, func(_ string) (mmdbReader, error) {
			calls++
			if calls == 2 {
				return nil, sentinelErr
			}
			return primary, nil
		})
		_, err := openMMDB("mmdb", "city.mmdb", "asn.mmdb")
		if !errors.Is(err, sentinelErr) {
			t.Fatalf("openMMDB() error = %v, want sentinelErr", err)
		}
		if !primary.closed {
			t.Fatal("primary reader was not closed after companion open failed")
		}
	})

	t.Run("ipdb language preference", func(t *testing.T) {
		restoreIPDBOpener(t, func(_ string) (ipdbReader, error) {
			return &fakeIPDBReader{langs: []string{"CN", "EN"}}, nil
		})
		src, err := openIPDB("ipdb", "city.ipdb")
		if err != nil {
			t.Fatalf("openIPDB() error: %v", err)
		}
		if got := src.(*ipdbSource).lang; got != "EN" {
			t.Fatalf("lang = %q, want EN", got)
		}
	})

	t.Run("ipdb fallback language", func(t *testing.T) {
		restoreIPDBOpener(t, func(_ string) (ipdbReader, error) {
			return &fakeIPDBReader{langs: []string{"FR"}}, nil
		})
		src, err := openIPDB("ipdb", "city.ipdb")
		if err != nil {
			t.Fatalf("openIPDB() error: %v", err)
		}
		if got := src.(*ipdbSource).lang; got != "FR" {
			t.Fatalf("lang = %q, want FR", got)
		}
	})

	t.Run("ipdb no languages", func(t *testing.T) {
		restoreIPDBOpener(t, func(_ string) (ipdbReader, error) {
			return &fakeIPDBReader{}, nil
		})
		_, err := openIPDB("ipdb", "city.ipdb")
		if err == nil || !strings.Contains(err.Error(), "no languages") {
			t.Fatalf("openIPDB() error = %v, want no languages", err)
		}
	})

	t.Run("ip2location success", func(t *testing.T) {
		restoreIP2LocationOpener(t, func(_ string) (ip2locationReader, error) {
			return &fakeIP2LocationReader{}, nil
		})
		src, err := openIP2Location("ip2location", "db.bin")
		if err != nil {
			t.Fatalf("openIP2Location() error: %v", err)
		}
		if _, ok := src.(*ip2locationSource); !ok {
			t.Fatalf("openIP2Location() = %T, want *ip2locationSource", src)
		}
	})

	t.Run("xdb success", func(t *testing.T) {
		restoreXDBOpener(t, func(_, _ string) (xdbSearcher, error) {
			return &fakeXDBSearcher{}, nil
		})
		src, err := openXDB("xdb", "v4.xdb", "v6.xdb")
		if err != nil {
			t.Fatalf("openXDB() error: %v", err)
		}
		if _, ok := src.(*xdbSource); !ok {
			t.Fatalf("openXDB() = %T, want *xdbSource", src)
		}
	})
}

func TestDatabaseOptionsWithInjectedOpeners(t *testing.T) {
	restoreMMDBOpener(t, func(_ string) (mmdbReader, error) { return &fakeMMDBReader{}, nil })
	restoreIPDBOpener(t, func(_ string) (ipdbReader, error) {
		return &fakeIPDBReader{langs: []string{"EN"}}, nil
	})
	restoreIP2LocationOpener(t, func(_ string) (ip2locationReader, error) {
		return &fakeIP2LocationReader{}, nil
	})
	restoreXDBOpener(t, func(_, _ string) (xdbSearcher, error) {
		return &fakeXDBSearcher{}, nil
	})

	tests := []struct {
		name    string
		creator SourceCreator
	}{
		{name: "mmdb", creator: MMDB("mmdb", "city.mmdb", "asn.mmdb")},
		{name: "ipdb", creator: IPDB("ipdb", "city.ipdb")},
		{name: "xdb", creator: XDB("xdb", "v4.xdb", "v6.xdb")},
		{name: "ip2location", creator: IP2Location("ip2location", "db.bin")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := Open(tt.creator)
			if err != nil {
				t.Fatalf("Open() error: %v", err)
			}
			if err := client.Close(); err != nil {
				t.Fatalf("Close() error: %v", err)
			}
		})
	}
}

func restoreMMDBOpener(t *testing.T, opener func(string) (mmdbReader, error)) {
	t.Helper()
	orig := openMaxMindDB
	openMaxMindDB = opener
	t.Cleanup(func() { openMaxMindDB = orig })
}

func restoreIPDBOpener(t *testing.T, opener func(string) (ipdbReader, error)) {
	t.Helper()
	orig := newIPDBCity
	newIPDBCity = opener
	t.Cleanup(func() { newIPDBCity = orig })
}

func restoreIP2LocationOpener(t *testing.T, opener func(string) (ip2locationReader, error)) {
	t.Helper()
	orig := openIP2LocationDB
	openIP2LocationDB = opener
	t.Cleanup(func() { openIP2LocationDB = orig })
}

func restoreXDBOpener(t *testing.T, opener func(string, string) (xdbSearcher, error)) {
	t.Helper()
	orig := newIP2RegionWithPath
	newIP2RegionWithPath = opener
	t.Cleanup(func() { newIP2RegionWithPath = orig })
}
