package ipgeo

import (
	"errors"
	"fmt"
	"time"
)

// Option configures a Client during Open.
type Option func(*Client) error

// WithMMDB adds a MaxMind DB source. companionPath is optional and supplements
// missing fields (e.g. ASN data from a separate DB file); pass "" to omit.
func WithMMDB(name, path, companionPath string) Option {
	return func(c *Client) error {
		db, err := openMMDB(name, path, companionPath)
		if err != nil {
			return fmt.Errorf("open mmdb %s: %w", name, err)
		}
		c.sources = append(c.sources, db)
		return nil
	}
}

// WithIPDB adds an IPIP.net IPDB source.
func WithIPDB(name, path string) Option {
	return func(c *Client) error {
		db, err := openIPDB(name, path)
		if err != nil {
			return fmt.Errorf("open ipdb %s: %w", name, err)
		}
		c.sources = append(c.sources, db)
		return nil
	}
}

// WithXDB adds an ip2region XDB source. v4Path and v6Path may each be empty,
// but at least one must be provided.
func WithXDB(name, v4Path, v6Path string) Option {
	return func(c *Client) error {
		db, err := openXDB(name, v4Path, v6Path)
		if err != nil {
			return fmt.Errorf("open xdb %s: %w", name, err)
		}
		c.sources = append(c.sources, db)
		return nil
	}
}

// WithIP2Location adds an IP2Location BIN database source.
func WithIP2Location(name, path string) Option {
	return func(c *Client) error {
		db, err := openIP2Location(name, path)
		if err != nil {
			return fmt.Errorf("open ip2location %s: %w", name, err)
		}
		c.sources = append(c.sources, db)
		return nil
	}
}

// WithSource adds a custom Source implementation.
func WithSource(src Source) Option {
	return func(c *Client) error {
		if src == nil {
			return errors.New("WithSource: src must not be nil")
		}
		c.sources = append(c.sources, src)
		return nil
	}
}

// WithCache enables a per-source LRU cache. Results are cached with a sliding
// resultTTL (0 = permanent); errors are cached with a fixed errorTTL
// (0 = disabled). Context errors are never cached. Pass maxEntries=0 to disable
// caching entirely.
func WithCache(maxEntries uint, resultTTL, errorTTL time.Duration) Option {
	return func(c *Client) error {
		if resultTTL < 0 {
			return fmt.Errorf("WithCache: resultTTL must not be negative, got %s", resultTTL)
		}
		if errorTTL < 0 {
			return fmt.Errorf("WithCache: errorTTL must not be negative, got %s", errorTTL)
		}
		c.cacheEntries = maxEntries
		c.cacheResultTTL = resultTTL
		c.cacheErrorsTTL = errorTTL
		return nil
	}
}
