// Package staticcheck_notes holds a fixture in staticcheck's form: the port meets every note, and the original, which
// never finds a call to gained and words its finding on renamed otherwise, misses two notes and makes a finding no
// note expects.
package staticcheck_notes

func shared() {}

func gained() {}

func renamed() {}

func calls() {
	shared()  //@ diag("call to shared")
	gained()  //@ diag(re"call to gai.ed")
	renamed() //@ diag("in the port's words")
}
