package ipgeo

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"
)

// Client queries one or more geolocation sources.
//
// A Client is safe for concurrent use by multiple goroutines: Lookup, LookupAll,
// LookupFrom, and SourceNames may be called concurrently. Close is safe to call
// multiple times and from concurrent goroutines, but it must not be called
// concurrently with any query method; the result of doing so is undefined, as
// with io.Closer.
type Client struct {
	sources        []Source
	sourceByName   map[string]Source
	cacheEntries   uint
	cacheResultTTL time.Duration
	cacheErrorsTTL time.Duration
	closeOnce      sync.Once
	closeErr       error
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
		return nil, ErrNoSources
	}

	seen := make(map[string]struct{}, len(c.sources))
	for _, src := range c.sources {
		if _, exists := seen[src.Name()]; exists {
			_ = c.Close()
			return nil, fmt.Errorf("%w: %q", ErrDuplicateSource, src.Name())
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
			cached, err := newCachedSource(wrapped, c.cacheEntries, c.cacheResultTTL, c.cacheErrorsTTL)
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
