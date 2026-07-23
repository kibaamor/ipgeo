package ipgeo

import (
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"
)

// Result holds the geolocation and network information resolved for an IP address.
type Result struct {
	ip           netip.Addr
	source       string
	countryCode  string
	country      string
	province     string
	city         string
	organization string
	asn          uint32
}

// NewResult returns a Result populated with the provided values.
func NewResult(ip netip.Addr, source, countryCode, country, province, city, organization string, asn uint32) *Result {
	return &Result{
		ip:           ip,
		source:       source,
		countryCode:  countryCode,
		country:      country,
		province:     province,
		city:         city,
		organization: organization,
		asn:          asn,
	}
}

// IP returns the queried IP address.
func (r Result) IP() netip.Addr { return r.ip }

// Source returns the source name that produced the result.
func (r Result) Source() string { return r.source }

// CountryCode returns the ISO country code, when available.
func (r Result) CountryCode() string { return r.countryCode }

// Country returns the country name, when available.
func (r Result) Country() string { return r.country }

// Province returns the province or region name, when available.
func (r Result) Province() string { return r.province }

// City returns the city name, when available.
func (r Result) City() string { return r.city }

// Organization returns the network operator or ASN organization name, when available.
func (r Result) Organization() string { return r.organization }

// ASN returns the autonomous system number, when available.
func (r Result) ASN() uint32 { return r.asn }

// MarshalJSON implements json.Marshaler.
func (r Result) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		IP           netip.Addr `json:"ip"`
		Source       string     `json:"source,omitempty"`
		CountryCode  string     `json:"country_code,omitempty"`
		Country      string     `json:"country,omitempty"`
		Province     string     `json:"province,omitempty"`
		City         string     `json:"city,omitempty"`
		Organization string     `json:"organization,omitempty"`
		ASN          uint32     `json:"asn,omitempty"`
	}{
		IP:           r.ip,
		Source:       r.source,
		CountryCode:  r.countryCode,
		Country:      r.country,
		Province:     r.province,
		City:         r.city,
		Organization: r.organization,
		ASN:          r.asn,
	})
}

// IsEmpty reports whether the result contains no geolocation or network data.
func (r Result) IsEmpty() bool {
	return r.countryCode == "" && r.country == "" &&
		r.province == "" && r.city == "" && r.organization == "" && r.asn == 0
}

// String returns a compact representation: "CountryOrCode/Province/City,Organization,ASN".
// Adjacent duplicate location segments and empty fields are omitted.
func (r Result) String() string {
	country := r.country
	if country == "" {
		country = r.countryCode
	}

	var sb strings.Builder
	prev := ""
	for _, p := range [3]string{country, r.province, r.city} {
		if p == "" || p == prev {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte('/')
		}
		sb.WriteString(p)
		prev = p
	}

	if r.organization != "" {
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(r.organization)
	}

	if r.asn != 0 {
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatUint(uint64(r.asn), 10))
	}

	return sb.String()
}
