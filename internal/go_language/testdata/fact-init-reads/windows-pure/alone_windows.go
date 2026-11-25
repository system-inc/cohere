package alone

// platformValue is pure here.
func platformValue(value int) int {
	return value + 2
}

// platformName is windows's.
func platformName() string {
	return "windows"
}
