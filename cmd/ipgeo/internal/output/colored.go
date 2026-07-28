package output

import (
	"io"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"

	"github.com/kibaamor/ipgeo"
)

type coloredFormatter struct {
	dim *color.Color
}

func newColoredFormatter(out io.Writer) *coloredFormatter {
	dim := color.New(color.Faint)

	if !isTerminalWriter(out) {
		dim.DisableColor()
	}

	return &coloredFormatter{dim: dim}
}

func isTerminalWriter(w io.Writer) bool {
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}
	return false
}

func (f *coloredFormatter) formatAnnotation(result ipgeo.Result) string {
	s := result.String()
	if s == "" {
		return ""
	}
	return f.dim.Sprint("[" + s + "]")
}
