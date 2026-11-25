package differences

import "fmt"

// The default clause declares its own x in its own block, where the original's fix declares the binding too, and
// `x := 0` then declares no new variable, which does not compile.
func redeclaredInClause(x interface{}) {
	// The finding stands, and the fix is left out, since the clause already holds a name the binding needs.
	switch x.(type) { // port withholds: Simplify type switch
	case string:
		fmt.Println(x.(string))
	default:
		x := 0
		fmt.Println(x)
	}
}
