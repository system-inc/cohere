// Package differences holds the shapes where the port and the original could most plausibly part, the bound of the
// length check a constant that is negative, or zero but no integer, and records each place they do. The original
// reads a bound as zero only when it is an integer of value zero and takes every other bound to be positive; the port
// reads the value, so where a nil one's length of zero makes the length check answer differently from the nil check,
// the port drops the original's finding, and where it makes it answer the same, the port finds what the original
// leaves alone. The names are short so each record fits on its finding's line.
package differences

const (
	minus    = -1
	minusTwo = -2.0
	nought   = 0.0
	two      = 2.0
)

func negative(row []int, set map[string]int, bus chan int) {
	// len is never at most -1, so this is row == nil, and without the nil check it is never true.
	if row == nil || len(row) <= minus { // port drops: should omit nil check; len() for nil slices is defined as zero
	}
	// len is never below -1, so this is set == nil, and without the nil check it is never true.
	if set == nil || len(set) < minus { // port drops: should omit nil check; len() for nil maps is defined as zero
	}
	// len is always above -1, so this is bus != nil, and without the nil check it is always true.
	if bus != nil && len(bus) > minus { // port drops: should omit nil check; len() for nil channels is defined as zero
	}
	// len is always at least -1, so this is set != nil, and without the nil check it is always true.
	if set != nil && len(set) >= minus { // port drops: should omit nil check; len() for nil maps is defined as zero
	}
	// len is always above -2, so this is row != nil, and without the nil check it is always true.
	if row != nil && len(row) > minusTwo { // port drops: should omit nil check; len() for nil slices is defined as zero
	}

	// Never true either way, so both report it.
	if row != nil && len(row) == minus {
	}
	// Neither is redundant here, so neither reports them.
	if row == nil || len(row) == minus {
	}
	if row != nil && len(row) != minus {
	}
}

func zeroNoInteger(row []int, set map[string]int, bus chan int) {
	// len is never below 0, so this is row == nil, and without the nil check it is never true.
	if row == nil || len(row) < nought { // port drops: should omit nil check; len() for nil slices is defined as zero
	}
	// A nil one is of length 0, so without the nil check a nil one passes too.
	if set != nil && len(set) == nought { // port drops: should omit nil check; len() for nil maps is defined as zero
	}
	// len is always at least 0, so this is set != nil, and without the nil check it is always true.
	if set != nil && len(set) >= nought { // port drops: should omit nil check; len() for nil maps is defined as zero
	}
	// A nil one is of length 0, so without the nil check a nil one passes too.
	if set != nil && len(set) == 0.0 { // port drops: should omit nil check; len() for nil maps is defined as zero
	}
	// A nil one is of length 0, so without the nil check a nil one passes too.
	if row != nil && len(row) == '\x00' { // port drops: should omit nil check; len() for nil slices is defined as zero
	}
	// len is always at least 0, so this is set != nil, and without the nil check it is always true.
	if set != nil && len(set) >= 0i { // port drops: should omit nil check; len() for nil maps is defined as zero
	}

	// A nil one is of length 0, so the length check alone already passes it.
	if row == nil || len(row) == nought { // port finds: should omit nil check; len() for nil slices is defined as zero
	}
	// A nil one is of length 0, so the length check alone already passes it.
	if bus == nil || len(bus) == 0.0 { // port finds: should omit nil check; len() for nil channels is defined as zero
	}
	// A nil one is of length 0, so the length check alone already refuses it.
	if set != nil && len(set) != '\x00' { // port finds: should omit nil check; len() for nil maps is defined as zero
	}
	// A nil one is of length 0, so the length check alone already passes it.
	if row == nil || len(row) == 0i { // port finds: should omit nil check; len() for nil slices is defined as zero
	}

	// Where the value is zero or positive the original's reading is the arithmetic's, so both report these.
	if row == nil || len(row) <= nought {
	}
	if row != nil && len(row) > 0.0 {
	}
	if row != nil && len(row) >= two {
	}
	if row == nil || len(row) < two {
	}
	if row != nil && len(row) == two {
	}
	// And neither reports these.
	if row == nil || len(row) == two {
	}
	if row != nil && len(row) != two {
	}
}
