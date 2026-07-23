// Package ipgeo resolves IP addresses with one or more geolocation sources.
//
// Open a [Client] with one or more source options, then query it with Lookup,
// LookupAll, or LookupFrom. Each query method accepts a [context.Context]; a
// cancelled context short-circuits the query.
//
// A Client is safe for concurrent use. Close is idempotent and safe from
// concurrent goroutines, but must not be called concurrently with a query.
//
// Open and LookupFrom return sentinel errors (ErrNoSources, ErrDuplicateSource,
// ErrSourceNotConfigured) for use with errors.Is.
package ipgeo
