// Package module_notes is a fixture module in staticcheck's form: the port meets both notes, and the original misses
// the one on gained.
package module_notes

func shared() {}

func gained() {}

func calls() {
	shared() //@ diag("call to shared")
	gained() //@ diag("call to gained")
}
