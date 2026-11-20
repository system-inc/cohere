package lazyreach

import "context"

var _ context.Context

// ctx is the package's first abbreviated name, and its advice turns on the package reaching one named context.
func describe(value any) {
	switch ctx := value.(type) { // want `Identifier "ctx" should not be abbreviated\. Use a descriptive name ending in "Context", since "context" is already Go's name for something else\.`
	default:
		_ = ctx
	}
}
