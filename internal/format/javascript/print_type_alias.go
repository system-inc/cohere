package javascript

import "github.com/system-inc/cohere/internal/format/doc"

// print/type-alias.js.

// printTypeAlias is upstream's printTypeAlias.
/*
- `DeclareTypeAlias`(flow)
- `TypeAlias`(flow)
- `TSTypeAliasDeclaration`(TypeScript)
*/
func printTypeAlias(path *Path, options *Options, print PrintFunc) Doc {
	current := node(path)
	parts := concatIn(path, printDeclareToken(path),
		doc.Text("type "),
		print("id", nil),
		print("typeParameters", nil),
	)

	rightPropertyName := "right"
	if current.Is("TSTypeAliasDeclaration") {
		rightPropertyName = "typeAnnotation"
	}
	return concatIn(path, printAssignment(path, options, print, parts, doc.Text(" ="), rightPropertyName),
		printSemicolon(options),
	)
}
