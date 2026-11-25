// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the parent,
// the key and the value, and context.WithValue held in a variable, whose callee is known only once the variable is
// lifted, or not at all when two functions meet. The port and the original find the same here, so the fixture records
// no difference.
package differences

import "context"

func parentKeyAndValue() (context.Context, string, any) {
	return nil, "key", nil
}

func parentListAndValue() (context.Context, []int, any) {
	return nil, nil, nil
}

func statements(parent context.Context) {
	defer context.WithValue(parent, "deferred", nil)
	go context.WithValue(parent, []int{}, nil)
	defer func() {
		context.WithValue(parent, struct{}{}, nil)
	}()
}

func multipleValues() {
	context.WithValue(parentKeyAndValue())
	context.WithValue(parentListAndValue())
}

func lifted(condition bool, parent context.Context) {
	with := context.WithValue
	if condition {
		with = context.WithValue
	}
	with(parent, "lifted", nil)
	choose := context.WithValue
	if condition {
		choose = func(parent context.Context, key any, value any) context.Context { return parent }
	}
	choose(parent, "chosen", nil)
}
