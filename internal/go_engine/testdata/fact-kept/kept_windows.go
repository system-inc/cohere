package kept

// platformBase is windows's, and pure.
func platformBase(value int) int {
	return value + 2
}

// platformShift is windows's, which counts nothing, so it is pure.
func platformShift(value int) int {
	return value + 3
}
