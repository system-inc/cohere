// Package builtins holds the universe's names used as the language predeclares them, parenthesized and plain, beside
// the same names a block declares for itself and names that only resemble them, for staticcheck's Builtin pattern and
// the port to judge side by side.
package builtins

type record struct {
	len  int
	size int
}

func (record) cap() int { return 0 }

const (
	first = iota
	second
)

func universal(values []int, table map[string]int, text string) {
	var pointer *int
	_ = pointer == nil
	_ = pointer != (nil)
	_ = len(values)
	_ = (len)(values)
	_ = (cap)(values)
	_ = append(values, 1)
	_ = make([]int, 0)
	_ = new(int)
	copy(values, values)
	delete(table, text)
	clear(table)
	_ = string(rune(65))
	_ = (string)(rune(66))
	_ = true
	_ = (false)
	_ = min(1, 2)
	var failure error
	_ = failure
	var anything any = 1
	_ = anything
	println(first, second)
	_ = values
}

func lookalikes(item record, values []int) {
	_ = item.len
	_ = item.cap()
	_ = record{len: 1, size: 2}
}

func shadowedInBlock(values []int) {
	{
		len := func(values []int) int { return -1 }
		_ = len(values)
		_ = (len)(values)
	}
	{
		nil := 0
		_ = nil
	}
	{
		string := "text"
		_ = string
	}
	{
		var cap, append = 1, 2
		_, _ = cap, append
	}
}

func generic[Element any](values []Element) int {
	return len(values) + cap(values)
}
