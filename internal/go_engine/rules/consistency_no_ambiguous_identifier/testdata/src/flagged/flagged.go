package flagged

import "slices"

// Every kind of declaration a letter can name is reported where it is declared, once, and nowhere it is used.

const c = 1 // want `^Single-letter identifier "c" is not descriptive enough\. The name is read everywhere it is used`

var v = c + 1 // want `"v"`

type t struct { // want `"t"`
	n int // want `"n"`
}

type list[T any] struct {
	items []T
}

// The receiver is judged; its type parameter, an uppercase letter, is not.
func (l *list[T]) push(item T) { // want `"l"`
	l.items = append(l.items, item)
}

// A lowercase type parameter is judged like any other lowercase letter.
func generic[q any](value q) q { // want `"q"`
	return value
}

func f() {} // want `"f"`

func named() (n int) { // want `"n"`
	n = len("named")
	return n
}

func ranged(items []string) int {
	total := 0
	for i, s := range items { // want `"i"` `"s"`
		total += i + len(s)
	}
	return total
}

func locals() int {
	k := 1    // want `"k"`
	var m = 2 // want `"m"`
	// A := that declares a new name beside an old one declares only the new one, so k is not reported again.
	k, other := 3, 4
	return k + m + other
}

// A parameter name is never part of a Go type, so a function type's parameters are ours like any other.
func callback(handle func(a, b int) int) int { // want `"a"` `"b"`
	return handle(1, 2)
}

// Go keeps no comparator exemption: house style names a comparator's parameters left and right.
func sorted(numbers []int) {
	slices.SortFunc(numbers, func(a, b int) int { // want `"a"` `"b"`
		return a - b
	})
}

// An interface we declare names its methods and their parameters.
type reader interface {
	r()                               // want `"r"`
	Read(p []byte) (n int, err error) // want `"p"` `"n"`
}

// A type switch's name is reported once, at the switch, though every clause holds its own.
func switched(value any) int {
	switch w := value.(type) { // want `"w"`
	case int:
		return w
	case string:
		return len(w)
	}
	return 0
}
