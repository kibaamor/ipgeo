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

		if r.countryCode != "CN" {
			t.Errorf("countryCode = %q, want CN", r.countryCode)
		}
		if r.country != "China" {
			t.Errorf("country = %q, want China", r.country)
		}
		if r.province != "Beijing" {
			t.Errorf("province = %q, want Beijing", r.province)
		}
		if r.city != "Haidian" {
			t.Errorf("city = %q, want Haidian", r.city)
		}
		if r.asn != 4134 {
			t.Errorf("asn = %d, want 4134", r.asn)
		}
		if r.organization != "ChinaNet" {
			t.Errorf("organization = %q, want ChinaNet", r.organization)
		}
	})

	t.Run("does not overwrite existing fields", func(t *testing.T) {
		r := &Result{
			countryCode:  "US",
			country:      "United States",
			province:     "CA",
			city:         "LA",
			asn:          7922,
			organization: "Comcast",
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

		if r.countryCode != "US" {
			t.Errorf("countryCode should not be overwritten, got %q", r.countryCode)
		}
		if r.country != "United States" {
			t.Errorf("country should not be overwritten, got %q", r.country)
		}
		if r.province != "CA" {
			t.Errorf("province should not be overwritten, got %q", r.province)
		}
		if r.city != "LA" {
			t.Errorf("city should not be overwritten, got %q", r.city)
		}
		if r.asn != 7922 {
			t.Errorf("asn should not be overwritten, got %d", r.asn)
		}
		if r.organization != "Comcast" {
			t.Errorf("organization should not be overwritten, got %q", r.organization)
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
		if r.province != "Province1" {
			t.Errorf("province = %q, want Province1 (first subdivision)", r.province)
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

		if r.country != "" {
			t.Errorf("country = %q, want empty", r.country)
		}
		if r.city != "" {
			t.Errorf("city = %q, want empty", r.city)
		}
		if r.province != "" {
			t.Errorf("province = %q, want empty", r.province)
		}
	})

	t.Run("partial result merge", func(t *testing.T) {
		r := &Result{province: "Beijing"}
		rec := &cityRecord{}
		rec.Country.ISOCode = "CN"
		rec.Country.Names = map[string]string{"en": "China"}
		rec.City.Names = map[string]string{"en": "Chaoyang"}

		mergeRecord(r, rec)

		if r.country != "China" {
			t.Errorf("country = %q, want China", r.country)
		}
		if r.province != "Beijing" {
			t.Errorf("province = %q, want Beijing (not overwritten)", r.province)
		}
		if r.city != "Chaoyang" {
			t.Errorf("city = %q, want Chaoyang", r.city)
		}
	})
}
