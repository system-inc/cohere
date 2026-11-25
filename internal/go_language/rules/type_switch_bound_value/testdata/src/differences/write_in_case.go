package differences

import "fmt"

// A write to x in a case of one type: the original's fix rebinds x as an int there, where `x = nil` does not compile.
func writeInCase(x interface{}) {
	// The finding stands, and the fix is left out, since no rebinding of x can take the write.
	switch x.(type) { // port withholds: Simplify type switch
	case int:
		fmt.Println(x.(int))
		x = nil
	}
	fmt.Println(x)
}
