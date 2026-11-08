// Package differences holds the remainders where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: a divisor whose type the context gives it, a literal of the largest values
// its type holds, a remainder in a constant or an array length, and divisors that only print like 1.
package differences

type small uint8

func plausible(value int, tiny small, huge uint64) {
	_ = tiny % 1
	_ = tiny % (+1)
	_ = huge % 18446744073709551615
	_ = huge % 0x1
	_ = value % 9223372036854775807
	_ = value % -9223372036854775808
	_ = value % 1.0
	_ = value % 0.1e1
	_ = value % 'a'
	var table [3 % 1]int
	_ = table
	const folded = 1 << 62 % 1
	_ = folded
}
