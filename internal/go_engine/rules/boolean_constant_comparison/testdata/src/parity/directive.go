package parity

// directive holds comparisons under line directives naming a test file and another file, which change no file the
// judgment reads: the file is the one parsed.
func directive() {
	var x bool
//line directive_test.go:1
	_ = x == true // reported: a directive naming a test file makes no test file
//line directive.go:12
	_ = x == false // reported
}
