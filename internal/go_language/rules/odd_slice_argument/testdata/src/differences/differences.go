// Package differences holds the calls where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: a slice whose length depends on the path taken, a slice through a phi of one
// value, arguments spread over lines, and a slice of a slice of an array.
package differences

func needsPairs(values ...int) {
	if len(values)%2 != 0 {
		panic("odd")
	}
}

func plausible(flag bool, array [7]int) {
	values := []int{1, 2, 3}
	if flag {
		values = []int{1, 2}
	}
	needsPairs(values...)
	same := []int{1}
	for range 2 {
		needsPairs(same...)
	}
	needsPairs(
		1,
		2,
		3,
	)
	needsPairs(array[1:][2:]...)
	needsPairs(array[:5][:3]...)
}
