package updater

import "testing"

func TestAssetMatchesRuntime_ExactOSTokens(t *testing.T) {
	tests := []struct {
		name   string
		asset  string
		goos   string
		goarch string
		want   bool
	}{
		{
			name:   "matching archive",
			asset:  "ipgeo_linux_amd64.tar.gz",
			goos:   "linux",
			goarch: "amd64",
			want:   true,
		},
		{
			name:   "arm does not match arm64",
			asset:  "ipgeo_linux_arm64.tar.gz",
			goos:   "linux",
			goarch: "arm",
			want:   false,
		},
		{
			name:   "wrong OS",
			asset:  "ipgeo_darwin_amd64.zip",
			goos:   "linux",
			goarch: "amd64",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := assetMatchesRuntime(tt.asset, tt.goos, tt.goarch); got != tt.want {
				t.Fatalf("assetMatchesRuntime(%q, %q, %q) = %v, want %v", tt.asset, tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}
