package differences

import (
	"fmt"
	"unsafe"
)

// An assertion in the case's type beside one in its body: the body's is replaced, and the type's is left as it is.
// The original replaces both, so the case is `[16]byte`, and the body's eight bytes are given sixteen.
func besideCaseList(x interface{}) {
	// The port's fix leaves `x.([8]byte)` in the case's type, where the original's writes `x`, and its related
	// entries leave that assertion out, since the binding can't eliminate it.
	switch x.(type) { // port moves: assigning the result of this type assertion to a variable (switch x := x.(type)) could eliminate type assertions in switch cases
	// The original's related entry on the case type's assertion says it could be eliminated, and it can't.
	case [unsafe.Sizeof(x.([8]byte))]byte: // port omits: could eliminate this type assertion
		var eight [8]byte = x.([8]byte)
		fmt.Println(eight)
	}
}
