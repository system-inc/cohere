package function_triviality

import (
	"cmp"
	"fmt"
	"go/ast"

	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
)

// declarationPlace is where a top-level declaration starts, as cohere-go keys a unit: the file a reader sees, past
// any //line directive, and the offset of the declaration's first byte in the file as parsed.
type declarationPlace struct {
	File   string
	Offset int
}

// comparePlaces orders places by file and then by offset.
func comparePlaces(left declarationPlace, right declarationPlace) int {
	return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Offset, right.Offset))
}

// declaringFunction is the top-level function read is declared in: read itself, or the function that encloses a
// function literal, however deep.
func declaringFunction(read *intermediate_representation.Function) *intermediate_representation.Function {
	declaring := read
	for declaring.Parent() != nil {
		declaring = declaring.Parent()
	}
	return declaring
}

// placeOf is where declared, a top-level function with syntax, is declared. An instantiation of a generic function is
// built from the generic function's declaration, so it is placed there.
func placeOf(declared *intermediate_representation.Function) declarationPlace {
	declaration, isDeclaration := declared.Syntax().(*ast.FuncDecl)
	if !isDeclaration {
		// Invariant: the walk enters only a function with syntax, and the top-level function such a one is declared
		// in is built from a function declaration. The package initializer, which is not, is never called.
		panic(fmt.Sprintf("function_triviality: %s is built from a %T, not a function declaration", declared,
			declared.Syntax()))
	}
	tokenFile := declared.Program.FileSet.File(declaration.Pos())
	if tokenFile == nil {
		// Invariant: a declaration the program built a function from was parsed into the program's file set.
		panic(fmt.Sprintf("function_triviality: the declaration of %s is in no file of its program", declared))
	}
	return declarationPlace{
		File:   tokenFile.Position(declaration.Pos()).Filename,
		Offset: tokenFile.Offset(declaration.Pos()),
	}
}
