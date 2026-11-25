// Package differences holds where the port parts from the original, one recorded shape a file, and in this file the
// shapes where the two could most plausibly part and do not: two values assigned to two, which is no comma-ok form, a
// read of x in the default clause and a write to an x a nested block declares, which keep the fix, a method's
// parameter and named result in deferred literals, a switch nested in a case of another variable, and a generic
// type's method.
package differences

import "fmt"

// Two values assigned to two is no comma-ok form, so the assertion is replaced with the name alone, as the original
// replaces it.
func twoToTwo(x interface{}) {
	switch x.(type) {
	case int:
		first, second := x.(int), 1
		fmt.Println(first, second)
	}
}

// A read of x in another clause is no write, so the fix is kept, and the default clause reads its copy.
func readInDefault(x interface{}) {
	switch x.(type) {
	case int:
		fmt.Println(x.(int))
	default:
		fmt.Println(x)
	}
}

// A write to an x a nested block declares is a write to another variable, and the nested declaration shadows the
// binding, so the fix is kept.
func shadowedWrite(x interface{}) {
	switch x.(type) {
	case int:
		fmt.Println(x.(int))
	default:
		{
			x := 1
			x = 2
			fmt.Println(x)
		}
	}
}

type box[T any] struct {
	value interface{}
}

type describer struct{}

// A method's parameter is a variable like any other, and so is a named result, asserted in a deferred literal.
func (describer) describe(parameter interface{}) (result interface{}) {
	switch parameter.(type) {
	case string:
		defer func() {
			fmt.Println(parameter.(string))
		}()
	}
	switch result.(type) {
	case error:
		defer func() {
			fmt.Println(result.(error).Error())
		}()
	}
	return parameter
}

// The inner switch is judged on its own, and its assertions of its own variable are unrelated to the outer one's.
func nested(x interface{}, y interface{}) {
	switch x.(type) {
	case int:
		fmt.Println(x.(int))
	case string:
		switch y.(type) {
		case bool:
			fmt.Println(y.(bool))
		}
	}
}

// A generic type's method switches on a field through no identifier, and on a local through one.
func (container box[T]) judge() {
	switch container.value.(type) {
	case T:
		fmt.Println(container.value.(T))
	}
	local := container.value
	switch local.(type) {
	case T:
		fmt.Println(local.(T))
	}
}
