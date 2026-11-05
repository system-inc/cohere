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
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/goengine/rules/served"
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

func run(pass *analysis.Pass) (any, error) {
	// The types a file serves come from package served, which consistency-require-matching-file-name reads too, so a
	// file this rule calls clean is judged by that rule against the one type it serves, and the two never disagree.
	index := served.NewIndex(pass)
	for _, file := range pass.Files {
		judgeFile(pass, file, index)
	}
	return nil, nil
}

// judgeFile reports every type file serves after the first in source order.
func judgeFile(pass *analysis.Pass, file *ast.File, index *served.Index) {
	servedTypes := index.Types(file)
	for _, extra := range servedTypes[min(1, len(servedTypes)):] {
		pass.Report(analysis.Diagnostic{
			Pos:      extra.Node.Pos(),
			End:      extra.Node.End(),
			Category: extraTypeMessage.Id,
			Message: extraTypeMessage.Render(map[string]string{
				"typeName":      extra.TypeName.Name(),
				"firstTypeName": servedTypes[0].TypeName.Name(),
			}),
		})
	}
}
