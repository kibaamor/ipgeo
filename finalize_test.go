package ipgeo

import (
	"errors"
	"net/netip"
	"testing"
)

func TestFinalize(t *testing.T) {
	addr := netip.MustParseAddr("1.2.3.4")
	tests := []struct {
		name    string
		r       Result
		wantErr error
	}{
		{name: "empty result returns ErrNotFound", r: Result{}, wantErr: ErrNotFound},
		{name: "populated result is returned", r: Result{CountryCode: "CN", Country: "China"}},
		{name: "ASN-only result is not empty", r: Result{ASN: 4134}},
		{name: "organization-only result is not empty", r: Result{Organization: "ChinaNet"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := finalize("src", addr, tt.r)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("finalize() error = %v, want %v", err, tt.wantErr)
				}
				if got != (Result{}) {
					t.Fatalf("finalize() result = %#v, want zero Result on not-found", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("finalize() error = %v, want nil", err)
			}
			if got.IP != addr {
				t.Errorf("IP = %v, want %v", got.IP, addr)
			}
			if got.Source != "src" {
				t.Errorf("Source = %q, want %q", got.Source, "src")
			}
		})
	}
}

func TestFinalize_NotFoundReturnsZeroResult(t *testing.T) {
	got, err := finalize("src", netip.MustParseAddr("1.2.3.4"), Result{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if got.IP.IsValid() || got.Source != "" {
		t.Fatalf("result = %#v, want zero Result (no IP/Source) on not-found", got)
	}
}

func TestParseASN(t *testing.T) {
	tests := []struct {
		in   string
		want uint32
	}{
		{"AS4134", 4134},
		{"4134", 4134},
		{"AS13335", 13335},
		{"AS4294967295", 4294967295},
		{"", 0},
		{"AS", 0},
		{"ASnotanumber", 0},
		{"as4134", 0},
		{"AS4294967296", 0},
	}
	for _, tt := range tests {
		if got := parseASN(tt.in); got != tt.want {
			t.Errorf("parseASN(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
