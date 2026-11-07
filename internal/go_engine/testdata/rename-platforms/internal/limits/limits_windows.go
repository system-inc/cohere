package limits

// extra is what windows adds to a read.
func extra() int {
	return maxBytes / 2
}
