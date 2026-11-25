// Package parity holds the shapes SA5010 depends on: the kinds of assertion the IR builds, and the method sets on
// either side of one. Each function's comment says what the original makes of it; the parity test holds only that the
// port makes the same.
package parity

import (
	"fmt"
	"io"
)

type label string

type counter interface {
	Count() int
}

type namer interface {
	Count() string
}

type sizedCounter interface {
	counter
	Size() int
}

type reader interface {
	Read() string
}

type joiner interface {
	Join(parts []string) string
}

type variadicJoiner interface {
	Join(parts ...string) string
}

type labeler interface {
	String() label
}

type agreeing interface {
	Count() int
	Name() string
}

type wide interface {
	Count() string
	Size() string
	Read() int
}

type generic[Value any] interface {
	Count() Value
}

type implementation struct{}

func (implementation) Count() int { return 0 }

func use(values ...any) {}

// Found: two interfaces with one method of one name and two results.
func plain(value counter) {
	_ = value.(namer)
}

// Found: the contradiction comes through an embedded interface on the asserting side.
func embedded(value sizedCounter) {
	_ = value.(namer)
}

// Found: a comma-ok assertion is judged as a plain one.
func commaOk(value counter) {
	asserted, isNamer := value.(namer)
	use(asserted, isNamer)
}

// Not found: the IR builds a type switch as a TypeSwitch instruction, no assertion, so its cases go unjudged.
func typeSwitch(value counter) {
	switch value.(type) {
	case namer:
	case fmt.Stringer:
	}
}

// Found: the asserted type is from another package, and both are written relative to this one.
func otherPackage(value reader) {
	_ = value.(io.Reader)
}

// Found: a variadic parameter is no slice parameter.
func variadic(value joiner) {
	_ = value.(variadicJoiner)
}

// Found: a named result type is no string.
func namedResult(value labeler) {
	_ = value.(fmt.Stringer)
}

// Found: three contradictions in one assertion, each written on its own lines.
func many(value interface {
	Count() int
	Size() int
	Read() string
}) {
	_ = value.(wide)
}

// Found: a literal interface on both sides.
func literals(value interface{ Count() int }) {
	_ = value.(interface{ Count() bool })
}

// Found: in a closure.
func closure(value counter) func() {
	return func() {
		_ = value.(namer)
	}
}

// Not found: the methods agree, and a method the value lacks contradicts nothing.
func agrees(value counter) {
	_ = value.(agreeing)
}

// Not found: an empty interface has no method to contradict.
func empty(value any) {
	_ = value.(namer)
	_ = value.(interface{})
}

// Not found: an assertion to a concrete type is the compiler's to refuse.
func concrete(value counter) {
	_ = value.(implementation)
}

// Not found: a generic interface's method on the value's side gives the assertion up.
func genericValue(value generic[int]) {
	_ = value.(namer)
}

// Not found: a generic interface's method on the asserted side gives the assertion up.
func genericAsserted(value counter) {
	_ = value.(generic[string])
}

// Not found: assertion to the same interface.
func same(value counter) {
	_ = value.(counter)
}
