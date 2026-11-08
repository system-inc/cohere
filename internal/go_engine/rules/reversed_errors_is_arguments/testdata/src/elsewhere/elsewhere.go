// Package elsewhere declares errors as globals, so a package of the fixture reads another package's global that is
// not the standard library's.
package elsewhere

import "errors"

var ErrMissing = errors.New("missing")

var ErrOther = errors.New("other")

var Holder = struct{ Err error }{Err: errors.New("held")}

var Errors = []error{errors.New("listed")}

func Make() error { return ErrMissing }
