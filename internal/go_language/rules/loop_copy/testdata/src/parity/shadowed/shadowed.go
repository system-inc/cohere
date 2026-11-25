// Package shadowed declares len and copy at package scope, so its counted loops call no builtin len and none of its
// loops is reported as a call to copy, while an assignment of arrays still is.
package shadowed

func len(values []int) int { return 0 }

var copy = 0

func loops() {
	var source, destination []int
	for index := 0; index < len(source); index++ {
		destination[index] = source[index]
	}
	for index, value := range source {
		destination[index] = value
	}
	var first, second [4]int
	for index := range first {
		second[index] = first[index]
	}
	_ = copy
}
