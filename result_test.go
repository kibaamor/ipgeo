package ipgeo

import (
	"encoding/json"
	"net/netip"
	"testing"
)

var addr1 = netip.MustParseAddr("1.2.3.4")

func makeResult(countryCode, country, province, city, organization string, asn uint32) Result {
	return Result{IP: addr1, Source: "test", CountryCode: countryCode, Country: country, Province: province, City: city, Organization: organization, ASN: asn}
}

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		name  string
		r     Result
		empty bool
	}{
		{"zero value", Result{}, true},
		{"only ip set", Result{IP: addr1}, true},
		{"only source set", Result{Source: "x"}, true},
		{"countryCode set", makeResult("CN", "", "", "", "", 0), false},
		{"country set", makeResult("", "China", "", "", "", 0), false},
		{"province set", makeResult("", "", "Beijing", "", "", 0), false},
		{"city set", makeResult("", "", "", "Beijing", "", 0), false},
		{"organization set", makeResult("", "", "", "", "ChinaNet", 0), false},
		{"asn set", makeResult("", "", "", "", "", 4134), false},
		{"all set", makeResult("CN", "China", "Beijing", "Beijing", "ChinaNet", 4134), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.IsEmpty(); got != tt.empty {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.empty)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name     string
		r        Result
		expected string
	}{
		{
			name:     "empty result",
			r:        Result{},
			expected: "",
		},
		{
			name:     "country only via Country field",
			r:        makeResult("", "China", "", "", "", 0),
			expected: "China",
		},
		{
			name:     "country only via CountryCode fallback",
			r:        makeResult("CN", "", "", "", "", 0),
			expected: "CN",
		},
		{
			name:     "Country preferred over CountryCode",
			r:        makeResult("CN", "China", "", "", "", 0),
			expected: "China",
		},
		{
			name:     "country/province/city",
			r:        makeResult("CN", "China", "Beijing", "Haidian", "", 0),
			expected: "China/Beijing/Haidian",
		},
		{
			name:     "deduplicate adjacent: country==province",
			r:        makeResult("CN", "Shanghai", "Shanghai", "Shanghai", "", 0),
			expected: "Shanghai",
		},
		{
			name:     "deduplicate adjacent: province==city",
			r:        makeResult("CN", "China", "Beijing", "Beijing", "", 0),
			expected: "China/Beijing",
		},
		{
			name:     "organization only",
			r:        makeResult("", "", "", "", "ChinaNet", 0),
			expected: "ChinaNet",
		},
		{
			name:     "asn only",
			r:        makeResult("", "", "", "", "", 4134),
			expected: "4134",
		},
		{
			name:     "organization and asn",
			r:        makeResult("", "", "", "", "ChinaNet", 4134),
			expected: "ChinaNet,4134",
		},
		{
			name:     "full result",
			r:        makeResult("CN", "China", "Beijing", "Beijing", "ChinaNet", 4134),
			expected: "China/Beijing,ChinaNet,4134",
		},
		{
			name:     "no location, organization and asn",
			r:        makeResult("", "", "", "", "Cloudflare", 13335),
			expected: "Cloudflare,13335",
		},
		{
			name:     "location and organization, no asn",
			r:        makeResult("", "US", "", "New York", "Comcast", 0),
			expected: "US/New York,Comcast",
		},
		{
			name:     "location and asn, no organization",
			r:        makeResult("", "US", "", "New York", "", 7922),
			expected: "US/New York,7922",
		},
		{
			name:     "province and city same, country different",
			r:        makeResult("", "France", "Paris", "Paris", "", 0),
			expected: "France/Paris",
		},
		{
			name:     "all three location fields same",
			r:        makeResult("", "Rome", "Rome", "Rome", "", 0),
			expected: "Rome",
		},
		{
			name:     "city empty but province and country present",
			r:        makeResult("", "US", "California", "", "", 0),
			expected: "US/California",
		},
		{
			name:     "city only",
			r:        makeResult("", "", "", "Singapore", "", 0),
			expected: "Singapore",
		},
		{
			name:     "country code fallback deduplicates adjacent province",
			r:        makeResult("HK", "", "HK", "Central", "", 0),
			expected: "HK/Central",
		},
		{
			name:     "non-adjacent duplicate location segments are kept",
			r:        makeResult("", "Paris", "France", "Paris", "", 0),
			expected: "Paris/France/Paris",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFields(t *testing.T) {
	r := Result{IP: addr1, Source: "src", CountryCode: "CN", Country: "China", Province: "Beijing", City: "Haidian", Organization: "ChinaNet", ASN: 4134}
	if r.IP != addr1 {
		t.Errorf("IP = %v, want %v", r.IP, addr1)
	}
	if r.Source != "src" {
		t.Errorf("Source = %q, want %q", r.Source, "src")
	}
	if r.CountryCode != "CN" {
		t.Errorf("CountryCode = %q, want %q", r.CountryCode, "CN")
	}
	if r.Country != "China" {
		t.Errorf("Country = %q, want %q", r.Country, "China")
	}
	if r.Province != "Beijing" {
		t.Errorf("Province = %q, want %q", r.Province, "Beijing")
	}
	if r.City != "Haidian" {
		t.Errorf("City = %q, want %q", r.City, "Haidian")
	}
	if r.Organization != "ChinaNet" {
		t.Errorf("Organization = %q, want %q", r.Organization, "ChinaNet")
	}
	if r.ASN != 4134 {
		t.Errorf("ASN = %d, want %d", r.ASN, 4134)
	}
}

func TestMarshalJSON(t *testing.T) {
	r := Result{IP: addr1, Source: "src", CountryCode: "CN", Country: "China", Province: "Beijing", City: "Haidian", Organization: "ChinaNet", ASN: 4134}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got, ok := m["ip"]; !ok {
		t.Error("key \"ip\" missing in JSON")
	} else if got != addr1.String() {
		t.Errorf("JSON[ip] = %v, want %q", got, addr1.String())
	}

	checks := map[string]any{
		"source":       "src",
		"country_code": "CN",
		"country":      "China",
		"province":     "Beijing",
		"city":         "Haidian",
		"organization": "ChinaNet",
	}
	for k, want := range checks {
		if got, ok := m[k]; !ok {
			t.Errorf("key %q missing in JSON", k)
		} else if got != want {
			t.Errorf("JSON[%q] = %v, want %v", k, got, want)
		}
	}

	if asn, ok := m["asn"].(float64); !ok || uint(asn) != 4134 {
		t.Errorf("JSON[asn] = %v, want 4134", m["asn"])
	}
}

func TestMarshalJSON_OmitEmpty(t *testing.T) {
	r := Result{IP: addr1}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got := m["ip"]; got != addr1.String() {
		t.Errorf("JSON[ip] = %v, want %q", got, addr1.String())
	}
	omitted := []string{"source", "country_code", "country", "province", "city", "organization", "asn"}
	for _, k := range omitted {
		if _, ok := m[k]; ok {
			t.Errorf("key %q should be omitted when empty", k)
		}
	}
	if len(m) != 1 {
		t.Errorf("JSON contains %d keys, want only ip; map=%v", len(m), m)
	}
}

func TestMarshalJSON_ZeroValueIncludesOnlyInvalidIP(t *testing.T) {
	data, err := json.Marshal(Result{})
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	const want = `{"ip":""}`
	if string(data) != want {
		t.Errorf("json.Marshal(Result{}) = %s, want %s", data, want)
	}
}

func TestUnmarshalJSON_RoundTrip(t *testing.T) {
	original := Result{IP: addr1, Source: "src", CountryCode: "CN", Country: "China", Province: "Beijing", City: "Haidian", Organization: "ChinaNet", ASN: 4134}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got Result
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if got != original {
		t.Errorf("round-trip = %#v, want %#v", got, original)
	}
}

func TestZeroValue(t *testing.T) {
	r := Result{}
	if !r.IsEmpty() {
		t.Error("expected IsEmpty() = true for zero-value result")
	}
	if r.String() != "" {
		t.Errorf("String() = %q, want empty", r.String())
	}
}
