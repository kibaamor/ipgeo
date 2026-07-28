package ipgeo

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/lionsoul2014/ip2region/binding/golang/service"
)

type xdbSource struct {
	name     string
	searcher xdbSearcher
}

type xdbSearcher interface {
	Search(ip any) (string, error)
	Close()
}

var newIP2RegionWithPath = func(v4Path, v6Path string) (xdbSearcher, error) {
	return service.NewIp2RegionWithPath(v4Path, v6Path)
}

func openXDB(name, v4Path, v6Path string) (Source, error) {
	if v4Path == "" && v6Path == "" {
		return nil, errors.New("at least one of v4Path or v6Path must be non-empty")
	}
	searcher, err := newIP2RegionWithPath(v4Path, v6Path)
	if err != nil {
		return nil, err
	}
	return &xdbSource{name: name, searcher: searcher}, nil
}

func (d *xdbSource) Name() string { return d.name }

func (d *xdbSource) Close() error {
	d.searcher.Close()
	return nil
}

// cleanXDBField normalises an XDB field value; "0" is the sentinel for missing data.
func cleanXDBField(s string) string {
	if s == "0" || s == "" {
		return ""
	}
	return s
}

func (d *xdbSource) Lookup(_ context.Context, addr netip.Addr) (Result, error) {
	info, err := d.searcher.Search(addr.String())
	if err != nil {
		return Result{}, err
	}
	if info == "" {
		return Result{}, ErrNotFound
	}

	parts := strings.Split(info, "|")
	if len(parts) < 5 {
		return Result{}, fmt.Errorf("unexpected xdb record format: %q", info)
	}

	result := Result{
		IP:           addr,
		Source:       d.name,
		Country:      cleanXDBField(parts[0]),
		Province:     cleanXDBField(parts[1]),
		City:         cleanXDBField(parts[2]),
		Organization: cleanXDBField(parts[3]),
		CountryCode:  cleanXDBField(parts[4]),
	}
	if result.IsEmpty() {
		return Result{}, ErrNotFound
	}
	return result, nil
}
