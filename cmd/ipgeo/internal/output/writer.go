package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/kibaamor/ipgeo"
)

type Renderer interface {
	WriteRaw(raw []byte) error
	WriteResult(result ipgeo.Result) error
	Flush() error
}

type stdoutWriteCloser struct{ *os.File }

func (stdoutWriteCloser) Close() error { return nil }

func NewRenderer(outputFile string, structured bool) (Renderer, io.Closer, error) {
	out, err := openDestination(outputFile)
	if err != nil {
		return nil, nil, err
	}
	if structured {
		return NewStructuredRenderer(out), out, nil
	}
	return NewInlineRenderer(out), out, nil
}

func openDestination(outputFile string) (io.WriteCloser, error) {
	if outputFile == "" {
		return stdoutWriteCloser{os.Stdout}, nil
	}

	file, err := os.Create(outputFile)
	if err != nil {
		return nil, fmt.Errorf("open output file: %w", err)
	}
	return file, nil
}

// InlineRenderer annotates IP addresses within each input line.
type InlineRenderer struct {
	formatter *coloredFormatter
	out       *bufio.Writer
}

// NewInlineRenderer creates an InlineRenderer using the given output destination.
func NewInlineRenderer(out io.Writer) *InlineRenderer {
	return &InlineRenderer{formatter: newColoredFormatter(out), out: bufio.NewWriter(out)}
}

// WriteRaw writes each raw stream segment.
func (w *InlineRenderer) WriteRaw(raw []byte) error {
	if _, err := w.out.Write(raw); err != nil {
		return err
	}
	return nil
}

// WriteResult annotates recognized IP addresses.
func (w *InlineRenderer) WriteResult(result ipgeo.Result) error {
	if ann := w.formatter.formatAnnotation(result); ann != "" {
		if _, err := fmt.Fprint(w.out, " ", ann); err != nil {
			return err
		}
	}
	return nil
}

func (w *InlineRenderer) Flush() error { return w.out.Flush() }

// StructuredRenderer writes one JSON object per matched IP.
type StructuredRenderer struct {
	out *bufio.Writer
}

// NewStructuredRenderer creates a StructuredRenderer using the given output destination.
func NewStructuredRenderer(out io.Writer) *StructuredRenderer {
	return &StructuredRenderer{out: bufio.NewWriter(out)}
}

// WriteRaw ignores raw stream segments because structured output only emits matched results.
func (w *StructuredRenderer) WriteRaw([]byte) error { return nil }

// WriteResult writes one formatted lookup result.
// Empty results are suppressed to mirror InlineRenderer's behavior.
func (w *StructuredRenderer) WriteResult(result ipgeo.Result) error {
	if result.IsEmpty() {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if _, err := w.out.Write(data); err != nil {
		return err
	}
	return w.out.WriteByte('\n')
}

func (w *StructuredRenderer) Flush() error { return w.out.Flush() }
