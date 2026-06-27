package downloader

import (
	"os"

	"github.com/mattn/go-isatty"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var isTTY = isatty.IsTerminal(os.Stderr.Fd())

type ProgressGroup struct {
	p *mpb.Progress
}

func NewProgressGroup() *ProgressGroup {
	if !isTTY {
		return &ProgressGroup{}
	}
	return &ProgressGroup{
		p: mpb.New(
			mpb.WithOutput(os.Stderr),
			mpb.WithWidth(64),
		),
	}
}

func (pg *ProgressGroup) AddBar(name string) *ProgressBar {
	if pg.p == nil {
		return &ProgressBar{name: name}
	}
	bar := pg.p.MustAdd(0,
		mpb.BarStyle().Lbound("[").Filler("=").Tip(">").Padding("-").Rbound("]").Build(),
		mpb.PrependDecorators(
			decor.Name(name, decor.WCSyncSpace),
		),
		mpb.AppendDecorators(
			decor.CountersKibiByte(" %.1f / %.1f"),
			decor.AverageSpeed(decor.SizeB1024(0), " %.1f"),
			decor.OnComplete(
				decor.AverageETA(decor.ET_STYLE_GO),
				" done",
			),
		),
	)
	return &ProgressBar{bar: bar, name: name}
}

func (pg *ProgressGroup) Wait() {
	if pg.p != nil {
		pg.p.Wait()
	}
}

type ProgressBar struct {
	bar  *mpb.Bar
	name string
}

func (pb *ProgressBar) SetTotal(total int64) {
	if pb.bar != nil {
		pb.bar.SetTotal(total, false)
	}
}

func (pb *ProgressBar) SetCurrent(n int64) {
	if pb.bar != nil {
		pb.bar.SetCurrent(n)
	}
}

func (pb *ProgressBar) MarkDone(size int64) {
	if pb.bar != nil {
		pb.bar.SetTotal(size, true)
		pb.bar.SetCurrent(size)
	}
}

func (pb *ProgressBar) Abort() {
	if pb.bar != nil {
		pb.bar.Abort(true)
	}
}