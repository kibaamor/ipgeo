package ipgeo

import (
	"context"
	"errors"
	"testing"
)

func TestOpen_ErrNoSources(t *testing.T) {
	_, err := Open()
	if !errors.Is(err, ErrNoSources) {
		t.Fatalf("Open() error = %v, want ErrNoSources", err)
	}
}

func TestOpen_ErrDuplicateSource(t *testing.T) {
	src := newMockSource("db")
	_, err := Open(Wrap(src), Wrap(src))
	if !errors.Is(err, ErrDuplicateSource) {
		t.Fatalf("Open() error = %v, want ErrDuplicateSource", err)
	}
}

func TestLookupFrom_ErrSourceNotConfigured(t *testing.T) {
	c := mustOpen(t, Wrap(newMockSource("db")))

	_, err := c.LookupFrom(context.Background(), "nonexistent", testAddr)
	if !errors.Is(err, ErrSourceNotConfigured) {
		t.Fatalf("LookupFrom() error = %v, want ErrSourceNotConfigured", err)
	}
}

func TestLookupFrom_WrapsSourceError(t *testing.T) {
	src := newMockSource("db")
	sentinelErr := errors.New("db broken")
	src.addErr(sentinelErr)
	c := mustOpen(t, Wrap(src))

	_, err := c.LookupFrom(context.Background(), "db", testAddr)
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("LookupFrom() error = %v, want to wrap sentinelErr", err)
	}
}
