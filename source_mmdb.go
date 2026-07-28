package ipgeo

import (
	"context"
	"errors"
	"net"
	"net/netip"

	"github.com/oschwald/maxminddb-golang"
)

type mmdbSource struct {
	name      string
	db        mmdbReader
	companion mmdbReader
}

type mmdbReader interface {
	Lookup(net.IP, any) error
	Close() error
}

var openMaxMindDB = func(path string) (mmdbReader, error) {
	return maxminddb.Open(path)
}

type cityRecord struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	AutonomousSystemNumber       uint   `maxminddb:"autonomous_system_number"`
	AutonomousSystemOrganization string `maxminddb:"autonomous_system_organization"`
}

func openMMDB(name, path, companionPath string) (Source, error) {
	if path == "" {
		return nil, errors.New("path must be non-empty")
	}
	db, err := openMaxMindDB(path)
	if err != nil {
		return nil, err
	}

	var companion mmdbReader
	if companionPath != "" {
		companion, err = openMaxMindDB(companionPath)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	return &mmdbSource{name: name, db: db, companion: companion}, nil
}

func (m *mmdbSource) Name() string { return m.name }

func (m *mmdbSource) Close() error {
	var companionErr error
	if m.companion != nil {
		companionErr = m.companion.Close()
	}
	return errors.Join(companionErr, m.db.Close())
}

func pickName(names map[string]string) string {
	if v, ok := names["en"]; ok {
		return v
	}
	for _, v := range names {
		return v
	}
	return ""
}

func mergeRecord(result *Result, record *cityRecord) {
	if result.CountryCode == "" {
		result.CountryCode = record.Country.ISOCode
	}
	if result.Country == "" {
		result.Country = pickName(record.Country.Names)
	}
	if result.Province == "" && len(record.Subdivisions) > 0 {
		result.Province = pickName(record.Subdivisions[0].Names)
	}
	if result.City == "" {
		result.City = pickName(record.City.Names)
	}
	if result.ASN == 0 {
		result.ASN = uint32(record.AutonomousSystemNumber) //nolint:gosec // ASN is 32-bit per RFC 6793
	}
	if result.Organization == "" {
		result.Organization = record.AutonomousSystemOrganization
	}
}

func addrToNetIP(addr netip.Addr) net.IP {
	if addr.Is4() {
		a := addr.As4()
		return net.IP(a[:])
	}
	a := addr.As16()
	return net.IP(a[:])
}

func (m *mmdbSource) Lookup(_ context.Context, addr netip.Addr) (Result, error) {
	ip := addrToNetIP(addr)

	var record cityRecord
	if err := m.db.Lookup(ip, &record); err != nil {
		return Result{}, err
	}

	var result Result
	result.IP = addr
	result.Source = m.name
	mergeRecord(&result, &record)

	if m.companion != nil {
		var companion cityRecord
		if err := m.companion.Lookup(ip, &companion); err != nil {
			return Result{}, err
		}
		mergeRecord(&result, &companion)
	}

	if result.IsEmpty() {
		return Result{}, ErrNotFound
	}
	return result, nil
}
