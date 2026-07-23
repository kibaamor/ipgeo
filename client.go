package ipgeo

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
)

// Client queries one or more geolocation sources.
//
// A Client is safe for concurrent use by multiple goroutines: Lookup, LookupAll,
// LookupFrom, and SourceNames may be called concurrently. Close is safe to call
// multiple times and from concurrent goroutines, but it must not be called
// concurrently with any query method; the result of doing so is undefined, as
// with io.Closer.
type Client struct {
	sources      []Source
	sourceByName map[string]Source
	closeOnce    sync.Once
	closeErr     error
}

// Open creates a new Client from the provided source creators.
// Each creator is built (Create) in order; if any fails, all previously
// created sources are closed and the error is returned.
// At least one creator is required (ErrNoSources); source names must be
// unique (ErrDuplicateSource).
func Open(creators ...SourceCreator) (*Client, error) {
	if len(creators) == 0 {
		return nil, ErrNoSources
	}

	sources := make([]Source, len(creators))
	sourceByName := make(map[string]Source, len(creators))
	for i, c := range creators {
		var err error
		sources[i], err = c.Create()
		if err != nil {
			closeSources(sources[:i])
			return nil, err
		}
		name := sources[i].Name()
		if _, exists := sourceByName[name]; exists {
			closeSources(sources[:i+1])
			return nil, fmt.Errorf("%w: %q", ErrDuplicateSource, name)
		}
		sourceByName[name] = sources[i]
	}

	return &Client{sources: sources, sourceByName: sourceByName}, nil
}

func closeSources(sources []Source) {
	for _, s := range sources {
		_ = s.Close()
	}
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
// If ctx is cancelled, Lookup returns the context error without querying.
func (c *Client) Lookup(ctx context.Context, addr netip.Addr) (*Result, error) {
	addr = addr.Unmap()

	for _, src := range c.sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := src.Lookup(ctx, addr)
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
// If ctx is cancelled, LookupAll stops querying and joins the context error.
func (c *Client) LookupAll(ctx context.Context, addr netip.Addr) ([]*Result, error) {
	addr = addr.Unmap()

	var results []*Result
	errs := make([]error, 0, len(c.sources))
	for _, src := range c.sources {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		result, err := src.Lookup(ctx, addr)
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
// If ctx is cancelled, LookupFrom returns the context error without querying.
func (c *Client) LookupFrom(ctx context.Context, sourceName string, addr netip.Addr) (*Result, error) {
	target := c.sourceByName[sourceName]
	if target == nil {
		return nil, fmt.Errorf("%w: %q", ErrSourceNotConfigured, sourceName)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	addr = addr.Unmap()

	result, err := target.Lookup(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", target.Name(), err)
	}

	return result, nil
}

// Close closes all sources and purges any per-source caches.
// It is safe to call Close multiple times; subsequent calls return the same
// error as the first and do not close sources again. Close must not be called
// concurrently with Lookup or the other query methods.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		errs := make([]error, 0, len(c.sources))
		for _, src := range c.sources {
			errs = append(errs, src.Close())
		}

		c.sources = nil
		c.sourceByName = nil

		c.closeErr = errors.Join(errs...)
	})
	return c.closeErr
}
