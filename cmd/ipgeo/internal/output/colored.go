package output

import (
	"io"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"

	"github.com/kibaamor/ipgeo"
)

// coloredFormatter renders inline annotations with ANSI terminal colors.
// Colors are automatically disabled when the output is not a terminal.
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

// isTerminalWriter reports whether w is a terminal file descriptor.
func isTerminalWriter(w io.Writer) bool {
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}
	return false
}

// formatAnnotation renders a result as a dim bracketed string.
// Returns an empty string for empty results.
func (f *coloredFormatter) formatAnnotation(result *ipgeo.Result) string {
	if result == nil {
		return ""
	}
	s := result.String()
	if s == "" {
		return ""
	}
	return f.dim.Sprint("[" + s + "]")
}
