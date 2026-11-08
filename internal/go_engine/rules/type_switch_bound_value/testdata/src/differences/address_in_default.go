package differences

import "fmt"

// The address of x taken in the default clause: the original's fix compiles, and the pointer is to the default
// clause's own copy of x, so the write through it never reaches the x printed after the switch.
func addressInDefault(x interface{}) {
	// The finding stands, and the fix is left out, since the rebinding would change what the write reaches.
	switch x.(type) { // port withholds: Simplify type switch
	case int:
		fmt.Println(x.(int))
	default:
		pointer := &x
		*pointer = 0
	}
	fmt.Println(x)
}
