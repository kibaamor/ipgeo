package ipgeo

import (
	"context"
	"net/netip"
	"strings"

	ip2loc "github.com/ip2location/ip2location-go/v9"
)

type ip2locationSource struct {
	name string
	db   ip2locationReader
}

type ip2locationReader interface {
	Get_all(ipaddress string) (ip2loc.IP2Locationrecord, error)
	Close()
}

var openIP2LocationDB = func(path string) (ip2locationReader, error) {
	return ip2loc.OpenDB(path)
}

func openIP2Location(name, path string) (Source, error) {
	db, err := openIP2LocationDB(path)
	if err != nil {
		return nil, err
	}
	return &ip2locationSource{name: name, db: db}, nil
}

func (s *ip2locationSource) Name() string { return s.name }

func (s *ip2locationSource) Close() error {
	s.db.Close()
	return nil
}

func cleanIP2LocationField(s string) string {
	if s == "-" || s == "" ||
		strings.HasPrefix(s, "This parameter") ||
		strings.HasPrefix(s, "This is a sample") {
		return ""
	}
	return s
}

func (s *ip2locationSource) Lookup(_ context.Context, addr netip.Addr) (Result, error) {
	rec, err := s.db.Get_all(addr.String())
	if err != nil {
		return Result{}, err
	}

	result := Result{
		CountryCode:  cleanIP2LocationField(rec.Country_short),
		Country:      cleanIP2LocationField(rec.Country_long),
		Province:     cleanIP2LocationField(rec.Region),
		City:         cleanIP2LocationField(rec.City),
		Organization: cleanIP2LocationField(rec.Isp),
	}
	if asnStr := cleanIP2LocationField(rec.Asn); asnStr != "" {
		result.ASN = parseASN(asnStr)
	}

	return finalize(s.name, addr, result)
}
