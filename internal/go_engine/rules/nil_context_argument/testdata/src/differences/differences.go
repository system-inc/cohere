// Package differences holds the calls where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: the original's false positive, a method expression whose receiver is the nil
// and whose first parameter is a context, and parameters whose types are a context only in part, an alias of
// context.Context, an interface embedding it and a type parameter constrained by it.
package differences

import "context"

type worker struct{}

func (*worker) run(ctx context.Context) {}

type alias = context.Context

type embedding interface {
	context.Context
}

func takesAlias(ctx alias) {}

func takesEmbedding(ctx embedding) {}

func takesConstrained[Context context.Context](ctx Context) {}

func methodExpression(ctx context.Context) {
	(*worker).run(nil, ctx)
}

func plausible() {
	takesAlias(nil)
	takesEmbedding(nil)
	takesConstrained[context.Context](nil)
}
