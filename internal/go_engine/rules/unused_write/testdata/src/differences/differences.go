// Package differences holds the shapes where the port and x/tools' unusedwrite disagree, each adjudicated in the
// port's favour. A finding only the port makes is recorded on its line by a "finds" comment with its message, and a
// finding both make in different words by a "renames" comment with the original's message and the port's. Nothing
// the original finds may be missing from the port.
package differences

type pair struct{ left, right int }

// staticcheck lifts the read of the whole struct that comes before the field's address is taken, so the read no
// longer counts as a use of the write after it, which nothing reads: the port is right.
func readThenWrite() pair {
	var value pair
	result := value
	value.left = 1 // port finds: unused write to field left
	return result
}

// The read of the whole struct is on a path the write never reaches, and staticcheck lifts it, so the write, which
// nothing after it reads, is reported: the port is right.
func branches(condition bool) pair {
	var value pair
	if condition {
		value.left = 1 // port finds: unused write to field left
	} else {
		return value
	}
	return pair{}
}

// The same for a literal read whole before a field is written. The read sees the literal's own fields, so they are
// not reported, and the field written after it, which nothing reads, is.
func literalReadThenWritten() pair {
	value := &pair{left: 1}
	copied := *value
	value.right = 2 // port finds: unused write to field right
	return copied
}

// buildir's functions include the closures of package-level initializers, which buildssa's leave out: the write is
// dead, and the port is right.
var initialized = func() int {
	var value pair
	value.left = 1 // port finds: unused write to field left
	return 0
}()

// x/tools loads the array as the range begins, although the Go specification evaluates no range expression when
// only the index is used and the length is constant, and that load keeps the writes alive. staticcheck builds no
// such load, so the writes, which nothing reads, are reported: the port is right. The index in the message is the
// form's own register, as the original's would be.
func rangeByIndex() {
	var values [3]int
	for index := range values {
		values[index] = index // port finds: unused write to array index t4 + 1:int
	}
}

func position() int { return 1 }

// The original names an index the function computes by its registers, and the two forms number their registers
// differently, so both report this write with messages that differ in the registers' names alone. That is an artifact
// of the single assignment form, not a difference in what is found, adjudicated in the port's favour: the finding is
// the same, and its text cannot be.
func computedCallIndex(count int) {
	total := count * 3
	var values [8]int
	values[position()*2+total] = 1 // port renames: unused write to array index t3 + t0 => unused write to array index t4 + t1
}
