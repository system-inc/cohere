package parity

// Under go1.3 a range statement may not leave out its variables, so neither reports a range here; a receive is
// reported under any version.
func olderRanges(m map[string]int, ch chan int) {
	var key string
	var x int
	for _ = range m {
	}
	for _, _ = range m {
	}
	for key, _ = range m {
	}
	for index, _ := range m {
		_ = index
	}
	_ = <-ch
	x, _ = <-ch
	_, _ = key, x
}
