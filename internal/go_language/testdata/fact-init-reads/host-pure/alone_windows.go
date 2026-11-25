package alone

// counter counts platformValue's calls.
var counter int

// platformValue counts its calls here, so it is not pure.
func platformValue(value int) int {
	counter += value
	return counter
}

// platformName is windows's.
func platformName() string {
	return "windows"
}
