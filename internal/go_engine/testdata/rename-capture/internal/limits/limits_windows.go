package limits

// extra is what windows adds to a read, a share of the most it takes.
func extra() int {
	maximumBytes := 8
	return maxBytes / maximumBytes
}
