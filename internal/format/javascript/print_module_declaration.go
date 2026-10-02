package javascript

// print/module-declaration.js.

/*
- `TSModuleDeclaration` (TypeScript)
*/
// printModuleDeclaration is upstream's printModuleDeclaration.
func printModuleDeclaration(path *Path, options *Options, print PrintFunc) Doc {
	current := node(path)

	kind := ""
	if current.String("kind") != "global" {
		kind = current.String("kind") + " "
	}
	var body Doc
	if current.Child("body") != nil {
		body = concat(" ", group(print("body", nil)))
	} else {
		body = printSemicolon(options)
	}
	return concat(
		printDeclareToken(path),
		kind,
		print("id", nil),
		body,
	)
}
