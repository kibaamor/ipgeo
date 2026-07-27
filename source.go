package ipgeo

import (
	"context"
	"net/netip"
)

// Source is the interface for IP geolocation data providers.
type Source interface {
	// Lookup returns geolocation data for addr.
	// Missing records return a nil Result and ErrNotFound.
	// ctx is respected: a cancelled context yields a context error and is not
	// cached by wrapping layers.
	Lookup(ctx context.Context, addr netip.Addr) (*Result, error)
	// Name returns the configured source name.
	Name() string
	// Close releases resources held by the source.
	Close() error
}
