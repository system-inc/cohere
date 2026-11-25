package runtime_test

// A line directive naming map_test.go, which both read as the file's name, so both skip what follows.
//
//line map_test.go:100
func directed() {
	_ = make(chan int, 0)
	_ = make([]int, 1, 1)
}
