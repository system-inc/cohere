// Package order holds two comparisons with nil of a function's result, for the order SA4023 asks its questions in:
// the nilness of the callee first, and its triviality only when the nilness leaves the comparison flagged.
package order

type failure struct{}

func (*failure) Error() string { return "failure" }

// typedNil never returns a nil interface but always holds a typed nil, after real work, so the nilness flags its
// comparison, its triviality is asked, and it is not trivial: the comparison is reported.
func typedNil() error {
	println("not trivial")
	var value *failure
	return value
}

// mayBeNil may return a nil interface, so the nilness leaves its comparison alone and its triviality is never asked.
func mayBeNil(failed bool) error {
	println("not trivial")
	if failed {
		return &failure{}
	}
	return nil
}

func comparesTypedNil() bool {
	return typedNil() != nil
}

func comparesMayBeNil(failed bool) bool {
	return mayBeNil(failed) != nil
}
