// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none. The original finds the function of each if statement with ir.EnclosingFunction over
// the whole package's syntax, and the port judges each if statement in the function whose own syntax holds it, asking
// ir.EnclosingFunction too, so these are the places where a function's syntax and the functions staticcheck builds part:
// literals nested deep, literals in dead code, literals staticcheck never builds because the expression around them is
// a constant, and a method named _.
package differences

import "unsafe"

type holder struct{}

func use(values ...any) {}

// Found by both: an if statement two function literals deep is judged in the inner one.
func nested(value any) {
	use(func() {
		use(func() {
			if value, ok := value.(int); ok {
			} else {
				use(value)
			}
		})
	})
}

// Found by both: staticcheck builds a function literal after a return too.
func dead(value any) {
	return
	use(func() {
		if value, ok := value.(int); ok {
		} else {
			use(value)
		}
	})
}

// Found by neither: unsafe.Sizeof of a function literal is a constant, so staticcheck builds no function for the
// literal, and ir.EnclosingFunction finds none.
func constant(value any) {
	use(unsafe.Sizeof(func() {
		if value, ok := value.(int); ok {
		} else {
			use(value)
		}
	}))
}

// Found by neither: a literal in a type's array length is in no function at all.
type sized [unsafe.Sizeof(func(value any) {
	if value, ok := value.(int); ok {
	} else {
		use(value)
	}
})]byte

// Found by neither: a method named _ is in no method set, so ir.EnclosingFunction finds no function for it.
func (holder) _(value any) {
	if value, ok := value.(int); ok {
	} else {
		use(value)
	}
}
