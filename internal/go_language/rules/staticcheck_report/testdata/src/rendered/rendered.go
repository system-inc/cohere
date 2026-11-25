// Package rendered holds expressions of every kind, some spread over lines, a comment inside one, and calls of no, one
// and several arguments, for the kit's Render and RenderArgs and staticcheck's to print side by side.
package rendered

type point struct {
	x int
	y int
}

func call(values ...int) int { return len(values) }

func expressions(values []int, table map[string]int, channel chan int, boxed interface{}) {
	_ = call()
	_ = call(1)
	_ = call(1, 2, values[0])
	_ = call(
		1,
		2,
	)
	_ = call(values...)
	_ = point{
		x: 1, // the first
		y: 2,
	}
	_ = values[1:2:3]
	_ = table["key"] + len(values)*(3-1)
	_ = boxed.(point).x
	_ = func(value int) int {
		return value * 2
	}(4)
	_ = map[string][]chan<- int{}
	_ = <-channel
	_ = &point{}
	_ = !(len(values) > 0 && len(values) < 10 ||
		len(values) == 0)
	_ = struct{ name string }{"name"}
	_ = [...]int{1, 2, 3}
}
