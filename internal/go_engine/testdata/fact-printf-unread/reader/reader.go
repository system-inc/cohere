// Package reader declares a variable, a field and an interface method of a printf function's shape, which nothing
// assigns a literal and nothing implements, so none is a wrapper in any build, and Target only takes each as a value,
// which printf never looks a fact up for. In a file of go1.26 or later printf gathers the interface's method, and finds
// it no wrapper. part_windows.go is there so windows is a build of its own.
package reader

import "example.com/printfunread/values"

// Logf logs args by format.
var Logf func(format string, args ...values.Value)

// Logger logs by its Logf.
type Logger struct {
	// Logf logs args by format.
	Logf func(format string, args ...values.Value)
}

// Printer prints args by format.
type Printer interface {
	Logf(format string, args ...values.Value)
}

// Target takes Logf, a Logger's Logf and a Printer's Logf as values and calls none of them.
func Target(logger Logger, printer Printer) {
	_ = Logf
	_ = logger.Logf
	_ = printer.Logf
}
