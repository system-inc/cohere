package javascript

import "github.com/system-inc/cohere/internal/format/doc"

// print/binary-cast-expression.js.

// printBinaryCastExpression is upstream's printBinaryCastExpression. Upstream's isFlowAsConstExpression
// branch (Flow's AsConstExpression prints "const") is Flow-only; TSAsExpression and
// TSSatisfiesExpression always print their typeAnnotation.
func printBinaryCastExpression(path *Path, options *Options, print PrintFunc) Doc {
	parent := parentOf(path)
	current := node(path)
	key := keyOf(path)
	typeAnnotationDoc := print("typeAnnotation", nil)

	keyword := "as"
	if isSatisfiesExpression(current) {
		keyword = "satisfies"
	}
	parts := []Doc{
		print("expression", nil),
		doc.Text(" "),
		textDoc(keyword),
		doc.Text(" "),
		typeAnnotationDoc,
	}

	if key == "callee" && isCallOrNewExpression(parent) ||
		key == "object" && isMemberExpression(parent) {
		return groupIn(
			path,
			concatIn(path, indentIn(path, concatIn(path, append([]Doc{softline}, parts...)...)), softline),
		)
	}

	return concatIn(path, parts...)
}
