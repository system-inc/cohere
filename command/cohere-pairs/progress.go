package main

import (
	"fmt"
	"io"
)

// progress is where the command says what it is doing, a line at a time, as it does it.
type progress struct {
	writer io.Writer
}

// say writes one formatted message.
func (progress progress) say(format string, arguments ...any) {
	// Ignored: what the command measured is in its files and its exit code, which a closed terminal does not change.
	_, _ = fmt.Fprintf(progress.writer, format, arguments...)
}
