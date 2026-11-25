package differences

import "fmt"

// A range that assigns to x in a case of one type: the original's fix rebinds x as a string there, and the range's
// values are interfaces, which a string can't take.
func rangeWrite(x interface{}, values []interface{}) {
	// The finding stands, and the fix is left out, since no rebinding of x can take the write.
	switch x.(type) { // port withholds: Simplify type switch
	case string:
		fmt.Println(x.(string))
		for _, x = range values {
		}
	}
	fmt.Println(x)
}
