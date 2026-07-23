package ipgeo

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

type cachedSource struct {
	source Source
	cache  *ttlcache.Cache[netip.Addr, *Result]
	errors *ttlcache.Cache[netip.Addr, error]
}

func newCachedSource(src Source, maxEntries int, cacheErrorsTTL time.Duration) (*cachedSource, error) {
	if maxEntries <= 0 {
		return nil, fmt.Errorf("ipgeo: create cache: maxEntries must be positive, got %d", maxEntries)
	}
	cache := ttlcache.New(
		ttlcache.WithCapacity[netip.Addr, *Result](uint64(maxEntries)),
	)
	var errors *ttlcache.Cache[netip.Addr, error]
	if cacheErrorsTTL > 0 {
		errors = ttlcache.New(
			ttlcache.WithCapacity[netip.Addr, error](uint64(maxEntries)),
			ttlcache.WithTTL[netip.Addr, error](cacheErrorsTTL),
			ttlcache.WithDisableTouchOnHit[netip.Addr, error](),
		)
	}
	return &cachedSource{
		source: src,
		cache:  cache,
		errors: errors,
	}, nil
}

func (s *cachedSource) Lookup(ctx context.Context, addr netip.Addr) (*Result, error) {
	addr = addr.Unmap()
	if result, err, ok := s.lookupCache(addr); ok {
		return result, err
	}

	result, lookupErr := s.source.Lookup(ctx, addr)
	if lookupErr != nil {
		if s.errors != nil && !isContextError(lookupErr) {
			s.errors.Set(addr, lookupErr, ttlcache.DefaultTTL)
		}
		return nil, lookupErr
	}
	s.cache.Set(addr, result, ttlcache.NoTTL)
	return result, nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s *cachedSource) lookupCache(addr netip.Addr) (*Result, error, bool) {
	if item := s.cache.Get(addr); item != nil {
		result := item.Value()
		return result, nil, true
	}
	if s.errors != nil {
		if item := s.errors.Get(addr); item != nil {
			return nil, item.Value(), true
		}
	}
	return nil, nil, false
}

func (s *cachedSource) Name() string {
	return s.source.Name()
}

func (s *cachedSource) Close() error {
	s.cache.DeleteAll()
	if s.errors != nil {
		s.errors.DeleteAll()
	}
	return s.source.Close()
}
