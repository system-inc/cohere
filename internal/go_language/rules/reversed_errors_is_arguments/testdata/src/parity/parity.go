// Package parity holds errors of each origin on each side of errors.Is: globals of the standard library, of another
// fixture package and of its own, locals, values built at the call, a global kept in a local, read through a phi, a
// pointer, a field or an index, and the shapes staticcheck's representation builds the call in more than one way.
package parity

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"

	"elsewhere"
)

var errOwn = errors.New("own")

var errOwnOther = errors.New("own other")

type customError struct{}

func (customError) Error() string { return "" }

var checked = errors.Is(fs.ErrNotExist, errOwn)

func init() {
	_ = errors.Is(io.EOF, errOwn)
}

func origins(local error) {
	_ = errors.Is(local, fs.ErrNotExist)
	_ = errors.Is(fs.ErrNotExist, local)
	_ = errors.Is(local, elsewhere.ErrMissing)
	_ = errors.Is(elsewhere.ErrMissing, local)
	_ = errors.Is(local, errOwn)
	_ = errors.Is(errOwn, local)
	_ = errors.Is(errOwn, errOwnOther)
	_ = errors.Is(errOwn, fs.ErrNotExist)
	_ = errors.Is(fs.ErrNotExist, errOwn)
	_ = errors.Is(io.EOF, fs.ErrNotExist)
	_ = errors.Is(elsewhere.ErrMissing, elsewhere.ErrOther)
	_ = errors.Is(elsewhere.ErrMissing, io.EOF)
	_ = errors.Is(context.Canceled, errors.New("built"))
	_ = errors.Is(context.DeadlineExceeded, customError{})
	_ = errors.Is(os.ErrNotExist, &customError{})
	_ = errors.Is(os.ErrNotExist, elsewhere.Make())
	_ = errors.Is(elsewhere.Make(), os.ErrNotExist)
	_ = errors.Is(os.ErrNotExist, nil)
	_ = errors.Is(nil, os.ErrNotExist)
	_ = errors.Is(local, local)
}

func shapes(local error, condition bool) {
	kept := fs.ErrNotExist
	_ = errors.Is(kept, local)
	var chosen error = fs.ErrNotExist
	if condition {
		chosen = io.EOF
	}
	_ = errors.Is(chosen, local)
	agreeing := fs.ErrNotExist
	if condition {
		agreeing = fs.ErrNotExist
	}
	_ = errors.Is(agreeing, local)
	pointer := &fs.ErrNotExist
	_ = errors.Is(*pointer, local)
	_ = errors.Is(elsewhere.Holder.Err, local)
	_ = errors.Is(elsewhere.Errors[0], local)
	_ = errors.Is(error(fs.ErrNotExist), local)
	_ = errors.Is(fs.ErrNotExist, error(local))
}

func callShapes(local error) {
	defer errors.Is(fs.ErrNotExist, local)
	go errors.Is(io.EOF, local)
	func() {
		_ = errors.Is(elsewhere.ErrMissing, local)
	}()
	is := errors.Is
	_ = is(fs.ErrNotExist, local)
	_ = errors.Is(
		fs.ErrNotExist,
		local,
	)
	if errors.Is(io.ErrUnexpectedEOF, local) || errors.Is(local, io.ErrUnexpectedEOF) {
		return
	}
	_ = checked
}

func generic[T any](value T, local error) T {
	_ = errors.Is(fs.ErrNotExist, local)
	return value
}

func instantiated() {
	generic(1, nil)
}
