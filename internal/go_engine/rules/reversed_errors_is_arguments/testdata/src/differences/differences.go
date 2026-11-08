// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns both
// errors, and errors.Is held in a variable, whose callee is known only once the variable is lifted, or not at all
// when two functions meet. The port and the original find the same here, so the fixture records no difference.
package differences

import (
	"errors"
	"io/fs"
)

func targetAndError() (error, error) {
	return fs.ErrNotExist, nil
}

func statements(local error) {
	defer errors.Is(fs.ErrNotExist, local)
	go errors.Is(fs.ErrExist, local)
	defer func() {
		_ = errors.Is(fs.ErrPermission, local)
	}()
}

func multipleValues() {
	_ = errors.Is(targetAndError())
}

func lifted(condition bool, local error) {
	is := errors.Is
	if condition {
		is = errors.Is
	}
	_ = is(fs.ErrNotExist, local)
	choose := errors.Is
	if condition {
		choose = func(error, error) bool { return false }
	}
	_ = choose(fs.ErrNotExist, local)
}
