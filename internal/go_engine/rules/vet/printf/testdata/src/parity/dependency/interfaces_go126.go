//go:build go1.26

package dependency

import "fmt"

// Logger's method is a printf wrapper, since an implementation that is one is assigned to it below, in a file of
// go1.26, so it has a fact parity reads.
type Logger interface {
	Logf(format string, arguments ...any)
}

type printLogger struct{}

func (printLogger) Logf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

var _ Logger = printLogger{}

// NewLogger is the package's logger.
func NewLogger() Logger {
	return printLogger{}
}
