// Package differences holds the shapes where the port and the original could most plausibly part, and records no
// difference: the two agree on every one.
package differences

import "fmt"

type pair[Value any] struct {
	first  Value
	second Value
}

// A generic type's method has a receiver and type parameters, and no results.
func (values pair[Value]) print() {
	fmt.Println(values.first, values.second)
	return
}

func nesting(value int, boxed interface{}, channel chan int) {
	select {
	case received := <-channel:
		// The select's clause is left alone, and the switch inside it is judged as any other.
		switch received {
		case 1:
			fmt.Println()
			break
		}
	}

	switch value {
	case 1:
		// The clause ends in a call of a literal, which is judged as a function on its own.
		fmt.Println()
		func() {
			fmt.Println()
			return
		}()
	case 2:
		// The break in a block is not the clause's last statement.
		fmt.Println()
		{
			break
		}
	case 3:
		// A literal whose body ends in a switch whose clause ends in a break: the clause is reported, the literal not.
		_ = func() {
			switch value {
			default:
				fmt.Println()
				break
			}
		}
		break
	}

	// A type switch that binds nothing has case clauses as one that binds does.
	switch boxed.(type) {
	case int, string:
		fmt.Println()
		break
	}
}

// A function whose result is a function type has results, though the function it returns has none.
func returnsFunction() func() {
	return func() {
		fmt.Println()
		return
	}
}

// A function whose parameter is a function with no results has none itself, and so does the parameter's type.
func takesFunction(callback func()) {
	callback()
	return
}

// A deferred recover and a return after it, as a function that ends in a cleanup.
func deferred() {
	defer func() {
		_ = recover()
	}()
	panic("deferred")
	return
}
