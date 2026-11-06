// Package consistency_no_use_before_define is the Go engine's rule that a package-level variable or constant is
// declared above the code in its file that reads it.
//
// It is the Go form of no-use-before-define, narrowed to what Go leaves to the writer. Go resolves package-level
// names in any order, so the compiler never complains, but a reader goes down the file: a name met before its
// declaration is a value they cannot see yet, and they scroll away to learn what it holds. Functions and types are
// not judged, because reading top down puts a caller above what it calls, and a type above the code built on it is
// a choice the file makes. A reference from another file of the package is not judged either, since files have no
// order a reader follows.
package consistency_no_use_before_define

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-use-before-define"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_use_before_define",
	Doc: "Flags a reference to a package-level var or const that is declared further down the same file, because a " +
		"reader meets the name before the value it holds. Functions, types and references from other files are not " +
		"judged.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It resolves each reference to the object it names, so it
// reads types.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var useBeforeDefinitionMessage = policy.MessageOf(Name, "useBeforeDefinition")

func run(pass *analysis.Pass) (any, error) {
	packageScope := pass.Pkg.Scope()
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier {
				return true
			}
			// Uses, not the spelling: a local of the same name, a struct field or another package's name resolves to
			// an object of its own, and only one that lives in the package's own scope is judged.
			object := pass.TypesInfo.Uses[identifier]
			if object == nil || object.Parent() != packageScope {
				return true
			}
			kind := ""
			switch object.(type) {
			case *types.Var:
				kind = "variable"
			case *types.Const:
				kind = "constant"
			default:
				return true
			}
			// Positions are compared as token.Pos, which runs in file order whatever a //line directive says. A
			// declaration outside this file's span is in another file of the package, which has no order to keep.
			if object.Pos() < file.FileStart || object.Pos() > file.FileEnd || identifier.Pos() >= object.Pos() {
				return true
			}
			pass.Report(analysis.Diagnostic{
				Pos:      identifier.Pos(),
				End:      identifier.End(),
				Category: useBeforeDefinitionMessage.Id,
				Message: useBeforeDefinitionMessage.Render(map[string]string{
					"name": identifier.Name,
					"kind": kind,
				}),
			})
			return true
		})
	}
	return nil, nil
}
