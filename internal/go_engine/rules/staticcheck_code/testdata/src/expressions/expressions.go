// Package expressions holds every kind of expression go/ast declares, and every kind of call staticcheck's
// MayHaveSideEffects tells apart, for the port and the original to judge side by side.
package expressions

type point struct {
	x int
	y int
}

type shape interface {
	area() int
}

func (location point) area() int {
	return location.x * location.y
}

// double is pure, so staticcheck's purity pass marks it.
func double(value int) int {
	return value * 2
}

var counter int

// bump writes a package variable, so it is not pure.
func bump() int {
	counter++
	return counter
}

func sum(values ...int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func expressions(values []int, channel chan int, table map[string]int, boxed interface{}, flag bool) {
	location := point{x: 1, y: 2}
	pointer := &location
	array := [...]int{1, 2, 3}
	literal := func(value int) int { return value + 1 }
	var reader <-chan int = channel
	var anonymous struct{ name string }
	var kind shape = location

	_ = double(2)
	_ = double(bump())
	_ = bump()
	_ = len(values) + cap(values)
	_ = len(table)
	_ = append(values, 1)
	_ = int64(len(values))
	_ = literal(3)
	_ = location.area()
	_ = kind.area()
	_ = sum(values...)
	_ = (*pointer).x
	_ = pointer.y
	_ = values[1:2:3]
	_ = values[1:]
	_ = values[:double(1)]
	_ = values[bump():]
	_ = array[len(array)-1]
	_ = table["key"]
	_ = boxed.(int)
	_, _ = boxed.(point)
	_ = <-channel
	_ = <-reader
	_ = -counter
	_ = !flag
	_ = &location
	_ = &point{x: double(1)}
	_ = []point{{x: 1}, {y: bump()}}
	_ = map[string]chan int{"key": channel}
	_ = map[int]func() int{0: bump}
	_ = (counter + 1) * 2
	_ = anonymous.name
	_ = []interface{ area() int }{location}
	_ = [2]struct{}{}
	_ = "text"
}
