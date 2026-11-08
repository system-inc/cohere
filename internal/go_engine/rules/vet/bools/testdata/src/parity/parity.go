// Package parity holds every branch of bools' judgment and the shapes most likely to read differently between the
// original and the port. TestPortMatchesOriginalFindingForFinding runs both over it; it has no wants, since the two
// sides' findings are compared with each other.
package parity

import (
	"io"
	. "math"
	"strings"
	. "unsafe"
)

type record struct {
	name  string
	count int
}

type pair[First comparable, Second comparable] struct {
	first  First
	second Second
}

type alias = int

type named int

func (value named) method() int { return int(value) }

const limit = 10

const typedLimit named = 3

var global int

func identity[Value any](value Value) Value { return value }

// Each message: redundant or and, suspect or and, beside the clean case.
func messages(number int, other int) {
	_ = number == 1 || number == 1
	_ = number == 1 && number == 1
	_ = number != 1 || number != 2
	_ = number == 1 && number == 2
	_ = number == 1 || number == 2
	_ = number != 1 && number != 2
	_ = number == 1 || other == 1
	_ = number != 1 || other != 2
}

// Where the constant stands: on the right, on the left, on both sides, and on neither.
func constantSides(number int, other int, text string) {
	_ = number != limit || number != 2
	_ = limit != number || 2 != number
	_ = number != typedLimit.method() || number != 2
	_ = named(number) != typedLimit || named(number) != 4
	_ = "a" != "b" || "a" != "c"
	_ = "a" != "b" || "c" != "b"
	_ = number != other || number != 2
	_ = number != other || number != other
	_ = text != "x" || text != "y" || text != "x"
	_ = MaxInt8 != number || MaxInt16 != number
	_ = uintptr(number) != Sizeof(number) || uintptr(number) != 2
	_ = uintptr(number) != Sizeof(number) || uintptr(number) != Sizeof(other)
}

// Parentheses, nesting, and the other operator inside a chain.
func nesting(number int, first bool, second bool) {
	_ = (number == 1) || (number == 1)
	_ = (number == 1) || number == 1
	_ = number == 1 || (first && (number == 1 || number == 1))
	_ = (first && second) || (first && second)
	_ = (first || second) && (first || second)
	_ = first && (second || first) && first
	_ = !first || !first
	_ = !(first && second) || !(first && second)
	_ = first || (second || (first || (second || first)))
	_ = (number != 1 || number != 2) && (number != 1 || number != 2)
	_ = first == second || first == second
	_ = first != true || first != false
	_ = first == true && first == false
}

// Side effects split the chain into runs, judged each on its own.
func effects(number int, values []int, channel chan int, function func() int) {
	_ = number == 1 || function() == 1 || number == 1
	_ = number == 1 || number == 1 || function() == 1 || number == 1 || number == 1
	_ = function() == 1 || function() == 1
	_ = number != 1 || function() == 1 || number != 2
	_ = number != 1 || number != 2 || <-channel == 1
	_ = <-channel == 1 || number == 1 || number == 1
	_ = len(values) == 1 || len(values) == 1
	_ = cap(values) == 1 || cap(values) == 1
	_ = max(number, 2) == 1 || max(number, 2) == 1
	_ = min(number, 2) == 1 || min(number, 2) == 1
	_ = real(complex(1, 2)) == float64(number) || real(complex(1, 2)) == float64(number)
	_ = imag(complex(float64(number), 2)) == 1 || imag(complex(float64(number), 2)) == 1
	_ = new(int) == nil || new(int) == nil
	_ = make([]int, number) == nil || make([]int, number) == nil
	_ = len(make(map[string]int)) == 0 || len(make(map[string]int)) == 0
	_ = len(append(values, 1)) == 1 || len(append(values, 1)) == 1
	_ = copy(values, values) == 1 || copy(values, values) == 1
	_ = recover() == nil || recover() == nil
	_ = len(values[function():]) == 1 || len(values[function():]) == 1
	_ = len(values) == 1 || len(<-make(chan []int)) == 1 || len(values) == 1
	_ = identity(number) == 1 || identity(number) == 1
	_ = identity[int](number) == 1 || identity[int](number) == 1
	_ = named(number).method() == 1 || named(number).method() == 1
	_ = (function)() == 1 || (function)() == 1
	_ = Sizeof(number) == 1 || Sizeof(number) == 1
	_ = Abs(float64(number)) == 1 || Abs(float64(number)) == 1
	_ = strings.ToUpper("a") == "A" || strings.ToUpper("a") == "A"
}

// A builtin's name shadowed by a function variable is a call like any other.
func shadowed(values []int) {
	len := func(values []int) int { return 0 }
	_ = len(values) == 1 || len(values) == 1
	_ = cap(values) == 1 || cap(values) == 1
}

// Conversions, and type syntax, which the walk does not descend into.
func conversions(number int, value any, pointer *int, function func() int) {
	_ = alias(number) == 1 || alias(number) == 1
	_ = named(number) == 1 || named(number) == 1
	_ = (named)(number) == 1 || (named)(number) == 1
	_ = []int(nil) == nil || []int(nil) == nil
	_ = map[string]int(nil) == nil || map[string]int(nil) == nil
	_ = (chan int)(nil) == nil || (chan int)(nil) == nil
	_ = (func() int)(function) == nil || (func() int)(function) == nil
	_ = (*int)(pointer) == nil || (*int)(pointer) == nil
	_ = interface{ Read([]byte) (int, error) }(nil) == nil || interface{ Read([]byte) (int, error) }(nil) == nil
	_ = struct{ field int }{1} == struct{ field int }{1} || struct{ field int }{1} == struct{ field int }{1}
	_ = [...]int{1, 2} == [2]int{1, number} || [...]int{1, 2} == [2]int{1, number}
	_ = value.(int) == 1 || value.(int) == 1
	_ = value.(interface{ method() int }) == nil || value.(interface{ method() int }) == nil
	_ = *pointer == 1 || *pointer == 1
	_ = &global == pointer || &global == pointer
	_ = -number == 1 || -number == 1
	_ = ^number == 1 || ^number == 1
}

// Function literals have no effects until called, and the walk does not look inside them.
func literals(number int, function func() int) {
	_ = len([]func() int{func() int { return function() }}) == number ||
		len([]func() int{func() int { return function() }}) == number
	_ = func() int { return number }() == 1 || func() int { return number }() == 1
	_ = func() bool {
		return number == 1 || number == 1
	}()
	closure := func() bool { return number != 1 || number != 2 }
	_ = closure
}

// Composite literals, keyed and not, generic and not, and the selectors, indexes and slices around them.
func composites(number int, values []int, table map[string]int, item record, items []record, matrix [][]int) {
	_ = item == record{name: "a", count: number} || item == record{name: "a", count: number}
	_ = item == record{"a", 1} || item == record{"a", 1}
	_ = item.count == 1 || item.count == 1
	_ = item.count != 1 || item.count != 2
	_ = items[0].name == "a" && items[0].name == "b"
	_ = values[number] == 1 || values[number] == 1
	_ = table["key"] == 1 || table["key"] == 1
	_ = len(values[1:number]) == 1 || len(values[1:number]) == 1
	_ = len(values[1:2:3]) == 1 || len(values[1:2:3]) == 1
	_ = matrix[number][0] != 1 || matrix[number][0] != 2
	_ = pair[int, string]{1, "a"} == pair[int, string]{number, "a"} ||
		pair[int, string]{1, "a"} == pair[int, string]{number, "a"}
	_ = pair[int, string]{first: number}.first == 1 || pair[int, string]{first: number}.first == 1
}

// Method values and expressions, and embedded fields reached through promotion.
type embedding struct {
	record
	named
}

func methods(value named, outer embedding, pointer *embedding) {
	_ = value.method == nil || value.method == nil
	_ = named.method == nil || named.method == nil
	_ = outer.count == 1 || outer.count == 1
	_ = outer.count != 1 || outer.count != 2
	_ = outer.record.count == 1 && outer.count == 2
	_ = pointer.name != "a" || pointer.name != "b"
	_ = outer.method() == 1 || outer.method() == 1
	_ = outer.named.method == nil || outer.named.method == nil
}

// Generic functions: type parameters, their constraints, and instantiations.
func generic[Number interface{ ~int | ~int64 }, Text ~string](number Number, other Number, text Text) {
	_ = number == 1 || number == 1
	_ = number != 1 || number != 2
	_ = number == 1 && number == 2
	_ = number != other || number != other
	_ = Text("a") != text || Text("b") != text
	_ = Number(1) != number || Number(2) != number
	_ = pair[Number, Text]{number, text} == pair[Number, Text]{number, text} ||
		pair[Number, Text]{number, text} == pair[Number, Text]{number, text}
	_ = identity[Number](number) == 1 || identity[Number](number) == 1
	_ = len(text) == 1 || len(text) == 1
}

// Chains in every kind of place an expression stands.
func places(number int, reader io.Reader) bool {
	if number == 1 || number == 1 {
		return true
	}
	for number != 1 || number != 2 {
		break
	}
	switch {
	case number == 1 && number == 2:
	}
	go func() { _ = number == 1 || number == 1 }()
	defer func() { _ = reader == nil || reader == nil }()
	values := []bool{number == 1 || number == 1}
	_ = map[bool]bool{number == 1 || number == 1: number != 1 || number != 2}
	_ = values
	return number == 1 || number == 1
}

// Operands are compared as printed, without their comments, and across line breaks.
func printing(number int) {
	_ = number /* one */ == 1 || number == 1
	_ = number != /* one */ 1 || number != 2
	_ = number ==
		1 || number ==
		1
	_ = number == 1 || number ==
		1
}

// Equality against nil, and comparisons of errors, which the original leaves alone when the values differ.
func errorValues(failure error) {
	_ = failure != nil || failure != io.EOF
	_ = failure != nil || failure != nil
	_ = failure == nil && failure == io.EOF
	_ = failure == io.EOF || failure == io.EOF
}

// A long chain with every repeat after the first reported, across runs.
func longChain(number int, other int, function func() int) {
	_ = other == 0 ||
		number == 1 ||
		number != 3 ||
		function() == 1 ||
		other == 0 ||
		number == 1 ||
		number == 1 ||
		number != 4 ||
		number != 5 ||
		other == 0
}

// Floating point and complex constants.
func floats(value float64, complexValue complex128) {
	_ = value != 1.5 || value != 2.5
	_ = value != Pi || value != E
	_ = complexValue != 1i || complexValue != 2i
	_ = value == 1e3 && value == 1000
}
