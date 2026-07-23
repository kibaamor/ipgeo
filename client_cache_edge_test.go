package ipgeo

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestNewCachedSourceRejectsZeroSize(t *testing.T) {
	_, err := newCachedSource(newMockSource("db"), 0, 0, 0)
	if err == nil {
		t.Fatalf("newCachedSource(size=0) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "create cache") {
		t.Fatalf("newCachedSource(size=0) error = %v, want create cache error", err)
	}
}

func TestWrapSourcesWithoutCachePreservesOrderAndSkipsCache(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	c := &Client{sources: []Source{src1, src2}}

	if err := c.wrapSources(); err != nil {
		t.Fatalf("wrapSources() error: %v", err)
	}

	if got := c.SourceNames(); len(got) != 2 || got[0] != "db1" || got[1] != "db2" {
		t.Fatalf("SourceNames() = %v, want [db1 db2]", got)
	}
	for i, src := range c.sources {
		if _, ok := src.(*cachedSource); ok {
			t.Fatalf("source %d = %T, want cache skipped", i, src)
		}
		sf, ok := src.(*singleflightSource)
		if !ok {
			t.Fatalf("source %d = %T, want *singleflightSource", i, src)
		}
		want := []Source{src1, src2}[i]
		if sf.source != want {
			t.Fatalf("source %d wraps %p, want %p", i, sf.source, want)
		}
	}
}

func TestWrapSourcesWithCachePreservesOrder(t *testing.T) {
	src1 := newMockSource("db1")
	src2 := newMockSource("db2")
	c := &Client{sources: []Source{src1, src2}, cacheEntries: 2, cacheErrorsTTL: time.Second}

	if err := c.wrapSources(); err != nil {
		t.Fatalf("wrapSources() error: %v", err)
	}

	if got := c.SourceNames(); len(got) != 2 || got[0] != "db1" || got[1] != "db2" {
		t.Fatalf("SourceNames() = %v, want [db1 db2]", got)
	}
	for i, src := range c.sources {
		cached, ok := src.(*cachedSource)
		if !ok {
			t.Fatalf("source %d = %T, want *cachedSource", i, src)
		}
		sf, ok := cached.source.(*singleflightSource)
		if !ok {
			t.Fatalf("cached source %d wraps %T, want *singleflightSource", i, cached.source)
		}
		want := []Source{src1, src2}[i]
		if sf.source != want {
			t.Fatalf("source %d wraps %p, want %p", i, sf.source, want)
		}
	}
}

func TestSingleflightSourceUnexpectedSharedType(t *testing.T) {
	sf := newSingleflightSource(newMockSource("db"))
	addr := netip.MustParseAddr("1.2.3.4")
	inFlight := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})

	go func() {
		defer close(firstDone)
		_, _, _ = sf.group.Do(addr.String(), func() (any, error) {
			close(inFlight)
			<-release
			return "not a result", nil
		})
	}()
	<-inFlight

	timer := time.AfterFunc(50*time.Millisecond, func() { close(release) })
	_, err := sf.Lookup(context.Background(), addr)
	if !timer.Stop() {
		<-firstDone
	} else {
		close(release)
		<-firstDone
	}
	if err == nil {
		t.Fatal("Lookup() error = nil, want unexpected type error")
	}
	if !strings.Contains(err.Error(), "unexpected type string") {
		t.Fatalf("Lookup() error = %v, want unexpected type string", err)
	}
}

func TestSingleflightSourcePropagatesLookupAndCloseErrors(t *testing.T) {
	lookupErr := errors.New("lookup broken")
	src := newMockSource("db")
	src.addErr(lookupErr)
	sf := newSingleflightSource(src)

	_, err := sf.Lookup(context.Background(), testAddr)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("Lookup() error = %v, want lookupErr", err)
	}
	if err := sf.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !src.closed {
		t.Fatal("wrapped source was not closed")
	}
}

func TestClientCloseReturnsJoinedErrorsAndClearsSources(t *testing.T) {
	err1 := errors.New("close db1")
	err2 := errors.New("close db2")
	src1 := &closeErrorSource{mockSource: newMockSource("db1"), closeErr: err1}
	src2 := &closeErrorSource{mockSource: newMockSource("db2"), closeErr: err2}
	c, err := Open(WithSource(src1), WithSource(src2))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}

	err = c.Close()
	if !errors.Is(err, err1) || !errors.Is(err, err2) {
		t.Fatalf("Close() error = %v, want both close errors", err)
	}
	if c.sources != nil {
		t.Fatalf("sources = %#v, want nil after Close()", c.sources)
	}
	if c.sourceByName != nil {
		t.Fatalf("sourceByName = %#v, want nil after Close()", c.sourceByName)
	}
	if !src1.closed || !src2.closed {
		t.Fatalf("closed flags = %v/%v, want both true", src1.closed, src2.closed)
	}
}

func TestOpenOptionErrorClosesPreviouslyAddedSources(t *testing.T) {
	sentinelErr := errors.New("option broken")
	src := newMockSource("db")

	_, err := Open(WithSource(src), func(*Client) error { return sentinelErr })
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("Open() error = %v, want sentinelErr", err)
	}
	if !src.closed {
		t.Fatal("source added before failing option was not closed")
	}
}

type closeErrorSource struct {
	*mockSource
	closeErr error
}

func (s *closeErrorSource) Close() error {
	s.closed = true
	return s.closeErr
}
