// Package parity holds every shape S1031's query and judgment branch on, each beside its near miss, for the original
// and the port to judge alike.
package parity

import (
	"fmt"

	"parity/exported"
	. "parity/exported"
	"parity/other"
)

type holder struct {
	values []int
}

type named []int

type namedMap map[string]int

type alias = []string

type mapAlias = map[int]bool

type pointerToArray *[3]int

func listed() []int { return nil }

// Sequence is a function a range statement can range over, which a nil check around stays needed for.
func Sequence(yield func(int) bool) {}

// slices are the plainest finding, a slice checked and ranged over, in every form of range clause.
func slices(values []int, flag bool) {
	if values != nil {
		for range values {
		}
	}
	if values != nil {
		for index := range values {
			_ = index
		}
	}
	if values != nil {
		for index, value := range values {
			_, _ = index, value
		}
	}
	var index, value int
	if values != nil {
		for index, value = range values {
		}
	}
	_, _ = index, value
	// The range clause's variable is declared in the body's scope, so the ranged values is still the parameter.
	if values != nil {
		for _, values := range values {
			_ = values
		}
	}
}

// maps are the other type a nil range runs zero times over.
func maps(counts map[string]int) {
	if counts != nil {
		for key, count := range counts {
			counts[key] = count + 1
		}
	}
}

// types names slices and maps by every kind of name the underlying type is read through.
func types(first named, second namedMap, third alias, fourth mapAlias) {
	if first != nil {
		for range first {
		}
	}
	if second != nil {
		for range second {
		}
	}
	if third != nil {
		for range third {
		}
	}
	if fourth != nil {
		for range fourth {
		}
	}
}

// refused types are those a nil check around a range is not redundant for, or that are not slices or maps.
func refused(channel chan int, pointer *[3]int, pointerNamed pointerToArray, sequence func(func(int) bool)) {
	// A range over a nil channel blocks forever.
	if channel != nil {
		for value := range channel {
			fmt.Println(value)
		}
	}
	// A range over a nil pointer to an array panics, unless it reads no element.
	if pointer != nil {
		for index, value := range pointer {
			_, _ = index, value
		}
	}
	if pointerNamed != nil {
		for range pointerNamed {
		}
	}
	// A range over a nil function calls it.
	if sequence != nil {
		for value := range sequence {
			_ = value
		}
	}
	if Sequence != nil {
		for value := range Sequence {
			_ = value
		}
	}
}

// generic type parameters are judged by every type their type set holds.
func generic[S ~[]E, E any, M ~map[string]E, C ~chan E, P ~*[3]E](slice S, counts M, channel C, pointer P) {
	if slice != nil {
		for range slice {
		}
	}
	if counts != nil {
		for range counts {
		}
	}
	if channel != nil {
		for range channel {
		}
	}
	if pointer != nil {
		for range pointer {
		}
	}
}

// sliceOrNamed has two terms, both slices of one element type.
type sliceOrNamed interface {
	[]int | named
}

func union[T sliceOrNamed](values T) {
	if values != nil {
		for range values {
		}
	}
}

// bodies are what the if's body may be: one range, looked through labels and blocks of one statement.
func bodies(values []int) {
	if values != nil {
	outer:
		for range values {
			break outer
		}
	}
	if values != nil {
		{
			for range values {
			}
		}
	}
	if values != nil {
		{
			{
			inner:
				for range values {
					continue inner
				}
			}
		}
	}
	if values != nil {
	block:
		{
			for range values {
				goto block
			}
		}
	}
	// Two statements, in the body or in a block of it, are not the query's one.
	if values != nil {
		values = append(values, 1)
		for range values {
		}
	}
	if values != nil {
		{
			values = append(values, 1)
			for range values {
			}
		}
	}
	if values != nil {
		for range values {
		}
		fmt.Println()
	}
	// A body with no statement, a counted loop and an empty block are no range.
	if values != nil {
	}
	if values != nil {
		for index := 0; index < len(values); index++ {
		}
	}
	if values != nil {
		{
		}
	}
}

// conditions are the shapes the condition may take and its near misses.
func conditions(values []int, flag bool, it holder) {
	// Parentheses around an operand survive gofmt and are looked through.
	if (values) != nil {
		for range values {
		}
	}
	if values != (nil) {
		for range values {
		}
	}
	// The comparison is an ordered shape.
	if nil != values {
		for range values {
		}
	}
	if values == nil {
		for range values {
		}
	}
	if values != nil && flag {
		for range values {
		}
	}
	if !(values == nil) {
		for range values {
		}
	}
	// A selector is no identifier, though the range is over the same field.
	if it.values != nil {
		for range it.values {
		}
	}
	// A block's nil is no predeclared nil, and the comparison is then between two pointers.
	nil := (*[3]int)(nil)
	pointer := nil
	if pointer != nil {
		for range pointer {
		}
	}
}

// ranged are the near misses of what the range is over.
func ranged(values []int, others []int) {
	if values != nil {
		for range others {
		}
	}
	if values != nil {
		for range values[:] {
		}
	}
	if values != nil {
		for range values[1:] {
		}
	}
	if listed() != nil {
		for range listed() {
		}
	}
}

// branches are what the if may carry beside its condition and body.
func branches(values []int, flag bool) {
	if values != nil {
		for range values {
		}
	} else {
	}
	if values != nil {
		for range values {
		}
	} else {
		fmt.Println()
	}
	if values != nil {
		for range values {
		}
	} else if flag {
	}
	// The inner if of an else if is an if statement of its own.
	if flag {
	} else if values != nil {
		for range values {
		}
	}
	if more := values; more != nil {
		for range more {
		}
	}
}

// packages names a package's variable through a dot import in the condition and by the package's name in the range,
// one object spelled two ways, and its near misses.
func packages() {
	if Values != nil {
		for range exported.Values {
		}
	}
	if Counts != nil {
		for range exported.Counts {
		}
	}
	if Values != nil {
		for range Values {
		}
	}
	if Values != nil {
		for range other.Values {
		}
	}
	if exported.Values != nil {
		for range exported.Values {
		}
	}
}

// shadowedPackage declares Values over the dot import's, so the range by package name is over another object.
func shadowedPackage() {
	Values := []int{1}
	if Values != nil {
		for range exported.Values {
		}
	}
}

// nested are findings in a loop's body, a function literal and a finding's own range.
func nested(values []int, lists [][]int) {
	for range lists {
		if values != nil {
			for range values {
			}
		}
	}
	report := func() {
		if lists != nil {
			for _, list := range lists {
				if list != nil {
					for range list {
					}
				}
			}
		}
	}
	report()
}
