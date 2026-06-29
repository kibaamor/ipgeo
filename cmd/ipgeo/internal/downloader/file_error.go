package downloader

import (
	"fmt"
	"strings"
)

type FileError struct {
	Name string
	Path string
	Err  error
}

func (e FileError) Error() string {
	return fmt.Sprintf("%s: %v", e.Name, e.Err)
}

type FileErrors []FileError

func (e FileErrors) Error() string {
	if len(e) == 0 {
		return "no file errors"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d file(s) failed:", len(e))
	for _, fe := range e {
		fmt.Fprintf(&b, "\n  %s: %v", fe.Name, fe.Err)
	}
	return b.String()
}

func (e FileErrors) Unwrap() []error {
	errs := make([]error, len(e))
	for i, fe := range e {
		errs[i] = fe.Err
	}
	return errs
}
