// Package differences holds the defers where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: a call returning a type parameter constrained to functions, whose underlying
// type is an interface, a call returning an interface a function satisfies, and a method value deferred whole.
package differences

type caller interface {
	call()
}

type action func()

func (action) call() {}

func returnsTyped[Function ~func()](function Function) Function { return function }

func returnsInterface() caller { return action(func() {}) }

func returnsAction() action { return func() {} }

func plausible[Function ~func()](function Function) {
	defer returnsTyped(function)
	defer returnsTyped(action(func() {}))
	defer returnsInterface()
	defer returnsAction().call()
	defer returnsAction()
}
