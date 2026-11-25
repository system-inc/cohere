// Package parity holds the shapes SA9008 depends on: the forms of if statement the original's pattern accepts and
// refuses, parentheses included, the reads of the shadowing variable that still hold the failed assertion's value and
// those that no longer do, and the functions an if statement can be in. Each comment says what the original makes of
// it; the parity test holds only that the port makes the same.
package parity

import "fmt"

type holder struct {
	value any
}

func use(values ...any) {}

// Found: a read in the else branch, and each read until the variable is assigned.
func plain(value any) {
	if value, ok := value.(int); ok {
		use(value)
	} else {
		use(value)                // Found.
		fmt.Println(value, value) // Found twice.
		value = 2
		use(value)
	}
}

// Found: parentheses around the assertion and its operand are looked through. gofmt strips them from a condition,
// so no fixture can hold one there.
func parenthesized(value any) {
	if value, ok := (value.(int)); ok {
	} else {
		use(value) // Found.
	}
	if value, ok := (value).(int); ok {
	} else {
		use(value) // Found, the shadowed variable related with its parentheses.
	}
}

// Found: the success variable may have any name, and the assertion any type, an interface or a pointer.
func otherNames(thing any) {
	if thing, isError := thing.(error); isError {
	} else {
		use(thing) // Found.
	}
	if thing, isPointer := thing.(*holder); isPointer {
		use(thing)
	} else if thing != nil { // Found.
	} else {
		use(thing.value) // Found.
	}
}

// Not found: a condition that is more than the success variable, or its negation.
func otherConditions(value any) {
	if value, ok := value.(int); ok && value > 0 {
	} else {
		use(value)
	}
	if value, ok := value.(int); !ok {
		use(value)
	} else {
		use(value)
	}
}

// Not found: an operand other than the shadowed variable, a new variable of another name, or no else branch.
func otherShapes(value any, other any, table map[string]any) {
	if value, ok := other.(int); ok {
	} else {
		use(value)
	}
	if renamed, ok := value.(int); ok {
		use(renamed)
	} else {
		use(renamed)
	}
	if value, ok := value.(int); ok {
		use(value)
	}
	if value, ok := table[""]; ok {
	} else {
		use(value)
	}
	var ok bool
	if value, ok = value.(int); ok {
	} else {
		use(value)
	}
	if value, ok := value.(int); ok {
		use(value)
	} else {
	}
}

// Not found: once the variable is assigned in the else branch, or on one path of it, the reads after see another
// value.
func assigned(value any, condition bool) {
	if value, ok := value.(int); ok {
	} else {
		if condition {
			value = 1
		}
		use(value)
	}
}

// Not found: a variable whose address is taken lives in memory, so no read of it is the assertion's value.
func addressTaken(value any) {
	if value, ok := value.(int); ok {
	} else {
		use(&value)
		use(value)
	}
}

// Not found: a variable a closure captures lives in memory too.
func captured(value any) {
	if value, ok := value.(int); ok {
	} else {
		use(func() int { return value })
		use(value)
	}
}

// Found: an if statement inside a function literal is judged in the literal's own function.
func inLiteral(value any) {
	use(func() {
		if value, ok := value.(int); ok {
		} else {
			use(value) // Found.
		}
	})
}

// Found: a function literal of a package-level variable, a function of the package's initializer.
var literal = func(value any) {
	if value, ok := value.(string); ok {
	} else {
		use(value) // Found.
	}
}

// Found: an explicit init function, which ir.EnclosingFunction finds through the package's initializer.
func init() {
	var value any = 1
	if value, ok := value.(int); ok {
	} else {
		use(value) // Found.
	}
}

// Found: a method, and a generic function asserting to its type parameter.
func (target holder) method() {
	if value, ok := target.value.(int); ok {
		use(value)
	}
	value := target.value
	if value, ok := value.(int); ok {
	} else {
		use(value) // Found.
	}
}

func generic[Type any](value any) {
	if value, ok := value.(Type); ok {
	} else {
		use(value) // Found.
	}
}

type box[Type any] struct {
	value any
}

func (target box[Type]) method() {
	value := target.value
	if value, ok := value.(Type); ok {
	} else {
		use(value) // Found.
	}
}

// Not found: the original finds no function for a declaration named _, nor for the literals inside it.
func _(value any) {
	if value, ok := value.(int); ok {
	} else {
		use(value)
	}
	use(func() {
		if value, ok := value.(int); ok {
		} else {
			use(value)
		}
	})
}
