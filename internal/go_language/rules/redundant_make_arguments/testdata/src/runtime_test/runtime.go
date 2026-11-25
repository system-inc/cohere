// Package runtime_test is named as the runtime's own tests are, which both skip in a file named map_test.go and
// report in any other.
package runtime_test

func elsewhere() {
	_ = make(chan int, 0)
	_ = make([]int, 1, 1)
}
