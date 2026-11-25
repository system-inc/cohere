// Package parity holds every branch of errorsas's judgment, a clean case beside each, and the shapes most likely to
// tell the port from the original: how errors.As is named at the call, which calls are not calls of it at all, and
// targets reached through aliases, named types, type parameters, embedding, closures and initializers. Nothing here
// is expected to differ; the differential holds the two to the same findings.
package parity

import (
	"errors"
	stderrors "errors"
	"io/fs"
)

// valueError implements error with a value receiver, so both it and a pointer to it do.
type valueError struct{}

func (valueError) Error() string { return "" }

// pointerError implements error only through a pointer.
type pointerError struct{}

func (*pointerError) Error() string { return "" }

// errorAlias is error under another name, so a pointer to it is a *error.
type errorAlias = error

// namedError is a new interface type with error's methods, not error itself.
type namedError error

// empty is a named empty interface, whose underlying type is any's.
type empty interface{}

// anyAlias is any under another name.
type anyAlias = any

// methods is an interface error does not satisfy.
type methods interface{ method() }

// pointerToValueError is a named pointer type, judged by its underlying pointer.
type pointerToValueError *valueError

// pointerToErrorType is a named pointer to error, judged by its underlying *error.
type pointerToErrorType *error

// embedsError is a struct implementing error through an embedded field.
type embedsError struct{ error }

// embedsPointerError implements error through an embedded pointer to pointerError.
type embedsPointerError struct{ *pointerError }

// embedsPointerErrorValue embeds pointerError by value, so only a pointer to it implements error.
type embedsPointerErrorValue struct{ pointerError }

// holder has fields of each kind, so a target can be a field reached through a value and through a pointer.
type holder struct {
	value   valueError
	pointer *pointerError
	err     error
	inner   *holder
}

// generic implements error for every type argument.
type generic[T any] struct{ value T }

func (generic[T]) Error() string { return "" }

// pair returns two results, so a call can spread them into errors.As's two parameters.
func pair() (error, any) { return nil, nil }

func pointerToError() *error { return nil }

func pointerToNothing() *int { return nil }

// asValue holds errors.As in a variable, so a call through it is not a call of errors.As by typeutil.Callee.
var asValue = errors.As

// Every message, on each branch of the judgment, beside an allowed target of the same kind.
func branches(err error) {
	var (
		value      valueError
		pointer    pointerError
		pointerTo  *pointerError
		number     int
		face       methods
		anything   any
		emptyValue empty
		aliased    errorAlias
		named      namedError
	)
	// Any, by name, by alias, named, and as its literal spelling: always allowed.
	errors.As(err, anything)
	errors.As(err, anyAlias(nil))
	errors.As(err, emptyValue)
	errors.As(err, interface{}(nil))

	// Not a pointer.
	errors.As(err, nil)
	errors.As(err, value)
	errors.As(err, pointer)
	errors.As(err, number)
	errors.As(err, face)
	errors.As(err, err)
	errors.As(err, "text")
	errors.As(err, 3)
	errors.As(err, []error{})
	errors.As(err, map[string]error{})
	errors.As(err, func() {})
	errors.As(err, make(chan error))

	// A pointer to error, by name, by alias, through a named pointer type, a conversion, new and a call.
	errors.As(err, &err)
	errors.As(err, &aliased)
	errors.As(err, pointerToErrorType(nil))
	errors.As(err, (*error)(nil))
	errors.As(err, new(error))
	errors.As(err, pointerToError())

	// A pointer to a type that implements error, or to an interface: allowed, nil or not.
	errors.As(err, &value)
	errors.As(err, &pointerTo)
	errors.As(err, &face)
	errors.As(err, &anything)
	errors.As(err, &named)
	errors.As(err, &emptyValue)
	errors.As(err, (*valueError)(nil))
	errors.As(err, pointerToValueError(nil))
	errors.As(err, new(*fs.PathError))
	errors.As(err, &embedsError{})
	errors.As(err, &embedsPointerError{})
	errors.As(err, &generic[int]{})
	errors.As(err, new(*valueError))

	// A pointer to a type that neither implements error nor is an interface, among them a type whose pointer alone
	// implements error, as the original's documentation warns of.
	errors.As(err, &number)
	errors.As(err, &pointer)
	errors.As(err, pointerToNothing())
	errors.As(err, &struct{}{})
	errors.As(err, new(fs.PathError))
	errors.As(err, &embedsPointerErrorValue{})
	errors.As(err, new(**valueError))
	errors.As(err, new(**pointerError))
	errors.As(err, new(*error))
	errors.As(err, &[]error{})
}

// Fields reached through a value and through a pointer, and an index into a slice of targets.
func fields(err error, through holder, pointing *holder, targets []*valueError, slots []valueError) {
	errors.As(err, &through.value)
	errors.As(err, &through.pointer)
	errors.As(err, &through.err)
	errors.As(err, through.value)
	errors.As(err, through.pointer)
	errors.As(err, &pointing.inner.value)
	errors.As(err, pointing.inner)
	errors.As(err, &pointing.inner.err)
	errors.As(err, targets[0])
	errors.As(err, &slots[0])
	errors.As(err, slots[0])
}

// How the call names errors.As: by a renamed import, in parentheses, and as an argument of another call of it. Calls
// that are not calls of errors.As by typeutil.Callee are judged by neither: through a variable holding it, a local
// variable holding it, a field holding a function of its type, a conversion to its type, and the spread call.
func naming(err error) {
	var number int
	stderrors.As(err, number)
	stderrors.As(err, new(valueError))
	(errors.As)(err, number)
	(stderrors.As)(err, &err)
	errors.As(err, errors.As(err, number))

	asValue(err, number)
	local := errors.As
	local(err, number)
	var fieldHolder struct{ As func(error, any) bool }
	fieldHolder.As = errors.As
	fieldHolder.As(err, number)
	_ = (func(error, any) bool)(errors.As)
	errors.As(pair())
	_ = errors.Is(err, nil)
	_ = errors.Unwrap(err)
}

// A local variable named errors shadows the package, so its As is a field, not errors.As.
func shadowed(err error) {
	errors := struct{ As func(error, any) bool }{As: stderrors.As}
	var number int
	errors.As(err, number)
}

// Calls in closures, in go and defer statements, as conditions and in initializers, where the walk must still reach.
func places(err error) {
	var number int
	check := func() bool { return errors.As(err, number) }
	_ = check
	func() {
		errors.As(err, new(valueError))
	}()
	go errors.As(err, number)
	defer errors.As(err, &err)
	if errors.As(err, number) {
		return
	}
	for errors.As(err, new(valueError)) {
		break
	}
	switch {
	case errors.As(err, number):
	}
	matched := errors.As(err, number) && errors.As(err, &err)
	_ = matched
}

// packageLevel is a call in a package-level initializer, outside any function.
var packageLevel = errors.As(nil, 0)

// packageLevelClean is an allowed call in a package-level initializer.
var packageLevelClean = errors.As(nil, new(valueError))

// Type parameters: a parameter's underlying type is its constraint's interface, so one constrained by any is any,
// and one constrained by anything else is not a pointer; a pointer to a parameter points at an interface by
// types.IsInterface, whatever the constraint.
func parameters[
	Anything any,
	Err error,
	Number ~int,
	PointerConstrained interface{ *valueError },
	Faced methods,
	Comparable comparable,
](
	err error,
	anything Anything,
	errorValue Err,
	number Number,
	constrained PointerConstrained,
	faced Faced,
	comparableValue Comparable,
) {
	errors.As(err, anything)
	errors.As(err, errorValue)
	errors.As(err, number)
	errors.As(err, constrained)
	errors.As(err, faced)
	errors.As(err, comparableValue)
	errors.As(err, &anything)
	errors.As(err, &errorValue)
	errors.As(err, &number)
	errors.As(err, &constrained)
	errors.As(err, &faced)
	errors.As(err, &comparableValue)
	errors.As(err, new(Number))
	errors.As(err, generic[Number]{})
	errors.As(err, &generic[Number]{})
	errors.As(err, new(*Err))
}

// A generic function's own call sites are judged once, in its body, however many times it is instantiated.
func forward[T any](err error, target *T) bool {
	return errors.As(err, target)
}

func instantiations(err error) {
	forward(err, new(valueError))
	forward[int](err, nil)
	forward(err, new(error))
}

// methodReceiver judges a call inside a method, on targets reached through its receiver.
func (holding *holder) methodReceiver(err error) {
	errors.As(err, &holding.value)
	errors.As(err, holding.value)
	errors.As(err, &holding.err)
}
