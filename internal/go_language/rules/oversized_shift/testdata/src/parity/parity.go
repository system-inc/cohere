// Package parity holds the shapes SA9006 depends on: the shifts the pattern
// `(Or (AssignStmt _ (Or ">>=" "<<=") _) (BinaryExpr _ (Or ">>" "<<") _))` meets, the types of the shifted operand,
// fixed in size or not, and the shift amounts ExprToInt reads or does not. Each line's comment says what the original
// makes of it; the parity test holds only that the port makes the same.
package parity

type flags uint8

type wide = int64

type holder struct {
	value int16
}

const eight = 8

func use(values ...any) {}

func shifts(small int8, unsigned uint8, character rune, letter byte, named flags, aliased wide, count uint) {
	use(small << 8)               // Found.
	use(small << 7)               // Not found: below the width.
	use(unsigned >> 8)            // Found.
	use(character << 32)          // Found: a rune is an int32.
	use(letter << 8)              // Found: a byte is a uint8.
	use(named << 9)               // Found: a type defined on uint8.
	use(aliased << 64)            // Found: an alias of int64.
	use(small << eight)           // Found: a named constant.
	use(small << (4 * 2))         // Found: a constant expression.
	use((small) << 8)             // Found: the operand in parentheses.
	use(small << (8))             // Found: the amount in parentheses.
	use(int8(count) << 8)         // Found: a conversion's result.
	use(small << count)           // Not found: no constant.
	use((small << 8) << 8)        // Found twice: the inner shift and the outer, of an int8 too.
	use(small << 8.0)             // Not found: a constant written as a float is no integer to ExprToInt.
	use(small + 8)                // Not found: no shift.
	use(small < 8, small&^8 == 0) // Not found: no shift.
}

func assignments(small int8, value holder, pointer *uint16, values [2]uint32) {
	small <<= 8        // Found.
	small >>= 7        // Not found.
	value.value <<= 16 // Found: a field.
	*pointer >>= 16    // Found: through a pointer.
	values[0] <<= 32   // Found: an element.
	small += 8         // Not found: no shift.
	use(small, value, values)
}

func platformSized(number int, unsigned uint, pointer uintptr) {
	use(number<<64, unsigned>>64, pointer<<64) // Not found: their width depends on the platform.
	number <<= 64                              // Not found.
	use(number)
}

// Not found: a type parameter's underlying type is its constraint.
func generic[Small ~int8](value Small) {
	use(value << 8)
}
