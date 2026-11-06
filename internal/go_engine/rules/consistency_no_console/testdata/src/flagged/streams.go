package flagged

import (
	"fmt"
	"os"
	system "os"
)

// fmt's writes to a writer, handed the process's own standard output or error, are fmt.Println spelled longer.
func warn(message string, count int) {
	_, _ = fmt.Fprintln(os.Stderr, message)        // want `Do not call 'fmt\.Fprintln\(os\.Stderr, \.\.\.\)'`
	_, _ = fmt.Fprintf(os.Stdout, "%d\n", count)   // want `Do not call 'fmt\.Fprintf\(os\.Stdout, \.\.\.\)'`
	_, _ = fmt.Fprint((os.Stderr), message)        // want `Do not call 'fmt\.Fprint\(os\.Stderr, \.\.\.\)'`
	_, _ = fmt.Fprintln(system.Stdout, "renamed")  // want `Do not call 'fmt\.Fprintln\(os\.Stdout, \.\.\.\)'`
	_, _ = (fmt.Fprintln)(os.Stderr, "wrapped", 1) // want `Do not call 'fmt\.Fprintln\(os\.Stderr, \.\.\.\)'`
}
