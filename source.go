package ipgeo

import "net/netip"

// Source is the interface for IP geolocation data providers.
type Source interface {
	// Lookup returns geolocation data for addr.
	// Missing records return a nil Result with a nil error.
	Lookup(addr netip.Addr) (*Result, error)
	// Name returns the configured source name.
	Name() string
	// Close releases resources held by the source.
	Close() error
}
