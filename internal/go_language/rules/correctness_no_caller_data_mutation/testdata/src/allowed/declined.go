package allowed

// The writes below reach the caller's data, and the rule declines each by design: it judges a direct write at a
// declared function's own parameter, and a write it would have to follow the data further to see is left alone.

type Holder struct {
	*Record
}

type Nested struct {
	Inner Record
}

type Stack []int

func (stack *Stack) Push(value int) {
	*stack = append(*stack, value)
}

// A function literal's parameter belongs to the code that calls it, on the same page.
func literals(records []*Record) {
	each(records, func(record *Record) {
		record.Name = "seen"
	})
	reset := func(values []int) {
		values[0] = 0
	}
	reset(make([]int, 1))
}

func each(records []*Record, visit func(*Record)) {
	for _, record := range records {
		visit(record)
	}
}

// A second reach: a field of a field, an element's field, or an element behind a pointer to a slice or map.
func deeper(nested *Nested, records []Record, values *[]int, counts *map[string]int) {
	nested.Inner.Name = "inner"
	records[0].Name = "first"
	(*values)[0] = 1
	(*counts)["key"] = 1
}

// A struct passed by value is the function's copy, even with a promoted field behind its embedded pointer.
func embeddedPointer(holder Holder) {
	holder.Name = "through the embedded pointer"
}

// A parameter the function reassigns may be the function's own copy by the time it is written.
func reassigned(values []int, counts map[string]int, record *Record) {
	values = append(values, 1)
	values[0] = 2
	counts = map[string]int{}
	counts["fresh"] = 1
	record = &Record{}
	record.Name = "fresh"
}

func reusedByDefine(values []int) []int {
	values, extra := grow(values)
	values[0] = extra
	return values
}

func grow(values []int) ([]int, int) {
	return append(values, 0), len(values)
}

// Taking the address lets something else reassign it, and so does a pointer method on a value, which takes it too.
func addressTaken(values []int, stack Stack) {
	growInPlace(&values)
	values[0] = 1
	stack.Push(1)
	stack[0] = 2
}

// Mutates: values the caller's slice grows by one in place
func growInPlace(values *[]int) {
	*values = append(*values, 0)
}

func rangedOver(values []int, sources [][]int) {
	for _, values = range sources {
		values[0] = 1
	}
}
