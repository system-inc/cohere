package differences

import (
	"fmt"
	"unsafe"
)

// The case's array length asserts the switched variable to the case's own type, and the original's inspection of the
// whole clause finds it. But the case's type is read before the clause's binding is declared, so the assertion is
// needed there, and binding the value would eliminate nothing: the original's fix writes `[unsafe.Sizeof(x)]byte`,
// `[16]byte`, and a binding no case uses, which does not compile.
func inCaseList(x interface{}) {
	// The only assertion is in the case's type, which the binding can't replace, so there is nothing to simplify.
	switch x.(type) { // port drops: assigning the result of this type assertion to a variable (switch x := x.(type)) could eliminate type assertions in switch cases
	case [unsafe.Sizeof(x.([8]byte))]byte:
		fmt.Println("eight")
	}
}
