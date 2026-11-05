// Package consistency_require_one_type_per_file is the Go engine's rule that a file serves at most one type with
// methods.
//
// It is the Go form of max-classes-per-file: when each file serves one type, the file's name is an index, so a reader
// looking for a type opens the file named for it and finds its declaration and its methods together, and a change to
// one type never scrolls past another. A type's methods may still be split by theme across files, as long as each file
// serves that one type, and plain types without methods, which carry no behavior to find, never count. Nor does a sum
// type's variant whose methods only mark it a member, `func (*Await) instructionValue() {}`: that is Go's spelling of
// union membership, the way a member of a discriminated union is a type in TypeScript, which max-classes-per-file
// never counted, so the variants stay beside their interface.
package consistency_require_one_type_per_file

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/goengine/rules/markertype"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-require-one-type-per-file"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_require_one_type_per_file",
	Doc: "Flags every type with methods after the first that a file serves, where a file serves each type it " +
		"declares that has a method anywhere in the package and each type it declares a method of. A type without " +
		"methods, an interface among them, never counts, nor does a sum type's variant whose every method is a " +
		"marker, with an empty body and no results.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It resolves every receiver through the types, so a pointer,
// an alias or a generic receiver's type arguments never hide the type a method belongs to.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var extraTypeMessage = policy.MessageOf(Name, "extraType")

// servedType is one type a file serves, with the node a finding about it points at.
type servedType struct {
	typeName *types.TypeName
	node     ast.Node
}

func run(pass *analysis.Pass) (any, error) {
	// A type counts only if it has a method somewhere in the package that is more than a marker, and its methods may
	// sit in any file, generated and test files included, so every file is read before any is judged.
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
	// A sum type's variant, whose methods only mark it a member, is judged by every method it has in the package, so
	// a variant with one real method anywhere counts in every file that serves it. The judgment is markertype's, the
	// one the codemod that splits files by type also calls, so the two never disagree about what a file serves.
	counts := map[*types.TypeName]bool{}
	for typeName, methods := range methodsOf {
		counts[typeName] = !markertype.IsMarkerOnly(methods)
	}
	for _, file := range pass.Files {
		judgeFile(pass, file, counts)
	}
	return nil, nil
}

// judgeFile reports every type file serves after the first in source order, where counts holds the types with a
// method that is more than a marker.
func judgeFile(pass *analysis.Pass, file *ast.File, counts map[*types.TypeName]bool) {
	served := []servedType{}
	isServed := map[*types.TypeName]bool{}
	// A declared type is placed at its declaration, wherever its methods in this file sit, so declarations are read
	// before receivers. An alias declares no type of its own, so its object never has methods and never counts.
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
			if !isTypeSpec {
				continue
			}
			typeName, isTypeName := pass.TypesInfo.Defs[typeSpec.Name].(*types.TypeName)
			if !isTypeName || !counts[typeName] || isServed[typeName] {
				continue
			}
			served = append(served, servedType{typeName: typeName, node: typeSpec.Name})
			isServed[typeName] = true
		}
	}
	// A type declared elsewhere is placed at the receiver of its first method here, since declarations come in source
	// order. A marker type's receivers are skipped like its declaration, so a variant's marker method beside its
	// interface serves nothing.
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction {
			continue
		}
		typeName := receiverTypeName(pass, function)
		if typeName == nil || !counts[typeName] || isServed[typeName] {
			continue
		}
		served = append(served, servedType{typeName: typeName, node: function.Recv.List[0].Type})
		isServed[typeName] = true
	}
	slices.SortFunc(served, func(left, right servedType) int {
		return cmp.Compare(left.node.Pos(), right.node.Pos())
	})
	for _, extra := range served[min(1, len(served)):] {
		pass.Report(analysis.Diagnostic{
			Pos:      extra.node.Pos(),
			End:      extra.node.End(),
			Category: extraTypeMessage.Id,
			Message: extraTypeMessage.Render(map[string]string{
				"typeName":      extra.typeName.Name(),
				"firstTypeName": served[0].typeName.Name(),
			}),
		})
	}
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
