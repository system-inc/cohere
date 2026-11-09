// Package consistency_no_grouped_var is the Go engine's rule that inside a function each var and const declares one
// name on its own line.
//
// It is the Go form of one-var: a reader of a function body meets one name, its type and its value together, declared
// where it is first needed, and a change to one of them touches only its own line. A grouped block or a list of names
// makes the reader pair names with values across commas and lines. Two shapes keep their block: at package level no
// declaration is judged, since a grouped block there is how Go lays out a file's settings and enums, and inside a
// function a `const ( ... )` whose values count with iota stays grouped, since iota counts only within one block.
package consistency_no_grouped_var

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-grouped-var"

// listener hears every declaration statement of the syntax dispatcher's walk.
var listener = rule.Listener{
	FileLocal: true, Nodes: []ast.Node{(*ast.DeclStmt)(nil)}, Node: judgeStatement}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_grouped_var", "Flags, inside a function body, a "+
	"parenthesized var or const block and a var or const that declares more than one name, unless its one value "+
	"yields them all, as a multi-return call or a comma-ok read does. A const block that counts with iota keeps its "+
	"block, since iota counts only within one. Package-level declarations and := short declarations, including the "+
	"parallel assignment Go writes for a swap, are not judged.", &listener)

// Rule is this rule's entry in the Go engine's registry. It reads the type of a lone value to tell a multi-return
// call or comma-ok read, whose names cannot be split, from a list of names that can, and resolves each `iota` to the
// predeclared one, so a local that shadows the name does not excuse a block.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var groupedDeclarationMessage = policy.MessageOf(Name, "groupedDeclaration")

var severalNamesMessage = policy.MessageOf(Name, "severalNames")

// judgeStatement judges a var or const declaration statement. A declaration statement exists only inside a block, so
// the walk reaches every function body's, a literal's at package level included, and never a package-level
// declaration, which is a bare GenDecl.
func judgeStatement(context *rule.Context, node ast.Node, _ []ast.Node) {
	statement, isDeclaration := node.(*ast.DeclStmt)
	if !isDeclaration {
		return
	}
	general, isGeneral := statement.Decl.(*ast.GenDecl)
	if !isGeneral || (general.Tok != token.VAR && general.Tok != token.CONST) {
		return
	}
	judgeDeclaration(context, general)
}

// judgeDeclaration reports a parenthesized block at its keyword, unless it is a const block counting with iota, and
// every spec in it that declares several names at those names. Both can hold of one block, and each is its own fix:
// splitting the block still leaves a list of names.
func judgeDeclaration(context *rule.Context, general *ast.GenDecl) {
	keyword := general.Tok.String()
	if general.Lparen.IsValid() && !countsWithIota(context, general) {
		context.Report(analysis.Diagnostic{
			Pos:      general.TokPos,
			End:      general.TokPos + token.Pos(len(keyword)),
			Category: groupedDeclarationMessage.Id,
			Message:  groupedDeclarationMessage.Render(map[string]string{"keyword": keyword}),
		})
	}
	for _, spec := range general.Specs {
		valueSpec, isValueSpec := spec.(*ast.ValueSpec)
		if !isValueSpec || len(valueSpec.Names) < 2 || yieldsEveryName(context, valueSpec) {
			continue
		}
		names := make([]string, 0, len(valueSpec.Names))
		for _, name := range valueSpec.Names {
			names = append(names, name.Name)
		}
		context.Report(analysis.Diagnostic{
			Pos:      valueSpec.Names[0].Pos(),
			End:      valueSpec.Names[len(valueSpec.Names)-1].End(),
			Category: severalNamesMessage.Id,
			Message: severalNamesMessage.Render(map[string]string{
				"keyword": keyword,
				"names":   strings.Join(names, ", "),
			}),
		})
	}
}

// yieldsEveryName is whether valueSpec's names all come from one value, a multi-return call or a comma-ok read such
// as `cache[key]` or `anything.(string)`. Those names are the grammar of the value and cannot be split across
// declarations. The type checker records such a value's type as a tuple, a comma-ok read's included, so the tuple is
// read rather than the value's shape guessed at.
func yieldsEveryName(context *rule.Context, valueSpec *ast.ValueSpec) bool {
	if len(valueSpec.Values) != 1 {
		return false
	}
	_, isTuple := context.TypesInfo.TypeOf(valueSpec.Values[0]).(*types.Tuple)
	return isTuple
}

// countsWithIota is whether general is a const block whose values read the predeclared iota. iota counts the specs of
// one block, so splitting such a block resets every count to zero and changes the values; the block is the grammar.
// A spec without values repeats the one before it, so reading the written values finds every use. Each `iota` is
// resolved through the type checker, since a local named iota is an ordinary constant and counts nothing.
func countsWithIota(context *rule.Context, general *ast.GenDecl) bool {
	if general.Tok != token.CONST {
		return false
	}
	predeclared := types.Universe.Lookup("iota")
	for _, spec := range general.Specs {
		valueSpec, isValueSpec := spec.(*ast.ValueSpec)
		if !isValueSpec {
			continue
		}
		for _, value := range valueSpec.Values {
			counts := false
			ast.Inspect(value, func(node ast.Node) bool {
				identifier, isIdentifier := node.(*ast.Ident)
				if isIdentifier && context.TypesInfo.Uses[identifier] == predeclared {
					counts = true
				}
				return !counts
			})
			if counts {
				return true
			}
		}
	}
	return false
}
