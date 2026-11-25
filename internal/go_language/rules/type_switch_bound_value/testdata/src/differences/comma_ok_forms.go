package differences

import "fmt"

// A comma-ok assertion assigned to declared variables, declared by var, and parenthesized: the port writes
// `x, true` over the value as it stands, parentheses and all, since `(x, true)` is no expression.
func commaOkForms(x interface{}) {
	var text string
	var isText bool
	// Each of the three is `x, true` in the port's fix, and the name alone in the original's.
	switch x.(type) { // port moves: assigning the result of this type assertion to a variable (switch x := x.(type)) could eliminate type assertions in switch cases
	case string:
		text, isText = x.(string)
		var again, isAgain = x.(string)
		once, isOnce := (x.(string))
		fmt.Println(text, isText, again, isAgain, once, isOnce)
	}
}
