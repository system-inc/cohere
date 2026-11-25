// Package parity holds every shape S1032's original and its port branch on, and the near miss of each, where the two
// agree: each helper, the calls a permissible call silences before and after it and in the functions around it, the
// arguments that make a call permissible, the callees that are no call of sort.Sort, and function literals at every
// depth, each judged once for each function around it and reported once.
package parity

import "sort"

// ownInts sorts itself, so a call of sort.Sort on it is permissible.
type ownInts []int

func (values ownInts) Len() int                        { return len(values) }
func (values ownInts) Less(first int, second int) bool { return values[first] < values[second] }
func (values ownInts) Swap(first int, second int) {
	values[first], values[second] = values[second], values[first]
}

// holder has a helper slice for a field and a method's result.
type holder struct {
	values sort.IntSlice
}

func (held holder) helper() sort.IntSlice { return held.values }

func helperResult() sort.IntSlice { return nil }

// Both find each helper.
func eachHelper(values []int, floats []float64, texts []string) {
	sort.Sort(sort.IntSlice(values))
	sort.Sort(sort.Float64Slice(floats))
	sort.Sort(sort.StringSlice(texts))
}

// Both find a call whose callee is parenthesized, deferred or started, and of a literal or nil.
func calleeShapes(values []int) {
	(sort.Sort)(sort.IntSlice(values))
	defer sort.Sort(sort.IntSlice(values))
	go sort.Sort(sort.StringSlice(nil))
	sort.Sort(sort.Float64Slice([]float64{2, 1}))
	sort.Sort(sort.IntSlice(values[1:]))
}

// A method declaration is judged as a function is.
func (values ownInts) method(other []int) {
	sort.Sort(sort.IntSlice(other))
}

// A permissible call after a finding silences it.
func permissibleAfter(values []int, own ownInts) {
	sort.Sort(sort.IntSlice(values))
	sort.Sort(own)
}

// A permissible call before a finding silences it.
func permissibleBefore(values []int) {
	sort.Sort(ownInts(values))
	sort.Sort(sort.IntSlice(values))
	sort.Sort(sort.StringSlice(nil))
}

// A helper wrapped by sort.Reverse is no conversion, so it is permissible.
func reversed(values []int) {
	sort.Sort(sort.Reverse(sort.IntSlice(values)))
	sort.Sort(sort.IntSlice(values))
}

// A conversion to sort.Interface is to no helper, so it is permissible.
func toInterface(values []int, own ownInts) {
	sort.Sort(sort.Interface(own))
	sort.Sort(sort.IntSlice(values))
}

// A method's result of a helper's type is no conversion, so it is permissible.
func methodResult(held holder, values []int) {
	sort.Sort(held.helper())
	sort.Sort(sort.IntSlice(values))
}

// A method expression's result, whose callee is a selector, is no conversion either.
func methodExpressionResult(held holder, values []int) {
	sort.Sort(holder.helper(held))
	sort.Sort(sort.IntSlice(values))
}

// A function's result of a helper's type is permissible.
func functionResult(values []int) {
	sort.Sort(helperResult())
	sort.Sort(sort.IntSlice(values))
}

// A field of a helper's type is no call, so it is permissible.
func fieldValue(held holder, values []int) {
	sort.Sort(held.values)
	sort.Sort(sort.IntSlice(values))
}

// A composite literal of a helper is no call, so it is permissible.
func compositeLiteral(values []int) {
	sort.Sort(sort.IntSlice{3, 1, 2})
	sort.Sort(sort.IntSlice(values))
}

// A variable holding sort.Sort calls no sort.Sort, so its own type is not permissible, and the finding stands.
func variableCallee(values []int) {
	sorter := sort.Sort
	sorter(ownInts(values))
	sort.Sort(sort.IntSlice(values))
}

// sort.Stable is no sort.Sort, so neither finds it, and the finding beside it stands.
func stableSort(values []int, own ownInts) {
	sort.Stable(sort.IntSlice(values))
	sort.Stable(own)
	sort.Sort(sort.IntSlice(values))
}

// A permissible call in a function literal silences the function around it, and the literal itself.
func innerPermissible(values []int, own ownInts) {
	sort.Sort(sort.IntSlice(values))
	func() {
		sort.Sort(sort.StringSlice(nil))
		sort.Sort(own)
	}()
}

// A permissible call around a function literal leaves the literal's own call, found by the literal alone.
func outerPermissible(values []int, own ownInts) {
	sort.Sort(own)
	func() {
		sort.Sort(sort.IntSlice(values))
	}()
}

// A call nested two literals deep is judged three times and found once.
func nestedTwice(values []int) {
	func() {
		func() {
			sort.Sort(sort.IntSlice(values))
		}()
		sort.Sort(sort.Float64Slice(nil))
	}()
}

// A function literal at package level is judged on its own.
var _ = func(values []int) {
	sort.Sort(sort.IntSlice(values))
}

// A conversion of a type parameter's value is a conversion to the helper.
func generic[Values ~[]int](values Values) {
	sort.Sort(sort.IntSlice(values))
}

// A type parameter that sorts itself is permissible.
func genericOwn[Sortable sort.Interface](value Sortable, values []int) {
	sort.Sort(value)
	sort.Sort(sort.IntSlice(values))
}
