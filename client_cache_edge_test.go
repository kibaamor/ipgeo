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
		t.Fatalf("Close() error: %v", err)
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
	c, err := Open(Wrap(src1), Wrap(src2))
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

func TestOpenCreatorErrorClosesPreviouslyCreatedSources(t *testing.T) {
	sentinelErr := errors.New("creator broken")
	src := newMockSource("db")
	failing := SourceCreator{
		build: func() (Source, error) { return nil, sentinelErr },
	}

	_, err := Open(Wrap(src), failing)
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("Open() error = %v, want sentinelErr", err)
	}
	if !src.closed {
		t.Fatal("source created before failing creator was not closed")
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
