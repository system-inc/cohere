// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both render the sides with staticcheck's report.Render and judge their side
// effects with code.MayHaveSideEffects or its port, proven against it, given the same purity pass. These are the sides
// where the purity judgment or the rendering is least plain, instantiated generic calls, method values, nested calls
// and literals, where a predicate built from the purity pass otherwise than the original reads it would show here as a
// finding one side makes and the other does not.
package differences

type pair struct {
	left  int
	right int
}

func identity[T any](value T) T { return value }

func sum(first int, second int) int { return first + second }

func (receiver pair) total() int { return receiver.left + receiver.right }

// Found by both: nested pure calls, and a method the purity pass holds, called on a literal. Found by neither: an
// explicit instantiation, whose function the pass does not hold.
func pureShapes(values []int) {
	values[sum(sum(1, 2), 3)] = values[sum(sum(1, 2), 3)]
	values[identity[int](1)] = values[identity[int](1)]
	values[pair{1, 2}.total()] = values[pair{1, 2}.total()]
}

// Found by neither: a method value called, and a call through a function variable, which no purity fact covers.
func impureShapes(values []int, function func() int) {
	values[pair{}.total()+function()] = values[pair{}.total()+function()]
	values[(pair{}).total()] = values[pair{}.total()]
}
