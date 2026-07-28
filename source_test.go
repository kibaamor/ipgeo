package ipgeo

import "testing"

func TestCleanIP2LocationField(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"-", ""},
		{"This parameter requires...", ""},
		{"This is a sample...", ""},
		{"China", "China"},
		{"Beijing", "Beijing"},
		{"ChinaNet", "ChinaNet"},
	}
	for _, tt := range tests {
		if got := cleanIP2LocationField(tt.in); got != tt.want {
			t.Errorf("cleanIP2LocationField(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCleanXDBField(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"0", ""},
		{"China", "China"},
		{"0.0", "0.0"},
	}
	for _, tt := range tests {
		if got := cleanXDBField(tt.in); got != tt.want {
			t.Errorf("cleanXDBField(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPickName(t *testing.T) {
	tests := []struct {
		name  string
		names map[string]string
		want  string
	}{
		{"nil map", nil, ""},
		{"empty map", map[string]string{}, ""},
		{"en present", map[string]string{"en": "China", "zh": "中国"}, "China"},
		{"no en, returns any", map[string]string{"zh": "中国"}, "中国"},
		{"only en", map[string]string{"en": "United States"}, "United States"},
		{"multiple langs, en preferred", map[string]string{"fr": "États-Unis", "en": "United States", "es": "Estados Unidos"}, "United States"},
		{"single non-en entry", map[string]string{"es": "España"}, "España"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pickName(tt.names)
			if tt.want == "" {
				if got != "" {
					t.Errorf("pickName() = %q, want empty", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("pickName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeRecord(t *testing.T) {
	t.Run("fills empty fields", func(t *testing.T) {
		r := &Result{}
		rec := &cityRecord{
			AutonomousSystemNumber:       4134,
			AutonomousSystemOrganization: "ChinaNet",
		}
		rec.Country.ISOCode = "CN"
		rec.Country.Names = map[string]string{"en": "China"}
		rec.Subdivisions = []struct {
			Names map[string]string `maxminddb:"names"`
		}{{Names: map[string]string{"en": "Beijing"}}}
		rec.City.Names = map[string]string{"en": "Haidian"}

		mergeRecord(r, rec)

		if r.CountryCode != "CN" {
			t.Errorf("countryCode = %q, want CN", r.CountryCode)
		}
		if r.Country != "China" {
			t.Errorf("country = %q, want China", r.Country)
		}
		if r.Province != "Beijing" {
			t.Errorf("province = %q, want Beijing", r.Province)
		}
		if r.City != "Haidian" {
			t.Errorf("city = %q, want Haidian", r.City)
		}
		if r.ASN != 4134 {
			t.Errorf("asn = %d, want 4134", r.ASN)
		}
		if r.Organization != "ChinaNet" {
			t.Errorf("organization = %q, want ChinaNet", r.Organization)
		}
	})

	t.Run("does not overwrite existing fields", func(t *testing.T) {
		r := &Result{
			CountryCode:  "US",
			Country:      "United States",
			Province:     "CA",
			City:         "LA",
			ASN:          7922,
			Organization: "Comcast",
		}
		rec := &cityRecord{
			AutonomousSystemNumber:       4134,
			AutonomousSystemOrganization: "ChinaNet",
		}
		rec.Country.ISOCode = "CN"
		rec.Country.Names = map[string]string{"en": "China"}
		rec.Subdivisions = []struct {
			Names map[string]string `maxminddb:"names"`
		}{{Names: map[string]string{"en": "Guangdong"}}}
		rec.City.Names = map[string]string{"en": "Beijing"}

		mergeRecord(r, rec)

		if r.CountryCode != "US" {
			t.Errorf("countryCode should not be overwritten, got %q", r.CountryCode)
		}
		if r.Country != "United States" {
			t.Errorf("country should not be overwritten, got %q", r.Country)
		}
		if r.Province != "CA" {
			t.Errorf("province should not be overwritten, got %q", r.Province)
		}
		if r.City != "LA" {
			t.Errorf("city should not be overwritten, got %q", r.City)
		}
		if r.ASN != 7922 {
			t.Errorf("asn should not be overwritten, got %d", r.ASN)
		}
		if r.Organization != "Comcast" {
			t.Errorf("organization should not be overwritten, got %q", r.Organization)
		}
	})

	t.Run("uses first subdivision as province", func(t *testing.T) {
		r := &Result{}
		rec := &cityRecord{}
		rec.Subdivisions = []struct {
			Names map[string]string `maxminddb:"names"`
		}{
			{Names: map[string]string{"en": "Province1"}},
			{Names: map[string]string{"en": "Province2"}},
			{Names: map[string]string{"en": "Province3"}},
		}
		mergeRecord(r, rec)
		if r.Province != "Province1" {
			t.Errorf("province = %q, want Province1 (first subdivision)", r.Province)
		}
	})

	t.Run("handles empty names maps", func(t *testing.T) {
		r := &Result{}
		rec := &cityRecord{}
		rec.Country.Names = map[string]string{}
		rec.City.Names = map[string]string{}
		rec.Subdivisions = []struct {
			Names map[string]string `maxminddb:"names"`
		}{{Names: map[string]string{}}}

		mergeRecord(r, rec)

		if r.Country != "" {
			t.Errorf("country = %q, want empty", r.Country)
		}
		if r.City != "" {
			t.Errorf("city = %q, want empty", r.City)
		}
		if r.Province != "" {
			t.Errorf("province = %q, want empty", r.Province)
		}
	})

	t.Run("partial result merge", func(t *testing.T) {
		r := &Result{Province: "Beijing"}
		rec := &cityRecord{}
		rec.Country.ISOCode = "CN"
		rec.Country.Names = map[string]string{"en": "China"}
		rec.City.Names = map[string]string{"en": "Chaoyang"}

		mergeRecord(r, rec)

		if r.Country != "China" {
			t.Errorf("country = %q, want China", r.Country)
		}
		if r.Province != "Beijing" {
			t.Errorf("province = %q, want Beijing (not overwritten)", r.Province)
		}
		if r.City != "Chaoyang" {
			t.Errorf("city = %q, want Chaoyang", r.City)
		}
	})
}
