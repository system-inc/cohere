//go:build go1.18

package shapes

// tagged is in a file whose build line names its own language version.
func tagged(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
