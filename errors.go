package ipgeo

import "errors"

// Sentinel errors returned by Client, Open, and Source.Lookup. Use errors.Is to test for them.
var (
	// ErrNoSources is returned by Open when no source creator is provided.
	ErrNoSources = errors.New("ipgeo: at least one source creator is required")
	// ErrDuplicateSource is returned by Open when two sources share the same name.
	ErrDuplicateSource = errors.New("ipgeo: duplicate source name")
	// ErrSourceNotConfigured is returned by LookupFrom when the named source is not configured.
	ErrSourceNotConfigured = errors.New("ipgeo: source not configured")
	// ErrNotFound is returned by Lookup, LookupAll, and LookupFrom (and Source.Lookup)
	// when no matching record is found for the address.
	ErrNotFound = errors.New("ipgeo: not found")
)
