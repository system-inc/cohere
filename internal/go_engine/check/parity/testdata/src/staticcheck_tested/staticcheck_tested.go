// Package staticcheck_tested holds a note in a file the package and its test variant share, and one in its test file,
// so the test variant finds both calls and the package one.
package staticcheck_tested

func shared() {}

func calls() {
	shared() //@ diag("call to shared")
}
