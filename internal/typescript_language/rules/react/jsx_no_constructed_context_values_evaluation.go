package react

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// jsxNoConstructedContextValuesEvaluation is what one expression can evaluate to.
type jsxNoConstructedContextValuesEvaluation struct {
	// fresh is set when some evaluation is proven to be a new identity.
	fresh *jsxNoConstructedContextValuesInstability

	// other is set when some evaluation can instead be an object that already existed: a value read
	// from somewhere this walk cannot see into. When both are set, the fresh value may have been
	// stored on its way out and read back on a later call through the other path, which is exactly
	// what a cache does, so it proves nothing unless it never escapes.
	other bool

	// holders are the `const` (or assigned local) declarations the fresh value passed through in
	// the current function, which the escape check reads when `other` is set.
	holders []*ast.Node
}

// combine merges two evaluations that are alternatives, as the two branches of `?:` or the two
// sides of `??` are. The left finding wins for the message; the holders of both are kept, so an
// escape on either side silences the pair.
func (evaluation jsxNoConstructedContextValuesEvaluation) combine(
	alternative jsxNoConstructedContextValuesEvaluation,
) jsxNoConstructedContextValuesEvaluation {
	fresh := evaluation.fresh
	if fresh == nil {
		fresh = alternative.fresh
	}
	return jsxNoConstructedContextValuesEvaluation{
		fresh:   fresh,
		other:   evaluation.other || alternative.other,
		holders: append(append([]*ast.Node{}, evaluation.holders...), alternative.holders...),
	}
}
