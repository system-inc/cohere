// Package served finds the types a file serves: each type it declares that has a real method somewhere in the package,
// and each type declared elsewhere that it declares a real method of. A type without methods never counts, and nor does
// a sum type's variant whose every method is a marker, as markertype judges it.
//
// Two rules read the answer, consistency-require-one-type-per-file, which flags a file serving more than one type, and
// consistency-require-matching-file-name, which asks a file serving one type to be named for it. Both read it from
// here, so they never disagree about which types a file is for: a file one rule calls clean serves the type the other
// judges its name against.
package served

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rules/markertype"
)

// Type is one type a file serves, with the node a finding about it points at, and whether the file declares it or
// only holds methods of it.
type Type struct {
	TypeName *types.TypeName
	Node     ast.Node
	Declared bool
}

// NewIndex reads every file of pass's package before any is judged, since a type's methods may sit in any file,
// generated and test files included, and a type counts by all of them: a variant with one real method anywhere
// counts in every file that serves it.
func NewIndex(pass *analysis.Pass) *Index {
	methodsOf := map[*types.TypeName][]*ast.FuncDecl{}
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
				if typeName := receiverTypeName(pass, function); typeName != nil {
					methodsOf[typeName] = append(methodsOf[typeName], function)
				}
			}
		}
	}
	// The marker judgment is markertype's, the one the codemod that splits files by type also calls, so the rules
	// and the codemod agree on what a file serves.
	counts := map[*types.TypeName]bool{}
	for typeName, methods := range methodsOf {
		counts[typeName] = !markertype.IsMarkerOnly(methods)
	}
	return &Index{pass: pass, counts: counts}
}

// receiverTypeName is the named type a method belongs to, or nil when function is no method. The receiver's type is
// read from the types rather than its spelling, with the pointer stripped, an alias followed to the type it names,
// and a generic receiver's type arguments dropped, so `*List[T]` resolves to List itself.
func receiverTypeName(pass *analysis.Pass, function *ast.FuncDecl) *types.TypeName {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return nil
	}
	receiverType := types.Unalias(pass.TypesInfo.TypeOf(function.Recv.List[0].Type))
	if pointer, isPointer := receiverType.(*types.Pointer); isPointer {
		receiverType = types.Unalias(pointer.Elem())
	}
	named, isNamed := receiverType.(*types.Named)
	if !isNamed {
		return nil
	}
	return named.Origin().Obj()
}
