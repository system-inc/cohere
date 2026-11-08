package parity

// A file named map_test.go is skipped only in the package runtime_test, so both report this one.
func mapsOutsideTheRuntime() {
	_ = make(chan int, 0)
	_ = make([]int, 1, 1)
}
