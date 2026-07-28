package ipgeo

import (
	"net/netip"
	"strconv"
	"strings"
)

// Result holds the geolocation and network information resolved for an IP address.
// Fields are exported so callers can read and serialize them directly. A Result
// is a value type: Lookup methods and Source.Lookup return it by value, and the
// cache stores its own copy, so a caller mutating a returned Result never affects
// cached state. Use IsEmpty to test for a record with no data.
type Result struct {
	IP           netip.Addr `json:"ip"`
	Source       string     `json:"source,omitempty"`
	CountryCode  string     `json:"country_code,omitempty"`
	Country      string     `json:"country,omitempty"`
	Province     string     `json:"province,omitempty"`
	City         string     `json:"city,omitempty"`
	Organization string     `json:"organization,omitempty"`
	ASN          uint32     `json:"asn,omitempty"`
}

// IsEmpty reports whether the result contains no geolocation or network data.
func (r Result) IsEmpty() bool {
	return r.CountryCode == "" && r.Country == "" &&
		r.Province == "" && r.City == "" && r.Organization == "" && r.ASN == 0
}

// String returns a compact representation: "CountryOrCode/Province/City,Organization,ASN".
// Adjacent duplicate location segments and empty fields are omitted.
func (r Result) String() string {
	country := r.Country
	if country == "" {
		country = r.CountryCode
	}

	var sb strings.Builder
	prev := ""
	for _, p := range [3]string{country, r.Province, r.City} {
		if p == "" || p == prev {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte('/')
		}
		sb.WriteString(p)
		prev = p
	}

	if r.Organization != "" {
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(r.Organization)
	}

	if r.ASN != 0 {
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatUint(uint64(r.ASN), 10))
	}

	return sb.String()
}
