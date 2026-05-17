package ipgeo

import (
	"fmt"
	"net/netip"

	"golang.org/x/sync/singleflight"
)

type singleflightSource struct {
	source Source
	group  singleflight.Group
}

func newSingleflightSource(src Source) *singleflightSource {
	return &singleflightSource{source: src}
}

func (s *singleflightSource) Lookup(addr netip.Addr) (*Result, error) {
	addr = addr.Unmap()
	v, err, _ := s.group.Do(addr.String(), func() (any, error) {
		return s.source.Lookup(addr)
	})
	if err != nil {
		return nil, err
	}
	result, ok := v.(*Result)
	if !ok {
		return nil, fmt.Errorf("ipgeo: singleflight returned unexpected type %T", v)
	}
	return result, nil
}

func (s *singleflightSource) Name() string {
	return s.source.Name()
}

func (s *singleflightSource) Close() error {
	return s.source.Close()
}
