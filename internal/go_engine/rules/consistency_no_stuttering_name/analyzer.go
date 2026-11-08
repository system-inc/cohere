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
	"fmt"
	"go/ast"
	"go/types"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-stuttering-name"

// listener reads which of the package's files are generated as the syntax dispatcher's walk enters its first file,
// and hears every selector of the walk.
var listener = rule.Listener{
	Requires:  []*analysis.Analyzer{positions.Analyzer},
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.SelectorExpr)(nil)},
	Node:      judgeSelector,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_stuttering_name", "Flags a field or method reached "+
	"through a variable spelled as the member is, up to the case of the first letter, when the word is a "+
	"placeholder (outcome, result, data, value, output, response, state, item, thing): result.Result, "+
	"outcome.Outcome(). The variable must be one this package declares, so an imported package, a type's method "+
	"expression, a dot-imported variable and a variable a generated file declares are left alone.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

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

// packageState is what the rule reads once for its package, held in its context's state.
type packageState struct {
	// table is the package's line table, which reads a variable's file without the FileSet's lock.
	table *positions.Table
	// generatedFiles are the package's generated files, by name as table reads them, read as the walk enters its
	// first file. A variable declared in one may be used in any file, so every file is read before any is judged.
	generatedFiles map[string]bool
}

// enterFile reads the line table and the package's generated files the first time.
func enterFile(context *rule.Context, _ *ast.File) {
	state := rule.State[packageState](context)
	if state.generatedFiles != nil {
		return
	}
	table, isTable := context.ResultOf(positions.Analyzer).(*positions.Table)
	if !isTable {
		// Invariant: the listener requires positions, whose result is always a table, so anything else is a mistake
		// in building the rule, which the dispatcher's recover makes this rule's crash on the package.
		panic(fmt.Sprintf("reading the line table: positions gave %T", context.ResultOf(positions.Analyzer)))
	}
	state.table = table
	// A variable declared in a generated file has its generator's name, and a rename by hand is undone on the next
	// run, so a stutter on one is no finding even where a hand-written file uses it. The files are held by name, which
	// table reads for a variable without the FileSet's lock.
	state.generatedFiles = map[string]bool{}
	for _, file := range context.Files {
		if ast.IsGenerated(file) {
			state.generatedFiles[table.Position(file.Pos()).Filename] = true
		}
	}
}

// judgeSelector reports a selector whose member stutters a placeholder variable this package declared.
func judgeSelector(context *rule.Context, node ast.Node, _ []ast.Node) {
	selector, isSelector := node.(*ast.SelectorExpr)
	if !isSelector {
		return
	}
	base := baseName(selector.X)
	if base == nil {
		return
	}
	word := lowerFirst(base.Name)
	if !genericNames[word] || lowerFirst(selector.Sel.Name) != word {
		return
	}
	// The base must be a variable, so a qualified identifier (result.Result on an imported package) and a method
	// expression (data.Data on a type) are never judged: neither name is a placeholder for a value. And it must be one
	// this package declared, outside a generated file: a dot-imported variable is another package's name, reached
	// bare.
	state := rule.State[packageState](context)
	variable, isVariable := context.TypesInfo.Uses[base].(*types.Var)
	if !isVariable || variable.Pkg() != context.Package ||
		state.generatedFiles[state.table.Position(variable.Pos()).Filename] {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos:      selector.Sel.Pos(),
		End:      selector.Sel.End(),
		Category: stutteringNameMessage.Id,
		Message: stutteringNameMessage.Render(map[string]string{
			"name":   base.Name,
			"member": selector.Sel.Name,
		}),
	})
}

// baseName is the bare name a member is reached through: result, (result), or *result, which reaches the same member
// through the pointer the variable holds. Anything longer, holder.result.Result, is a member chain whose outer word
// is a field rather than a placeholder, and gives nil.
func baseName(expression ast.Expr) *ast.Ident {
	unwrapped := ast.Unparen(expression)
	if star, isStar := unwrapped.(*ast.StarExpr); isStar {
		unwrapped = ast.Unparen(star.X)
	}
	identifier, _ := unwrapped.(*ast.Ident)
	return identifier
}

// lowerFirst is name with its first letter lowered, so an exported member, result.Result, matches its variable as an
// unexported one, result.result, does.
func lowerFirst(name string) string {
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToLower(first)) + name[size:]
}
