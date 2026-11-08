// Package parity holds every function shape `ST1008` reads, each found and each near miss, for the original and the
// port to judge alike.
package parity

import "unsafe"

// Every arrangement of results the check reads, each declared as a function.
func errorFirst() (error, int)                   { return nil, 0 }
func errorMiddle() (int, error, string)          { return 0, nil, "" }
func twoMisplaced() (error, error, int)          { return nil, nil, 0 }
func namedResults() (err error, count int)       { return nil, 0 }
func groupedNames() (first, second error, n int) { return nil, nil, 0 }
func errorLast() (int, error)                    { return 0, nil }
func errorsBothEnds() (error, int, error)        { return nil, 0, nil }
func commaOk() (error, bool)                     { return nil, false }
func commaOkAfterValue() (int, error, bool)      { return 0, nil, false }
func boolAfterInteger() (error, int, bool)       { return nil, 0, false }
func singleError() error                         { return nil }
func noResults()                                 {}

// errorAlias names error, so a result of it is an error, as the original resolves it.
type errorAlias = error

func throughAlias() (errorAlias, int) { return nil, 0 }

// namedError is a type of its own, no error by identity, so neither side reports it.
type namedError error

func throughNamedType() (namedError, int) { return nil, 0 }

// An interface written out as error's is no error by identity either.
func throughInterfaceLiteral() (interface{ Error() string }, int) { return nil, 0 }

// A method is a function the intermediate representation builds, judged as one.
type receiver struct{}

func (receiver) method() (error, int)          { return nil, 0 }
func (*receiver) pointerMethod() (error, bool) { return nil, false }

// A blank function is built all the same.
func _() (error, int) { return nil, 0 }

// A generic function and a generic type's method are built once, generic.
func generic[Element any]() (error, Element) {
	var zero Element
	return nil, zero
}

type genericType[Element any] struct{}

func (genericType[Element]) method() (error, Element) {
	var zero Element
	return nil, zero
}

// A function type and an interface's method declare no function, so neither side reports them.
type functionType func() (error, int)

type withMethod interface {
	method() (error, int)
}

// Function literals are anonymous functions of the function around them, nested ones too.
func literals() {
	first := func() (error, int) { return nil, 0 }
	second := func() (int, error) {
		inner := func() (error, string) { return nil, "" }
		_ = inner
		return 0, nil
	}
	defer func() (error, int) { return nil, 0 }()
	go func() (error, int) { return nil, 0 }()
	_, _ = first, second
}

// A literal in a package variable's initializer is an anonymous function of the package's initializer, a blank
// variable's too.
var initialized = func() (error, int) { return nil, 0 }

var _ = func() (error, int) { return nil, 0 }

// A literal inside an expression the type checker gives a constant value is never built, since the builder takes the
// constant, so neither side reports it.
const literalSize = unsafe.Sizeof(func() (error, int) { return nil, 0 })

var literalCount = len([1]func() (error, int){func() (error, int) { return nil, 0 }})

func constants() uintptr {
	var array [unsafe.Sizeof(func() (error, int) { return nil, 0 })]byte
	_ = array
	return unsafe.Sizeof(func() (error, int) { return nil, 0 })
}

// A literal beside a constant, never inside one, is built.
func besideConstant() (uintptr, func() (error, int)) {
	return unsafe.Sizeof(0), func() (error, int) { return nil, 0 }
}

// A local type named error shadows the universe's, so its results are no error to either side.
func shadowed() {
	type error interface{ Error() string }
	_ = func() (error, int) { return nil, 0 }
}
