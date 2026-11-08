package parity

import "errors"

// notAnError is no error, so its Is, As and Unwrap are left alone whatever their signatures.
type notAnError int

func (notAnError) Is(int) int    { return 0 }
func (notAnError) As() string    { return "" }
func (notAnError) Unwrap() error { return nil }

// pointerError implements error only through its pointer, so its methods on the value are left alone and those on
// the pointer are judged.
type pointerError int

func (*pointerError) Error() string { return "" }
func (pointerError) Is() bool       { return false }
func (*pointerError) As() bool      { return false }

// anError implements error, and declares Is, As and Unwrap with the canonical signatures.
type anError int

func (anError) Error() string     { return "" }
func (anError) Is(error) bool     { return false }
func (anError) As(any) bool       { return false }
func (anError) Unwrap() []error   { return nil }
func (*anError) Unwrapped() error { return nil }

// spelledError writes any as the empty interface, which matches it.
type spelledError int

func (spelledError) Error() string         { return "" }
func (spelledError) As(interface{}) bool   { return false }
func (spelledError) Unwrap() error         { return nil }
func (spelledError) Is(err error) (b bool) { return false }

// badError implements error, and declares each of Is and As wrong in another way.
type badError int

func (badError) Error() string            { return "" }
func (badError) Is(error) error           { return nil }
func (badError) As(interface{ M() }) bool { return false } // an interface that is not the empty one
func (badError) Unwrap() (error, bool)    { return nil, false }
func (*badError) Is2(error) error         { return nil }

// Each of these declares Unwrap wrong in another way: a parameter, a result of another type, and no result.
type unwrapParameter int

func (unwrapParameter) Error() string      { return "" }
func (unwrapParameter) Unwrap(error) error { return nil }

type unwrapPointer int

func (unwrapPointer) Error() string            { return "" }
func (unwrapPointer) Unwrap() *unwrapParameter { return nil }

type unwrapSlice int

func (unwrapSlice) Error() string         { return "" }
func (unwrapSlice) Unwrap() []interface{} { return nil }

// embeddedError implements error through an embedded field, and its Unwrap is judged.
type embeddedError struct{ error }

func (embeddedError) Unwrap() int { return 0 }

// wrappedError is a struct embedding a pointer to an error type, and implements error by its promoted method.
type wrappedError struct{ *pointerError }

func (wrappedError) Is(target error) bool { return errors.Is(target, nil) }
func (wrappedError) As(target int) bool   { return false }
