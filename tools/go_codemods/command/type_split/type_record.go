package main

import "go/ast"

// typeRecord is a named type declared at the top level of a group, with the methods declared on it.
type typeRecord struct {
	name        string
	file        *sourceFile
	declaration *ast.GenDecl
	spec        *ast.TypeSpec
	methods     []methodRecord
	// ambiguous is set when the name is declared more than once in the group, as a type per platform
	// is; which declaration a method belongs to is then a build question, so nothing of it moves.
	ambiguous bool
}

// counted is whether record's type counts as a type with methods: it has a method the build
// compiles, and not every one of them is a marker.
func (record *typeRecord) counted() bool {
	declarations := make([]*ast.FuncDecl, 0, len(record.methods))
	for _, method := range record.methods {
		declarations = append(declarations, method.declaration)
	}
	return len(declarations) > 0 && !isMarkerOnly(declarations)
}
