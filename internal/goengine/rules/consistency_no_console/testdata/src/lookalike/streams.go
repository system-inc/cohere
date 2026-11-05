package lookalike

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// A writer handed in stays the caller's choice, whatever the caller hands in.
func emit(writer io.Writer, text string) {
	_, _ = fmt.Fprintln(writer, text)
}

// Standard error given as a value to print, not as the writer, goes where the writer goes.
func describeStream(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, os.Stderr)
}

// A local value with a field named Stdout is not os's variable, however it is spelled.
func capture(text string) string {
	os := struct{ Stdout *bytes.Buffer }{Stdout: &bytes.Buffer{}}
	_, _ = fmt.Fprint(os.Stdout, text)
	return os.Stdout.String()
}
