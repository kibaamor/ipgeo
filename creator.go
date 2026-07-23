package ipgeo

import (
	"errors"
	"fmt"
	"time"
)

// SourceDecorator wraps a Source, returning the decorated Source.
// A nil error is returned on success; errors propagate through Create.
type SourceDecorator func(Source) (Source, error)

// SourceCreator configures and constructs a decorated Source.
// Create one via MMDB, IPDB, XDB, IP2Location, or Wrap, then apply
// decorators with Decorate. Pass the SourceCreator directly to Open,
// or call Create to build the Source manually.
type SourceCreator struct {
	name       string
	build      func() (Source, error)
	decorators []SourceDecorator
}

// MMDB returns a SourceCreator for a MaxMind DB source.
// companionPath is optional; pass "" to omit.
func MMDB(name, path, companionPath string) SourceCreator {
	return SourceCreator{
		name: name,
		build: func() (Source, error) {
			src, err := openMMDB(name, path, companionPath)
			if err != nil {
				return nil, fmt.Errorf("open mmdb %s: %w", name, err)
			}
			return src, nil
		},
	}
}

// IPDB returns a SourceCreator for an IPIP.net IPDB source.
func IPDB(name, path string) SourceCreator {
	return SourceCreator{
		name: name,
		build: func() (Source, error) {
			src, err := openIPDB(name, path)
			if err != nil {
				return nil, fmt.Errorf("open ipdb %s: %w", name, err)
			}
			return src, nil
		},
	}
}

// XDB returns a SourceCreator for an ip2region XDB source.
// v4Path and v6Path may each be empty, but at least one must be provided.
func XDB(name, v4Path, v6Path string) SourceCreator {
	return SourceCreator{
		name: name,
		build: func() (Source, error) {
			src, err := openXDB(name, v4Path, v6Path)
			if err != nil {
				return nil, fmt.Errorf("open xdb %s: %w", name, err)
			}
			return src, nil
		},
	}
}

// IP2Location returns a SourceCreator for an IP2Location BIN database source.
func IP2Location(name, path string) SourceCreator {
	return SourceCreator{
		name: name,
		build: func() (Source, error) {
			src, err := openIP2Location(name, path)
			if err != nil {
				return nil, fmt.Errorf("open ip2location %s: %w", name, err)
			}
			return src, nil
		},
	}
}

// Wrap returns a SourceCreator for an existing Source, allowing it to be
// decorated and included in a Client.
func Wrap(src Source) SourceCreator {
	var name string
	if src != nil {
		name = src.Name()
	}
	return SourceCreator{
		name: name,
		build: func() (Source, error) {
			if src == nil {
				return nil, errors.New("Wrap: src must not be nil")
			}
			return src, nil
		},
	}
}

// Decorate appends a decorator to the creator. Decorators are applied in
// the order they are added: the first added is the innermost wrapper,
// the last added is the outermost.
func (c SourceCreator) Decorate(d SourceDecorator) SourceCreator {
	c.decorators = append(c.decorators, d)
	return c
}

// Create builds the source by opening the database file (or retrieving the
// wrapped source) and applying decorators in order. If a decorator fails,
// the source built so far is closed.
func (c SourceCreator) Create() (Source, error) {
	if c.build == nil {
		return nil, errors.New("ipgeo: SourceCreator has no build function")
	}
	src, err := c.build()
	if err != nil {
		return nil, err
	}
	for _, d := range c.decorators {
		decorated, err := d(src)
		if err != nil {
			_ = src.Close()
			return nil, err
		}
		src = decorated
	}
	return src, nil
}

// Singleflight returns a SourceDecorator that wraps a source with
// singleflight to deduplicate concurrent Lookup calls for the same address.
func Singleflight() SourceDecorator {
	return func(src Source) (Source, error) {
		return newSingleflightSource(src), nil
	}
}

// Cache returns a SourceDecorator that wraps a source with a TTL cache.
// maxEntries must be positive; resultTTL must not be negative (0 = permanent);
// errorTTL must not be negative (0 = errors not cached). Context errors are
// never cached.
func Cache(maxEntries uint, resultTTL, errorTTL time.Duration) SourceDecorator {
	return func(src Source) (Source, error) {
		return newCachedSource(src, maxEntries, resultTTL, errorTTL)
	}
}
