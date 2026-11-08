// Package shapes holds every shape staticcheck's shortRange cuts short, and the expressions, calls and selectors it
// leaves whole, for the kit's Report and staticcheck's to report at side by side.
package shapes

type pair struct {
	first  int
	second int
}

func (both pair) sum() int {
	return both.first + both.second
}

func statements(values []int, channel chan int, boxed interface{}) int {
	defer println("deferred")
	println(pair{first: 1, second: 2}.sum())
	total := 0
	for index := 0; index < 3; index++ {
		total += index
	}
	for len(values) > 0 {
		values = values[1:]
	}
	for index := 0; ; {
		total += index
		break
	}
	for {
		break
	}
	double := func(value int) int {
		return value * 2
	}
	go double(1)
	go func() {}()
	go (func() {})()
	if len(values) == 0 {
		total++
	}
	for _, value := range values {
		total += value
	}
	select {
	case value := <-channel:
		total += value
	default:
	}
	switch len(values) {
	case 0:
		total--
	default:
	}
	switch count := len(values); {
	case count > 0:
		total++
	}
	switch {
	case len(values) > 1:
		total++
	}
	switch typed := boxed.(type) {
	case int:
		total += typed
	}
	return total
}
