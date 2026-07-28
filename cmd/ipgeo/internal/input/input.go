package input

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chzyer/readline"
	"github.com/mattn/go-isatty"
)

type readlineReader struct {
	rl  *readline.Instance
	buf []byte
}

func newReadlineReader() (*readlineReader, error) {
	rl, err := readline.New("> ")
	if err != nil {
		return nil, err
	}
	return &readlineReader{rl: rl}, nil
}

func (r *readlineReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		line, err := r.rl.Readline()
		if err != nil {
			if errors.Is(err, readline.ErrInterrupt) || errors.Is(err, io.EOF) {
				return 0, io.EOF
			}
			return 0, err
		}
		r.buf = []byte(line + "\n")
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *readlineReader) Close() error { return r.rl.Close() }

// NewReader turns every CLI input source into a single stream.
// Command arguments take precedence over --input.
func NewReader(args []string, path string) (io.ReadCloser, error) {
	if len(args) > 0 {
		return io.NopCloser(strings.NewReader(strings.Join(args, "\n") + "\n")), nil
	}

	if path == "" {
		if isatty.IsTerminal(os.Stdin.Fd()) {
			return newReadlineReader()
		}
		return io.NopCloser(os.Stdin), nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open input file: %w", err)
	}

	return f, nil
}
