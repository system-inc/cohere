// Package differences holds where the port and the original part, each with its reason. The package declares its own
// panic in declaration.go, which returns, so no call of panic here ends the flow of control. The parser resolves a name
// only within its file, so the original, which reads that resolution alone, takes these calls for the builtin's.
package differences

import "fmt"

func callsThePackagesPanic() {
	panic("the package's own, which returns")
	// The statement runs once the package's panic returns, so it is reachable.
	fmt.Println("reached") // port drops: unreachable code
}

func callsItInASwitch(number int) {
	switch number {
	case 1:
		return
	default:
		panic("the package's own, which returns")
	}
	// The default case falls out of the switch once the package's panic returns, so the statement is reachable.
	fmt.Println("reached") // port drops: unreachable code
}

func agreesOnAReturn() {
	return
	fmt.Println("dead")
}
