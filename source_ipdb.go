package ipgeo

import (
	"context"
	"errors"
	"net/netip"

	"github.com/ipipdotnet/ipdb-go"
)

type ipdbSource struct {
	name string
	db   ipdbReader
	lang string
}

type ipdbReader interface {
	FindInfo(addr, language string) (*ipdb.CityInfo, error)
	Languages() []string
}

var newIPDBCity = func(path string) (ipdbReader, error) {
	return ipdb.NewCity(path)
}

func openIPDB(name, path string) (Source, error) {
	db, err := newIPDBCity(path)
	if err != nil {
		return nil, err
	}

	lang := ""
	// Prefer EN, then CN, then any advertised language.
	for _, l := range db.Languages() {
		if l == "EN" {
			lang = l
			break
		}
		if l == "CN" {
			lang = l
		} else if lang == "" {
			lang = l
		}
	}
	if lang == "" {
		return nil, errors.New("ipdb: database has no languages")
	}

	return &ipdbSource{name: name, db: db, lang: lang}, nil
}

func (i *ipdbSource) Name() string { return i.name }

func (i *ipdbSource) Close() error { return nil }

func (i *ipdbSource) Lookup(_ context.Context, addr netip.Addr) (Result, error) {
	info, err := i.db.FindInfo(addr.String(), i.lang)
	if err != nil {
		if errors.Is(err, ipdb.ErrDataNotExists) {
			return Result{}, ErrNotFound
		}
		return Result{}, err
	}

	result := Result{
		CountryCode:  info.CountryCode,
		Country:      info.CountryName,
		Province:     info.RegionName,
		City:         info.CityName,
		Organization: info.IspDomain,
	}

	if len(info.ASNInfo) > 0 {
		if asn := info.ASNInfo[0].ASN; asn >= 0 {
			result.ASN = uint32(asn) //nolint:gosec // ASN is 32-bit per RFC 6793
		}
	} else if info.ASN != "" {
		result.ASN = parseASN(info.ASN)
	}

	return finalize(i.name, addr, result)
}
