// Package shadowed imports fmt under its own name and another, and declares a local fmt, under which the original's
// fix names the local's method.
package shadowed

import (
	"errors"
	"fmt"
	formatting "fmt"
)

type shadow struct{}

func (shadow) Sprintf(format string, arguments ...any) string { return "" }

func shadowed(count int) {
	// Before the local, fmt names the import, and the two fixes are the same.
	_ = errors.New(fmt.Sprintf("%d", count))
	fmt := shadow{}
	// The original writes fmt.Errorf, the local's, which has no Errorf, and the port formatting.Errorf.
	_ = errors.New( // port moves: should use fmt.Errorf(...) instead of errors.New(fmt.Sprintf(...))
		formatting.Sprintf("%d", count))
	// The local's method is not fmt.Sprintf.
	_ = errors.New(fmt.Sprintf("%d", count))
}
