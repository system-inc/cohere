package differences

import "fmt"

// A comma-ok assertion in the case of its own type always holds. The original replaces it with the name alone,
// `number, isNumber := x`, one value assigned to two, which does not compile, and the port with `x, true`.
func commaOk(x interface{}) {
	// The port writes `number, isNumber := x, true` where the original writes `number, isNumber := x`.
	switch x.(type) { // port moves: assigning the result of this type assertion to a variable (switch x := x.(type)) could eliminate type assertions in switch cases
	case int:
		number, isNumber := x.(int)
		fmt.Println(number, isNumber)
	}
}
