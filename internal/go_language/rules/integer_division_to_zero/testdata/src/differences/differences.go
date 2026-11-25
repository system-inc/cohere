// Package differences holds the divisions where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: literals larger than any integer type, results that land in a complex or an
// unsigned type, a division in an array length, and spellings Render could print differently from the source.
package differences

func plausible() {
	_ = 1 / 18446744073709551616
	_ = 18446744073709551615 / 18446744073709551616
	_ = 100000000000000000000000000000 / 100000000000000000000000000001
	var number complex128 = 1 / 2
	_ = number
	var count uint64 = 0 / 18446744073709551615
	_ = count
	var table [2 / 3]int
	_ = table
	_ = 1 /
		2
	_ = (1) /
		(2)
	_ = 1 / 2 // a comment after
	_ = 1 /* inside */ / 2
}
