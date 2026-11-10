package javascript

import "github.com/system-inc/cohere/internal/format/doc"

// print/module-declaration.js.

/*
- `TSModuleDeclaration` (TypeScript)
*/
// printModuleDeclaration is upstream's printModuleDeclaration.
func printModuleDeclaration(path *Path, options *Options, print PrintFunction) Doc {
	current := node(path)

	kind := ""
	if current.String("kind") != "global" {
		kind = current.String("kind") + " "
	}
	var body Doc
	if current.Child("body") != nil {
		body = concatIn(path, doc.Text(" "), groupIn(path, print("body", nil)))
	} else {
		body = printSemicolon(options)
	}
	return concatIn(path, printDeclareToken(path),
		textDoc(kind),
		print("id", nil),
		body,
	)
}
