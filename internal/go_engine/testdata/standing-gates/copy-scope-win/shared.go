package gate

func target(values []int, n int, offset int) {
	for i := 0; i < n; i++ {
		values[i] = values[offset+i]
	}
}
