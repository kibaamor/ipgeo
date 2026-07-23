// Package ipgeo resolves IP addresses with one or more geolocation sources.
//
// Build a source with MMDB, IPDB, XDB, IP2Location, or Wrap, optionally
// decorate it with Singleflight and Cache, then open a [Client] with one or
// more source creators. Query the client with Lookup, LookupAll, or
// LookupFrom. Each query method accepts a [context.Context]; a cancelled
// context short-circuits the query.
//
// A Client is safe for concurrent use. Close is idempotent and safe from
// concurrent goroutines, but must not be called concurrently with a query.
//
// Open and LookupFrom return sentinel errors (ErrNoSources,
// ErrDuplicateSource, ErrSourceNotConfigured) for use with errors.Is.
package ipgeo
