package ipgeo

import (
	"context"
	"net/netip"
)

// Source is the interface for IP geolocation data providers.
type Source interface {
	// Lookup returns geolocation data for addr.
	// Missing records return a zero Result and ErrNotFound.
	//
	// Implementations should respect ctx where feasible. Sources backed by
	// libraries that cannot be interrupted may ignore it; cancellation is then
	// still enforced by Client (it checks ctx before each source) and by
	// Singleflight. Context errors are never cached by the Cache wrapper.
	Lookup(ctx context.Context, addr netip.Addr) (Result, error)
	// Name returns the configured source name.
	Name() string
	// Close releases resources held by the source.
	Close() error
}
