// Package contextcalls holds contexts named ctx, matched by their type: a context.Context parameter named for its
// caller, a local named for the call that made it when it is written once by a call in the table, and every other
// ctx keeping the advice toward a descriptive name.
//
// Its golden shows each offered rename at the declaration alone, with the uses still spelled the old way: analysistest
// applies the fix's one edit, and the engine's rename fix renames every use itself, through the types.
package contextcalls

import "context"

// A parameter's context is its caller's.
func serve(ctx context.Context) error { // want `Identifier "ctx" should not be abbreviated\. Use "callerContext", named for where the context came from\.`
	return ctx.Err()
}

// A method's parameter is ours to name, since an interface it satisfies reads no parameter's name.
type worker struct{}

func (worker) run(ctx context.Context) error { // want `Identifier "ctx" should not be abbreviated\. Use "callerContext", named for where the context came from\.`
	return ctx.Err()
}

// A local from context.WithCancel is named for that call.
func start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent) // want `Identifier "ctx" should not be abbreviated\. Use "cancelContext", named for where the context came from\.`
	defer cancel()
	_ = ctx
}

// A ctx written twice is two definitions a single rename can't split, so it keeps the advice.
func twice(parent context.Context) {
	ctx, cancel := context.WithCancel(parent) // want `Identifier "ctx" should not be abbreviated\. Use a descriptive name ending in "Context", since "context" is already Go's name for something else\.`
	defer cancel()
	ctx = context.WithoutCancel(ctx)
	_ = ctx
}

// A local from a call outside the table names nothing.
func detached(parent context.Context) {
	ctx := context.WithoutCancel(parent) // want `Identifier "ctx" should not be abbreviated\. Use a descriptive name ending in "Context", since "context" is already Go's name for something else\.`
	_ = ctx
}

// The caller's name is already declared where the parameter is, so it keeps the advice.
func taken(ctx context.Context) { // want `Identifier "ctx" should not be abbreviated\. Use a descriptive name ending in "Context", since "context" is already Go's name for something else\.`
	callerContext := "taken"
	_, _ = ctx, callerContext
}

// The call's name is declared in a scope nested in the local's, which the rename would capture its use in.
func nested(parent context.Context) {
	ctx, cancel := context.WithCancel(parent) // want `Identifier "ctx" should not be abbreviated\. Use a descriptive name ending in "Context", since "context" is already Go's name for something else\.`
	defer cancel()
	if parent != nil {
		cancelContext := ctx
		_ = cancelContext
	}
}

// A parameter the body writes holds the caller's context only until the write.
func rewrapped(ctx context.Context) { // want `Identifier "ctx" should not be abbreviated\. Use a descriptive name ending in "Context", since "context" is already Go's name for something else\.`
	ctx = context.WithoutCancel(ctx)
	_ = ctx
}

// A ctx that isn't a context.Context keeps the advice, a string or a type of ours that only shares the name.
type Context struct{}

func spelled(ctx string) string { // want `Identifier "ctx" should not be abbreviated\. Use a descriptive name ending in "Context", since "context" is already Go's name for something else\.`
	return ctx
}

func own(ctx Context) Context { // want `Identifier "ctx" should not be abbreviated\. Use a descriptive name ending in "Context", since "context" is already Go's name for something else\.`
	return ctx
}
