package odd

// platformPairs is windows's, which says how many values it has and needs them even.
func platformPairs(values []any) {
	println("pairs", len(values))
	if len(values)%2 != 0 {
		panic("odd")
	}
}
