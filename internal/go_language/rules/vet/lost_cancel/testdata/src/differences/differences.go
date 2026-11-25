// Package differences holds where the port and the original disagree, each finding the original alone makes recorded
// on its line as dropped, with the reason on the line above. Where a call's qualifier is no package name, the
// original falls back on its spelling, a heuristic for an import that failed, and takes whatever is named context
// for the package. lostcancel never runs over a package that does not type-check, so its imports never failed, and
// what is named context here is a variable, a parameter or a type, never the package.
package differences

import "context"

var background = context.Background()

var condition bool

// canceller has a method named WithCancel, as the context package has a function of that name.
type canceller struct{}

func (canceller) WithCancel(parent context.Context) (context.Context, func()) {
	return parent, func() {}
}

// A local variable named context shadows the package.
func variableNamedContext() {
	context := canceller{}
	// Wrong: context is a canceller, whose method returns a function that cancels nothing.
	ctx, _ := context.WithCancel(background) // port drops: the cancel function returned by context.WithCancel should be called, not discarded, to avoid a context leak
	// Wrong: what cancel holds comes from canceller's method, not from the context package.
	_, cancel := context.WithCancel(ctx) // port drops: the cancel function is not used on all paths (possible context leak)
	if condition {
		cancel()
	}
	// Wrong: the return leaks no context, since cancel was never one of the context package's.
} // port drops: this return statement may be reached without using the cancel var defined on line 27

// A parameter named context shadows the package.
func parameterNamedContext(context canceller) {
	// Wrong: context is the parameter, a canceller, not the package.
	ctx, _ := context.WithCancel(background) // port drops: the cancel function returned by context.WithCancel should be called, not discarded, to avoid a context leak
	_ = ctx
}

// A local type named context, its method called as a method expression.
func typeNamedContext() {
	type context = canceller
	// Wrong: context.WithCancel is canceller's method expression, not the package's function.
	ctx, _ := context.WithCancel(canceller{}, background) // port drops: the cancel function returned by context.WithCancel should be called, not discarded, to avoid a context leak
	_ = ctx
}
