// Package parity holds every error variable shape `ST1012` reads, each found and each near miss, for the original and
// the port to judge alike.
package parity

import (
	"errors"
	standardErrors "errors"
	"fmt"
)

// Unexported names start err, exported ones Err, and each that does not is found.
var (
	errGood          = errors.New("")
	ErrExported      = errors.New("")
	err              = errors.New("")
	Err              = fmt.Errorf("")
	errorSpelledOut  = errors.New("")
	ErrorSpelledOut  = errors.New("")
	missingPrefix    = errors.New("")
	Exported         = fmt.Errorf("")
	errLowerExported = errors.New("")
	ExportedLower    = errors.New("")
	Ärger            = errors.New("")
	ärger            = errors.New("")
)

// A blank variable's name, "_", does not start err either.
var _ = errors.New("")

// Each name paired with its own value is judged on its own.
var first, errSecond, Third = errors.New(""), errors.New(""), fmt.Errorf("")

// A value of another kind beside an error is left alone, and the error beside it judged.
var mixedError, mixedCount = errors.New(""), 1

// A call of fmt.Errorf wrapping another error is a new error all the same.
var wrapped = fmt.Errorf("%w", errGood)

// A typed declaration is judged as an untyped one.
var typed error = errors.New("")

// An import under another name still calls errors.New.
var renamedImport = standardErrors.New("")

// One call giving every value pairs no name with a value of its own, so neither side judges it.
var pairedFirst, pairedSecond = twoErrors()

func twoErrors() (error, error) { return nil, nil }

// A declaration with no value, a parenthesized call, another constructor and a call through a variable are no call of
// errors.New or fmt.Errorf, so neither side judges them.
var (
	noValue       error
	parenthesized = (errors.New(""))
	joined        = errors.Join(errGood)
	newError      = errors.New
	throughValue  = newError("")
	formatted     = fmt.Sprintf("")
)

// A variable declared in a function is no part of the package's surface, so neither side judges it.
func local() error {
	var inside = errors.New("")
	return inside
}
