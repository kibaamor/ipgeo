package ipgeo

import (
	"context"
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

func (s *singleflightSource) Lookup(ctx context.Context, addr netip.Addr) (*Result, error) {
	addr = addr.Unmap()
	ch := s.group.DoChan(addr.String(), func() (any, error) { //nolint:contextcheck // shared lookup must not be bound to a single caller's context
		return s.source.Lookup(context.Background(), addr)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return nil, r.Err
		}
		result, ok := r.Val.(*Result)
		if !ok {
			return nil, fmt.Errorf("ipgeo: singleflight returned unexpected type %T", r.Val)
		}
		return result, nil
	}
}

func (s *singleflightSource) Name() string {
	return s.source.Name()
}

func (s *singleflightSource) Close() error {
	return s.source.Close()
}
