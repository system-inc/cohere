package generated

// Shared ends in a redundant return that a line directive places in gen_notwindows.go, which every build but
// windows compiles and staticcheck judges generated, so S1023 withholds it there and reports it on windows.
func Shared() {
	println()
//line gen_notwindows.go:1
	return
}
