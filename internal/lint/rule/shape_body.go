package rule

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// ElidedBodyRange is the body-cut definition shared by SignatureEntry.Syntax and readers that must
// reject foreign body reads. The half-open range includes leading trivia before the body's first token.
// The compiler exposes no Body() for static blocks on this base, so SignatureEntry retains them.
// Sharing this definition deliberately preserves that behavior.
// Function headers/defaults are outside their own body, but may contain another function's body.
func ElidedBodyRange(node *ast.Node) (start, end int, cut bool) {
	if ast.IsFunctionLikeDeclaration(node) || ast.IsClassStaticBlockDeclaration(node) {
		if body := node.Body(); body != nil {
			return body.Pos(), body.End(), true
		}
	}
	return 0, 0, false
}
