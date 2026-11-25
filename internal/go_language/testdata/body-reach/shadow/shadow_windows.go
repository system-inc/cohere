package shadow

// byte is windows's own name for the universe's byte.
type byte = uint8

// len counts values on windows in place of the universe's.
func len(values []int) int {
	count := 0
	for range values {
		count++
	}
	return count
}
