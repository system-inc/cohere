package runtime_test

// Both skip this file by its name, in the runtime's test package.
func maps() {
	_ = make(chan int, 0)
	_ = make([]int, 1, 1)
}

// A line directive gives what follows another file's name, which both read, so both report this.
//
//line renamed.go:1
func renamed() {
	_ = make(chan int, 0)
}
