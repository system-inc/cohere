// Package stripped holds a function with a body whose body a strip takes out of the type check, beside one declared
// without a body, each named by a function of the assembly file.
package stripped

// foo has a body, so no Go declaration lays out a frame for its assembly, whatever the type check is given of it.
func foo() { _ = 1 }

// bar is declared without a body, so its frame is laid out from its signature's types, nodes the strip shares.
func bar(x int32)
