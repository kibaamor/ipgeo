package ipgeo

import (
	"errors"
	"fmt"
	"net/netip"
	"time"
)

// Client queries one or more geolocation sources.
type Client struct {
	sources        []Source
	sourceByName   map[string]Source
	cacheEntries   int
	cacheErrorsTTL time.Duration
}

// Open creates a new Client configured by the provided options.
// At least one source option (e.g. WithMMDB, WithSource) is required.
// Returns an error if any option fails or if source names are duplicated.
func Open(opts ...Option) (*Client, error) {
	c := &Client{}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	if len(c.sources) == 0 {
		return nil, errors.New("ipgeo: Open: at least one source option is required")
	}

	seen := make(map[string]struct{}, len(c.sources))
	for _, src := range c.sources {
		if _, exists := seen[src.Name()]; exists {
			_ = c.Close()
			return nil, fmt.Errorf("ipgeo: duplicate source name %q", src.Name())
		}
		seen[src.Name()] = struct{}{}
	}

	if err := c.wrapSources(); err != nil {
		_ = c.Close()
		return nil, err
	}

	return c, nil
}

func (c *Client) wrapSources() error {
	sourceByName := make(map[string]Source, len(c.sources))
	for i, src := range c.sources {
		wrapped := Source(newSingleflightSource(src))
		if c.cacheEntries > 0 {
			cached, err := newCachedSource(wrapped, c.cacheEntries, c.cacheErrorsTTL)
			if err != nil {
				return err
			}
			wrapped = cached
		}
		c.sources[i] = wrapped
		sourceByName[wrapped.Name()] = wrapped
	}
	c.sourceByName = sourceByName
	return nil
}

// SourceNames returns the names of all configured sources in order.
func (c *Client) SourceNames() []string {
	names := make([]string, len(c.sources))
	for i, src := range c.sources {
		names[i] = src.Name()
	}
	return names
}

// Lookup queries sources in order and returns the first result found.
// If no source has a matching record, it returns a nil Result with a nil error.
// IPv4-mapped IPv6 addresses are unmapped before lookup.
func (c *Client) Lookup(addr netip.Addr) (*Result, error) {
	addr = addr.Unmap()

	for _, src := range c.sources {
		result, err := src.Lookup(addr)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", src.Name(), err)
		}
		if result == nil {
			continue
		}

		return result, nil
	}

	return nil, nil
}

// LookupAll queries all sources and returns every result found.
// If no source has a matching record, it returns a nil result slice and nil error.
// Nil results from individual sources are silently skipped; errors are joined.
func (c *Client) LookupAll(addr netip.Addr) ([]*Result, error) {
	addr = addr.Unmap()

	var results []*Result
	errs := make([]error, 0, len(c.sources))
	for _, src := range c.sources {
		result, err := src.Lookup(addr)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", src.Name(), err))
			continue
		}
		if result == nil {
			continue
		}

		results = append(results, result)
	}

	return results, errors.Join(errs...)
}

// LookupFrom queries a specific named source.
// If that source has no matching record, it returns a nil Result with a nil error.
func (c *Client) LookupFrom(sourceName string, addr netip.Addr) (*Result, error) {
	target := c.sourceByName[sourceName]
	if target == nil {
		return nil, fmt.Errorf("ipgeo: source %q is not configured", sourceName)
	}

	addr = addr.Unmap()

	result, err := target.Lookup(addr)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// Close closes all sources and purges any per-source caches.
func (c *Client) Close() error {
	errs := make([]error, 0, len(c.sources))
	for _, src := range c.sources {
		errs = append(errs, src.Close())
	}

	c.sources = nil
	c.sourceByName = nil

	return errors.Join(errs...)
}
