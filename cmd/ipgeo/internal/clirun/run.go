package clirun

import (
	"context"
	"errors"
	"io"
	"net/netip"

	"github.com/kibaamor/ipgeo"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/input"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/output"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/sources"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/updater"
	"github.com/kibaamor/ipstream"
)

const streamBufferSize = 32 * 1024

type Options struct {
	Config     *config.Config
	Args       []string
	JSONMode   bool
	SourceName string
	InputFile  string
	OutputFile string
}

func Run(ctx context.Context, opts Options) (runErr error) {
	svc, err := loadSources(ctx, opts.Config, opts.SourceName)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, svc.Close())
	}()

	in, err := input.NewReader(opts.Args, opts.InputFile)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	renderer, closer, err := output.NewRenderer(opts.OutputFile, opts.JSONMode)
	if err != nil {
		return err
	}

	err = streamInput(ctx, in, renderer, svc.Lookup)
	return errors.Join(err, closer.Close())
}

func streamInput(ctx context.Context, in io.Reader, renderer output.Renderer, lookup func(context.Context, netip.Addr) (ipgeo.Result, error)) (runErr error) {
	var streamErr error
	streamer := ipstream.NewStreamer(ipstream.HandleFunc(func(raw []byte, addr netip.Addr) {
		if streamErr != nil {
			return
		}
		streamErr = handleSegment(ctx, renderer, lookup, raw, addr)
	}))
	defer func() {
		runErr = errors.Join(runErr, renderer.Flush())
	}()

	buf := make([]byte, streamBufferSize)
	for {
		n, readErr := in.Read(buf)
		if n > 0 {
			streamer.Write(buf[:n])
			if streamErr != nil {
				return streamErr
			}
			if err := renderer.Flush(); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			streamer.Flush()
			if streamErr != nil {
				return streamErr
			}
			return nil
		}
		if readErr != nil {
			streamer.Flush()
			return errors.Join(readErr, streamErr)
		}
	}
}

func handleSegment(ctx context.Context, renderer output.Renderer, lookup func(context.Context, netip.Addr) (ipgeo.Result, error), raw []byte, addr netip.Addr) error {
	if err := renderer.WriteRaw(raw); err != nil {
		return err
	}
	if !addr.IsValid() {
		return nil
	}
	result, err := lookup(ctx, addr.WithZone(""))
	if err != nil {
		if errors.Is(err, ipgeo.ErrNotFound) {
			return nil
		}
		return err
	}
	return renderer.WriteResult(result)
}

func loadSources(ctx context.Context, cfg *config.Config, sourceName string) (*ipgeo.Client, error) {
	selected, err := sources.Select(cfg.Sources, sourceName)
	if err != nil {
		return nil, err
	}
	if err := updater.EnsureSources(ctx, cfg, selected); err != nil {
		return nil, err
	}

	creators, err := sources.Creators(selected, cfg.SourcePath)
	if err != nil {
		return nil, err
	}
	return ipgeo.Open(creators...)
}
