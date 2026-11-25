// Package parity holds every branch of appends's judgment and the shapes most likely to make the port and the
// original differ, each finding beside a clean case. The parity test compares the two over it finding for finding.
package parity

// The judged call: the builtin, given the slice alone.

func sliceAlone(values []string) []string {
	return append(values) // found
}

func sliceLiteralAlone() []int {
	return append([]int{1}) // found
}

func spreadOverLines(values []int) []int {
	return append( // found
		values,
	)
}

// Clean beside it: the builtin given values, one, several or spread.

func oneValue(values []int) []int {
	return append(values, 1)
}

func severalValues(values []int) []int {
	return append(values, 1, 2, 3)
}

func spreadValues(values []int, more []int) []int {
	return append(values, more...)
}

func spreadString(bytes []byte) []byte {
	return append(bytes, "text"...)
}

// The called name in parentheses is still the builtin, and the call is judged.

func parenthesized(values []int) []int {
	return (append)(values) // found
}

func parenthesizedClean(values []int) []int {
	return (append)(values, 1)
}

// Nested calls: each is judged on its own, the outer before the inner.

func nestedInner(values []int) []int {
	return append(append(values), 1) // found
}

func nestedBoth(values []int) []int {
	return append(append(values)) // found, outer and inner
}

func nestedClean(values []int) []int {
	return append(append(values, 1), 2)
}

// A named slice type, an alias and a generic slice are all slices to the builtin.

type names []string

type alias = []string

func namedType(values names) names {
	return append(values) // found
}

func aliasType(values alias) alias {
	return append(values) // found
}

func generic[Slice ~[]Element, Element any](values Slice) Slice {
	return append(values) // found
}

func genericClean[Slice ~[]Element, Element any](values Slice, value Element) Slice {
	return append(values, value)
}

func genericBytes[Bytes ~[]byte](values Bytes) Bytes {
	return append(values, "text"...)
}

// Inside a closure, a method and a package-level initializer, the builtin is judged the same.

func closure(values []int) func() []int {
	return func() []int {
		return append(values) // found
	}
}

type holder struct {
	values []int
}

func (current holder) appended() []int {
	return append(current.values) // found
}

var initialized = append([]int{1, 2}) // found

var initializedClean = append([]int{1, 2}, 3)

// A call whose one argument is a call returning a slice and values passes values, yet it is one argument, and both
// sides judge the syntax, so both report it.

func sliceAndValue() ([]int, int) {
	return []int{1}, 2
}

func multipleValues() []int {
	return append(sliceAndValue()) // found
}

// Names that are not the builtin: a method named append, a field of function type, a parameter and a local
// variable that shadow it. None is judged, given one argument or more.

type appender struct {
	appendFunction func([]int) []int
}

func (current appender) append(values []int) []int {
	return values
}

func method(current appender, values []int) []int {
	return current.append(values)
}

func methodValue(current appender, values []int) []int {
	bound := current.append
	return bound(values)
}

func methodExpression(current appender, values []int) []int {
	return appender.append(current, values)
}

func field(current appender, values []int) []int {
	return current.appendFunction(values)
}

func parameter(append func([]int) []int, values []int) []int {
	return append(values)
}

func local(values []int) []int {
	append := func(values []int) []int { return values }
	return append(values)
}

// A conversion calls nothing, so it resolves to no builtin.

func conversion(values []string) names {
	return names(values)
}

// Other builtins given one argument are not append.

func otherBuiltins(values []int) int {
	return len(values) + cap(values)
}
