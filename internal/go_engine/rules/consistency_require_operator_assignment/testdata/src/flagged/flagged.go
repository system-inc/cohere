package flagged

import "counter"

type holder struct {
	total int
	inner struct{ depth float64 }
}

type Number interface{ ~int | ~float64 }

const first = 0

var tally int

// Every operator with a compound form, the target on the left.
func operators(count int, step int, flags uint, mask uint) {
	count = count + step  // want `names .count. twice.*\+=`
	count = count - step  // want `names .count. twice.*-=`
	count = count * step  // want `names .count. twice.*\*=`
	count = count / step  // want `names .count. twice.*/=`
	count = count % step  // want `names .count. twice.*%=`
	flags = flags & mask  // want `names .flags. twice.*&=`
	flags = flags | mask  // want `names .flags. twice.*\|=`
	flags = flags ^ mask  // want `names .flags. twice.*\^=`
	flags = flags << step // want `names .flags. twice.*<<=`
	flags = flags >> step // want `names .flags. twice.*>>=`
	flags = flags &^ mask // want `names .flags. twice.*&\^=`
	tally = tally + 1     // want `names .tally. twice`
	_, _ = count, flags
}

// Strings concatenate in order, so appending to the target is its compound form.
func concatenation(text string, suffix string) string {
	text = text + suffix // want `names .text. twice.*\+=`
	text = text + "!"    // want `names .text. twice.*\+=`
	return text
}

// The right operand is kept whole, so a tighter operation or a group after the operator stays as written.
func operands(count int, step int, ratio float64) {
	count = count + step*2        // want `names .count. twice`
	count = count + (step + 1)    // want `names .count. twice`
	count = count - (step - 1)    // want `names .count. twice`
	count = count + step<<1       // want `names .count. twice`
	ratio = ratio * float64(step) // want `names .ratio. twice`
	_, _ = count, ratio
}

// Parentheses are transparent, around the whole operation, the target, or the operand.
func parentheses(count int, step int) int {
	count = (count + step) // want `names .count. twice`
	(count) = count + step // want `names .count. twice`
	count = (count) + step // want `names .count. twice`
	return count
}

// Fields, elements, dereferences and another package's variable are places too.
func places(item holder, pointer *holder, values []int, counts map[string]int, cell *int, index int) {
	item.total = item.total + 1             // want `names .item.total. twice`
	item.inner.depth = item.inner.depth * 2 // want `names .item.inner.depth. twice`
	pointer.total = pointer.total - 1       // want `names .pointer.total. twice`
	values[0] = values[0] + 1               // want `names .values.0.. twice`
	values[index] = values[index] + 1       // want `names .values.index.. twice`
	values[first] = values[0] + 1           // want `names .values.first.. twice`
	values[0x1] = values[1] + 1             // want `names .values.0x1.. twice`
	values[counter.Limit] = values[3] + 1   // want `names .values.counter.Limit.. twice`
	counts["a"] = counts["a"] + 1           // want `names .counts."a".. twice`
	*cell = *cell * 2                       // want `names ..cell. twice`
	counter.Total = counter.Total + 1       // want `names .counter.Total. twice`
	_ = item
}

// A type parameter supports the compound form wherever every type in its set does.
func sum[Value Number](values []Value) Value {
	var total Value
	for _, value := range values {
		total = total + value // want `names .total. twice`
	}
	return total
}

// A loop's post statement is an assignment like any other.
func loop(limit int) {
	for index := 0; index < limit; index = index + 2 { // want `names .index. twice`
	}
}

// An identifier spelled with runes of more than one byte is a variable like any other.
func runes(größe int) int {
	größe = größe + 1 // want `names .größe. twice`
	return größe
}

// The reversed form is reported for an operator that commutes, and is not fixed.
func reversed(count int, flags uint, mask uint) {
	count = 2 * count    // want `names .count. twice.*\*=`
	flags = mask & flags // want `names .flags. twice.*&=`
	flags = mask | flags // want `names .flags. twice.*\|=`
	flags = mask ^ flags // want `names .flags. twice.*\^=`
	_, _ = count, flags
}

// A comment the rewrite would discard leaves the finding without a fix.
func comments(count int, step int) int {
	count = count /* kept */ + step   // want `names .count. twice`
	count = (count + step /* kept */) // want `names .count. twice`
	// A comment after the operator survives the rewrite, so this one is fixed.
	count = count + /* kept */ step // want `names .count. twice`
	return count
}
