// Package consistency_no_stuttering_name is the Go engine's rule that a placeholder name does not stutter against
// its own field or method: result.Result, outcome.Outcome.
//
// A name is read where it is used, not where it is declared, so the question is never whether a word is generic but
// whether it still carries anything there. Most generic names are fine: a response that lives three lines needs no
// better word. The stutter is the case where the name provably carries nothing, since the variable had nothing of its
// own to say and borrowed its member's word to say it twice. The repair is to rename the variable for what it holds,
// and a rule cannot pick that noun, so there is no fix.
//
// This is the Go form of nexus/consistency-no-stuttering-name, with its word list. It is not Go's package-name
// stutter (http.HTTPServer), which is about an exported name repeating its package: here the base is always a
// variable this package declared, so the name being judged is one the code chose and can change.
package consistency_no_stuttering_name

import (
	"go/ast"
	"go/token"
	"go/types"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-stuttering-name"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_stuttering_name",
	Doc: "Flags a field or method reached through a variable spelled as the member is, up to the case of the first " +
		"letter, when the word is a placeholder (outcome, result, data, value, output, response, state, item, " +
		"thing): result.Result, outcome.Outcome(). The variable must be one this package declares, so an imported " +
		"package, a type's method expression, a dot-imported variable and a variable a generated file declares are " +
		"left alone.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var stutteringNameMessage = policy.MessageOf(Name, "stutteringName")

// genericNames are the words that count as saying nothing when they stutter, the TypeScript and Swift rules' list.
// It is kept short on purpose: these already read as placeholders everywhere, and a stutter on any other word, such as
// user.User, may be exactly right.
var genericNames = map[string]bool{
	"outcome":  true,
	"result":   true,
	"data":     true,
	"value":    true,
	"output":   true,
	"response": true,
	"state":    true,
	"item":     true,
	"thing":    true,
}

func run(pass *analysis.Pass) (any, error) {
	// A variable declared in a generated file has its generator's name, and a rename by hand is undone on the next
	// run, so a stutter on one is no finding even where a hand-written file uses it.
	generatedFiles := map[*token.File]bool{}
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			generatedFiles[pass.Fset.File(file.Pos())] = true
		}
	}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			selector, isSelector := node.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			base := baseName(selector.X)
			if base == nil {
				return true
			}
			word := lowerFirst(base.Name)
			if !genericNames[word] || lowerFirst(selector.Sel.Name) != word {
				return true
			}
			// The base must be a variable, so a qualified identifier (result.Result on an imported package) and a
			// method expression (data.Data on a type) are never judged: neither name is a placeholder for a value. And
			// it must be one this package declared, outside a generated file: a dot-imported variable is another
			// package's name, reached bare.
			variable, isVar := pass.TypesInfo.Uses[base].(*types.Var)
			if !isVar || variable.Pkg() != pass.Pkg || generatedFiles[pass.Fset.File(variable.Pos())] {
				return true
			}
			pass.Report(analysis.Diagnostic{
				Pos:      selector.Sel.Pos(),
				End:      selector.Sel.End(),
				Category: stutteringNameMessage.Id,
				Message: stutteringNameMessage.Render(map[string]string{
					"name":   base.Name,
					"member": selector.Sel.Name,
				}),
			})
			return true
		})
	}
	return nil, nil
}

// baseName is the bare name a member is reached through: result, (result), or *result, which reaches the same member
// through the pointer the variable holds. Anything longer, holder.result.Result, is a member chain whose outer word
// is a field rather than a placeholder, and gives nil.
func baseName(expression ast.Expr) *ast.Ident {
	expression = ast.Unparen(expression)
	if star, isStar := expression.(*ast.StarExpr); isStar {
		expression = ast.Unparen(star.X)
	}
	identifier, _ := expression.(*ast.Ident)
	return identifier
}

// lowerFirst is name with its first letter lowered, so an exported member, result.Result, matches its variable as an
// unexported one, result.result, does.
func lowerFirst(name string) string {
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToLower(first)) + name[size:]
}
