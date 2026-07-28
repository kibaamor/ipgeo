package ipgeo

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

type resultEntry struct {
	result Result
	ok     bool
}

type cachedSource struct {
	source  Source
	results *ttlcache.Cache[netip.Addr, resultEntry]
	errors  *ttlcache.Cache[netip.Addr, error]
}

func newCachedSource(src Source, maxEntries uint, resultTTL, errorTTL time.Duration) (*cachedSource, error) {
	if maxEntries == 0 {
		return nil, errors.New("ipgeo: create cache: maxEntries must be positive, got 0")
	}
	if resultTTL < 0 {
		return nil, fmt.Errorf("ipgeo: create cache: resultTTL must not be negative, got %s", resultTTL)
	}
	if errorTTL < 0 {
		return nil, fmt.Errorf("ipgeo: create cache: errorTTL must not be negative, got %s", errorTTL)
	}
	cache := ttlcache.New(
		ttlcache.WithCapacity[netip.Addr, resultEntry](uint64(maxEntries)),
		ttlcache.WithTTL[netip.Addr, resultEntry](resultTTL),
	)
	var errors *ttlcache.Cache[netip.Addr, error]
	if errorTTL > 0 {
		errors = ttlcache.New(
			ttlcache.WithCapacity[netip.Addr, error](uint64(maxEntries)),
			ttlcache.WithTTL[netip.Addr, error](errorTTL),
			ttlcache.WithDisableTouchOnHit[netip.Addr, error](),
		)
	}
	return &cachedSource{
		source:  src,
		results: cache,
		errors:  errors,
	}, nil
}

func (s *cachedSource) Lookup(ctx context.Context, addr netip.Addr) (Result, error) {
	addr = addr.Unmap()
	if ok, result, err := s.lookupCache(addr); ok {
		return result, err
	}

	result, lookupErr := s.source.Lookup(ctx, addr)
	if lookupErr != nil {
		if errors.Is(lookupErr, ErrNotFound) {
			s.results.Set(addr, resultEntry{ok: false}, ttlcache.DefaultTTL)
			return Result{}, lookupErr
		}
		if s.errors != nil && !isContextError(lookupErr) {
			s.errors.Set(addr, lookupErr, ttlcache.DefaultTTL)
		}
		return Result{}, lookupErr
	}
	s.results.Set(addr, resultEntry{result: result, ok: true}, ttlcache.DefaultTTL)
	return result, nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s *cachedSource) lookupCache(addr netip.Addr) (bool, Result, error) {
	if item := s.results.Get(addr); item != nil {
		entry := item.Value()
		if entry.ok {
			return true, entry.result, nil
		}
		return true, Result{}, ErrNotFound
	}
	if s.errors != nil {
		if item := s.errors.Get(addr); item != nil {
			return true, Result{}, item.Value()
		}
	}
	return false, Result{}, nil
}

func (s *cachedSource) Name() string {
	return s.source.Name()
}

func (s *cachedSource) Close() error {
	s.results.DeleteAll()
	if s.errors != nil {
		s.errors.DeleteAll()
	}
	return s.source.Close()
}
