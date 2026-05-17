package ipgeo

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	ip2loc "github.com/ip2location/ip2location-go/v9"
	"github.com/ipipdotnet/ipdb-go"
)

func TestMMDBSourceLookup(t *testing.T) {
	addr := netip.MustParseAddr("1.2.3.4")
	primary := &fakeMMDBReader{record: cityRecord{}}
	primary.record.Country.ISOCode = "CN"
	primary.record.Country.Names = map[string]string{"en": "China"}
	companion := &fakeMMDBReader{record: cityRecord{AutonomousSystemNumber: 4134, AutonomousSystemOrganization: "ChinaNet"}}
	src := &mmdbSource{name: "mmdb", db: primary, companion: companion}

	result, err := src.Lookup(addr)
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if result.CountryCode() != "CN" || result.Country() != "China" || result.ASN() != 4134 || result.Organization() != "ChinaNet" {
		t.Fatalf("result = %#v", result)
	}
	if !primary.ip.Equal(net.IPv4(1, 2, 3, 4)) || !companion.ip.Equal(net.IPv4(1, 2, 3, 4)) {
		t.Fatalf("lookup IPs = %v/%v, want 1.2.3.4", primary.ip, companion.ip)
	}
}

func TestMMDBSourceLookupErrors(t *testing.T) {
	sentinelErr := errors.New("lookup failed")
	tests := []struct {
		name string
		src  *mmdbSource
		want error
	}{
		{name: "primary error", src: &mmdbSource{db: &fakeMMDBReader{err: sentinelErr}}, want: sentinelErr},
		{name: "companion error", src: &mmdbSource{db: &fakeMMDBReader{record: cityRecord{AutonomousSystemNumber: 1}}, companion: &fakeMMDBReader{err: sentinelErr}}, want: sentinelErr},
		{name: "empty result", src: &mmdbSource{db: &fakeMMDBReader{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.src.Lookup(testAddr)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Lookup() error = %v, want nil", err)
				}
				if result != nil {
					t.Fatalf("Lookup() result = %#v, want nil", result)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Lookup() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestMMDBSourceClose(t *testing.T) {
	errDB := errors.New("db close")
	errCompanion := errors.New("companion close")
	db := &fakeMMDBReader{closeErr: errDB}
	companion := &fakeMMDBReader{closeErr: errCompanion}
	err := (&mmdbSource{db: db, companion: companion}).Close()
	if !errors.Is(err, errDB) || !errors.Is(err, errCompanion) {
		t.Fatalf("Close() error = %v, want both close errors", err)
	}
	if !db.closed || !companion.closed {
		t.Fatalf("closed flags = %v/%v, want true/true", db.closed, companion.closed)
	}
}

func TestIPDBSourceLookup(t *testing.T) {
	tests := []struct {
		name string
		info *ipdb.CityInfo
		asn  uint
	}{
		{
			name: "asn info",
			info: &ipdb.CityInfo{CountryCode: "CN", CountryName: "China", RegionName: "Beijing", CityName: "Haidian", IspDomain: "ChinaNet", ASNInfo: []ipdb.ASNInfo{{ASN: 4134}}},
			asn:  4134,
		},
		{
			name: "asn string",
			info: &ipdb.CityInfo{CountryCode: "US", ASN: "AS13335"},
			asn:  13335,
		},
		{
			name: "negative asn info ignored",
			info: &ipdb.CityInfo{CountryCode: "US", ASNInfo: []ipdb.ASNInfo{{ASN: -1}}},
			asn:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeIPDBReader{info: tt.info}
			src := &ipdbSource{name: "ipdb", db: reader, lang: "EN"}
			result, err := src.Lookup(testAddr)
			if err != nil {
				t.Fatalf("Lookup() error: %v", err)
			}
			if result.ASN() != tt.asn {
				t.Fatalf("ASN() = %d, want %d", result.ASN(), tt.asn)
			}
			if reader.addr != testAddr.String() || reader.lang != "EN" {
				t.Fatalf("FindInfo called with %q/%q", reader.addr, reader.lang)
			}
		})
	}
}

func TestIPDBSourceLookupErrors(t *testing.T) {
	sentinelErr := errors.New("ipdb failed")
	tests := []struct {
		name string
		src  *ipdbSource
		want error
	}{
		{name: "lookup error", src: &ipdbSource{db: &fakeIPDBReader{err: sentinelErr}, lang: "EN"}, want: sentinelErr},
		{name: "data missing", src: &ipdbSource{db: &fakeIPDBReader{err: ipdb.ErrDataNotExists}, lang: "EN"}},
		{name: "empty result", src: &ipdbSource{db: &fakeIPDBReader{info: &ipdb.CityInfo{}}, lang: "EN"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.src.Lookup(testAddr)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Lookup() error = %v, want nil", err)
				}
				if result != nil {
					t.Fatalf("Lookup() result = %#v, want nil", result)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Lookup() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestIP2LocationSourceLookup(t *testing.T) {
	reader := &fakeIP2LocationReader{record: ip2loc.IP2Locationrecord{
		Country_short: "CN",
		Country_long:  "China",
		Region:        "Beijing",
		City:          "Haidian",
		Isp:           "ChinaNet",
		Asn:           "AS4134",
	}}
	src := &ip2locationSource{name: "ip2location", db: reader}

	result, err := src.Lookup(testAddr)
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if result.CountryCode() != "CN" || result.ASN() != 4134 {
		t.Fatalf("result = %#v", result)
	}
	if reader.addr != testAddr.String() {
		t.Fatalf("Get_all called with %q", reader.addr)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if !reader.closed {
		t.Fatal("reader was not closed")
	}
}

func TestIP2LocationSourceLookupErrors(t *testing.T) {
	sentinelErr := errors.New("ip2location failed")
	tests := []struct {
		name string
		src  *ip2locationSource
		want error
	}{
		{name: "lookup error", src: &ip2locationSource{db: &fakeIP2LocationReader{err: sentinelErr}}, want: sentinelErr},
		{name: "empty result", src: &ip2locationSource{db: &fakeIP2LocationReader{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.src.Lookup(testAddr)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Lookup() error = %v, want nil", err)
				}
				if result != nil {
					t.Fatalf("Lookup() result = %#v, want nil", result)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Lookup() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestXDBSourceLookup(t *testing.T) {
	reader := &fakeXDBSearcher{info: "China|Beijing|Haidian|ChinaNet|CN"}
	src := &xdbSource{name: "xdb", searcher: reader}

	result, err := src.Lookup(testAddr)
	if err != nil {
		t.Fatalf("Lookup() error: %v", err)
	}
	if result.Country() != "China" || result.Province() != "Beijing" || result.City() != "Haidian" || result.Organization() != "ChinaNet" || result.CountryCode() != "CN" {
		t.Fatalf("result = %#v", result)
	}
	if reader.ip != testAddr.String() {
		t.Fatalf("Search called with %v", reader.ip)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if !reader.closed {
		t.Fatal("searcher was not closed")
	}
}

func TestXDBSourceLookupErrors(t *testing.T) {
	sentinelErr := errors.New("xdb failed")
	tests := []struct {
		name    string
		src     *xdbSource
		wantErr string
	}{
		{name: "lookup error", src: &xdbSource{searcher: &fakeXDBSearcher{err: sentinelErr}}, wantErr: sentinelErr.Error()},
		{name: "empty result", src: &xdbSource{searcher: &fakeXDBSearcher{}}},
		{name: "unexpected format", src: &xdbSource{searcher: &fakeXDBSearcher{info: "too|short"}}, wantErr: "unexpected xdb record format"},
		{name: "empty fields", src: &xdbSource{searcher: &fakeXDBSearcher{info: "0|0|0|0|0"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.src.Lookup(testAddr)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Lookup() error = %v, want nil", err)
				}
				if result != nil {
					t.Fatalf("Lookup() result = %#v, want nil", result)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Lookup() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

type fakeMMDBReader struct {
	record   cityRecord
	ip       net.IP
	err      error
	closeErr error
	closed   bool
}

func (r *fakeMMDBReader) Lookup(ip net.IP, v any) error {
	r.ip = append(net.IP(nil), ip...)
	if r.err != nil {
		return r.err
	}
	*v.(*cityRecord) = r.record
	return nil
}

func (r *fakeMMDBReader) Close() error {
	r.closed = true
	return r.closeErr
}

type fakeIPDBReader struct {
	info  *ipdb.CityInfo
	err   error
	addr  string
	lang  string
	langs []string
}

func (r *fakeIPDBReader) FindInfo(addr, language string) (*ipdb.CityInfo, error) {
	r.addr, r.lang = addr, language
	if r.err != nil {
		return nil, r.err
	}
	return r.info, nil
}

func (r *fakeIPDBReader) Languages() []string { return r.langs }

type fakeIP2LocationReader struct {
	record ip2loc.IP2Locationrecord
	err    error
	addr   string
	closed bool
}

func (r *fakeIP2LocationReader) Get_all(ipaddress string) (ip2loc.IP2Locationrecord, error) {
	r.addr = ipaddress
	if r.err != nil {
		return ip2loc.IP2Locationrecord{}, r.err
	}
	return r.record, nil
}

func (r *fakeIP2LocationReader) Close() { r.closed = true }

type fakeXDBSearcher struct {
	info   string
	err    error
	ip     any
	closed bool
}

func (s *fakeXDBSearcher) Search(ip any) (string, error) {
	s.ip = ip
	if s.err != nil {
		return "", s.err
	}
	return s.info, nil
}

func (s *fakeXDBSearcher) Close() { s.closed = true }
