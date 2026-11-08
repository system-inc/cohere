// Package parity holds every shape S1018's query and judgment branch on, each beside its near miss, for the original
// and the port to judge alike.
package parity

type ints []int

type holder struct {
	values []int
	offset int
}

const zero = 0

const size = 4

// counters is the initializer: a counter, one identifier, declared with := as an integer literal that prints as 0. A
// counter assigned, not declared, is in the differences fixture, where the port withholds the fix, and so are the
// operators that leave it where it stood, where the port drops the finding.
func counters(values []int, n int, offset int) {
	for i := 0; i < n; i++ {
		values[i] = values[offset+i]
	}
	var i int
	// Every spelling of the literal zero.
	for i := 0x0; i < n; i++ {
		values[i] = values[offset+i]
	}
	for i := 00; i < n; i++ {
		values[i] = values[offset+i]
	}
	for i := 0b0; i < n; i++ {
		values[i] = values[offset+i]
	}
	for i := 0o0; i < n; i++ {
		values[i] = values[offset+i]
	}
	for i := -0; i < n; i++ {
		values[i] = values[offset+i]
	}
	for i := +0; i < n; i++ {
		values[i] = values[offset+i]
	}
	for i := (0); i < n; i++ {
		values[i] = values[offset+i]
	}
	for i := -(0); i < n; i++ {
		values[i] = values[offset+i]
	}
	// A start other than zero, and a zero that is no literal.
	for i := 1; i < n; i++ {
		values[i] = values[offset+i]
	}
	for i := zero; i < n; i++ {
		values[i] = values[offset+i]
	}
	for i := int(0); i < n; i++ {
		values[i] = values[offset+i]
	}
	for i := '\x00'; i < rune(n); i++ {
		values[i] = values[rune(offset)+i]
	}
	// Two counters, and none.
	for i, j := 0, 0; i < n; i++ {
		values[i] = values[offset+i]
		_ = j
	}
	for i, j := 0, 0; i < n; i++ {
		values[i] = values[offset+i+j]
	}
	for ; i < n; i++ {
		values[i] = values[offset+i]
	}
}

// conditions is the condition: the counter less than a limit named by an identifier.
func conditions(values []int, n int, offset int, held holder) {
	for i := 0; (i) < (n); i++ {
		values[i] = values[offset+i]
	}
	for i := 0; i < size; i++ {
		values[i] = values[offset+i]
	}
	for i := 0; i <= n; i++ {
		values[i] = values[offset+i]
	}
	for i := 0; n > i; i++ {
		values[i] = values[offset+i]
	}
	for i := 0; i < len(values); i++ {
		values[i] = values[offset+i]
	}
	for i := 0; i < held.offset; i++ {
		values[i] = values[offset+i]
	}
	for i := 0; i < n+0; i++ {
		values[i] = values[offset+i]
	}
	for i := 0; i < n && i < offset; i++ {
		values[i] = values[offset+i]
	}
	j := 0
	for i := 0; j < n; i++ {
		values[i] = values[offset+i]
	}
	for i := 0; ; i++ {
		values[i] = values[offset+i]
	}
}

// posts is the post statement: the counter incremented.
func posts(values []int, n int, offset int) {
	for i := 0; i < n; (i)++ {
		values[i] = values[offset+i]
	}
	for i := 0; i < n; i-- {
		values[i] = values[offset+i]
	}
	for i := 0; i < n; i += 1 {
		values[i] = values[offset+i]
	}
	j := 0
	for i := 0; i < n; j++ {
		values[i] = values[offset+i]
	}
	for i := 0; i < n; {
		values[i] = values[offset+i]
	}
}

// bodies is the body: one assignment storing at the counter the same slice's element at the offset plus the counter.
func bodies(values []int, others []int, n int, offset int, held holder) {
	// Parentheses are looked through everywhere the pattern looks.
	for i := 0; i < n; i++ {
		(values)[(i)] = (values)[(offset + i)]
	}
	for i := 0; i < n; i++ {
		(values[i]) = (values[(offset)+(i)])
	}
	// A body of one block of one statement is that statement.
	for i := 0; i < n; i++ {
		{
			values[i] = values[offset+i]
		}
	}
	for i := 0; i < n; i++ {
		{
			{
				values[i] = values[offset+i]
			}
		}
	}
	// Two statements are no slide, in the body or in its one block.
	for i := 0; i < n; i++ {
		values[i] = values[offset+i]
		_ = i
	}
	for i := 0; i < n; i++ {
		{
			values[i] = values[offset+i]
			_ = i
		}
	}
	for i := 0; i < n; i++ {
	}
	for i := 0; i < n; i++ {
		{
		}
	}
	// The offset comes first in the sum, and is added.
	for i := 0; i < n; i++ {
		values[i] = values[i+offset]
	}
	for i := 0; i < n; i++ {
		values[i] = values[offset-i]
	}
	for i := 0; i < n; i++ {
		values[i] = values[offset+i+0]
	}
	for i := 0; i < n; i++ {
		values[i] = values[1+i]
	}
	for i := 0; i < n; i++ {
		values[i] = values[held.offset+i]
	}
	// The store is at the counter, plainly assigned, of one value.
	for i := 0; i < n; i++ {
		values[i+0] = values[offset+i]
	}
	j := 0
	for i := 0; i < n; i++ {
		values[j] = values[offset+i]
	}
	for i := 0; i < n; i++ {
		values[i] = values[offset+j]
	}
	for i := 0; i < n; i++ {
		values[i] += values[offset+i]
	}
	for i := 0; i < n; i++ {
		values[i], values[j] = values[offset+i], 0
	}
	for i := 0; i < n; i++ {
		values[i] = values[offset+i] + 0
	}
	// One slice on both sides, named by an identifier.
	for i := 0; i < n; i++ {
		others[i] = values[offset+i]
	}
	for i := 0; i < n; i++ {
		values[i] = others[offset+i]
	}
	for i := 0; i < n; i++ {
		held.values[i] = held.values[offset+i]
	}
}

// kinds is the slice's type: every type in its type set a slice.
func kinds(n int, offset int) {
	var named ints
	for i := 0; i < n; i++ {
		named[i] = named[offset+i]
	}
	var rows [][]int
	for i := 0; i < n; i++ {
		rows[i] = rows[offset+i]
	}
	var array [8]int
	for i := 0; i < n; i++ {
		array[i] = array[offset+i]
	}
	pointer := &array
	for i := 0; i < n; i++ {
		pointer[i] = pointer[offset+i]
	}
	var table map[int]int
	for i := 0; i < n; i++ {
		table[i] = table[offset+i]
	}
}

// generic's slice is a type parameter whose type set is slices alone.
func generic[S ~[]E, E any](values S, n int, offset int) {
	for i := 0; i < n; i++ {
		values[i] = values[offset+i]
	}
}

// mixed's type parameter admits an array beside the slice, so not every type in its type set is a slice.
func mixed[S []int | [8]int](values S, n int, offset int) {
	for i := 0; i < n; i++ {
		values[i] = values[offset+i]
	}
}

// labeled loops are reported at the loop, the label left in place.
func labeled(values []int, n int, offset int) {
first:
	for i := 0; i < n; i++ {
		values[i] = values[offset+i]
	}
	goto first
}

// functionLiteral holds a loop in a function literal, walked like any other.
func functionLiteral(values []int, n int, offset int) func() {
	return func() {
		for i := 0; i < n; i++ {
			values[i] = values[offset+i]
		}
	}
}

// ranged loops are no for statement.
func ranged(values []int, offset int) {
	for i := range values {
		values[i] = values[offset+i]
	}
}
