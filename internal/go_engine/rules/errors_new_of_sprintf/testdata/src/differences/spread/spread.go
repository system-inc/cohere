// Package spread imports fmt under its own name, so the original's fix names fmt.Errorf rightly, and differs only
// where it drops a spread's ellipsis, which type-checks and formats the list as one value.
package spread

import (
	"errors"
	"fmt"
)

func spread(format string, list []any, count int) {
	// The original writes fmt.Errorf(format, list), and the port fmt.Errorf(format, list...).
	_ = errors.New( // port moves: should use fmt.Errorf(...) instead of errors.New(fmt.Sprintf(...))
		fmt.Sprintf(format, list...))
	// The original writes fmt.Errorf(format, append([]any{count}, list...)), and the port keeps the outer spread.
	_ = errors.New( // port moves: should use fmt.Errorf(...) instead of errors.New(fmt.Sprintf(...))
		(fmt.Sprintf)(format, append([]any{count}, list...)...))
	// Without a spread, the two fixes are the same.
	_ = errors.New(fmt.Sprintf(format, count))
	// errors.New of a string the code holds is no finding, and keeps errors imported once every fix is applied.
	_ = errors.New(format)
}
