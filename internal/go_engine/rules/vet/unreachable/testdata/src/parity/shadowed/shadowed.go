// Package shadowed declares its own panic at package level, in the file that calls it, so the parser resolves each
// call to it, and neither the original nor the port takes one for the builtin's. That the port agrees where the
// declaration is in another file is in testdata/src/differences.
package shadowed

import "fmt"

func panic(message string) {
	fmt.Println(message)
}

func callsThePackagesPanic() {
	panic("the package's own, which returns")
	fmt.Println("reached")
}

func stillEndsAtAReturn() {
	return
	fmt.Println("dead") // want "unreachable code"
}
