// Package consistency_require_octal_prefix is the Go engine's rule that an octal integer says it is octal.
//
// Go reads an integer literal that opens with a zero followed by more digits, `0755`, as octal, a grammar it keeps
// from C. Nothing on the line says so but the zero, and a reader who misses it reads the decimal number the digits
// spell. Go 1.13 added the `0o` prefix, so `0o755` names its base the way `0x` and `0b` name theirs, and the rule
// asks for it, offering the rewrite as a fix.
//
// Which literals are integers is the scanner's decision, read from the literal's token kind, so a float that opens
// with a zero (`07.5`, `0e1`) and an imaginary literal (`0755i`, decimal in Go) are never judged.
package consistency_require_octal_prefix

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-require-octal-prefix"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_require_octal_prefix",
	Doc: "Flags an integer literal written in legacy octal, a leading zero followed by digits like 0755, and offers " +
		"0o755 in its place, because only the zero says the number is not decimal. Floats, imaginary literals and " +
		"generated files are not judged.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var legacyOctalMessage = policy.MessageOf(Name, "legacyOctal")

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// A generated file's literals are its generator's, and the fix belongs there.
		if ast.IsGenerated(file) {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, isLiteral := node.(*ast.BasicLit)
			if isLiteral && isLegacyOctal(literal) {
				report(pass, literal)
			}
			return true
		})
	}
	return nil, nil
}

// isLegacyOctal is whether literal is an integer the scanner reads as octal for its leading zero alone: a zero, then
// a digit or the underscore Go allows after a base prefix (`0_600`). A bare `0` is decimal zero, and `0o`, `0x` and
// `0b` already name their base.
func isLegacyOctal(literal *ast.BasicLit) bool {
	if literal.Kind != token.INT || len(literal.Value) < 2 || literal.Value[0] != '0' {
		return false
	}
	second := literal.Value[1]
	return second == '_' || ('0' <= second && second <= '9')
}

// report flags literal and offers the same digits after a `0o` prefix. The digits are kept as written, separators
// included, so `0_600` becomes `0o_600`, which the grammar accepts, rather than a respelling the writer did not choose.
func report(pass *analysis.Pass, literal *ast.BasicLit) {
	suggestion := "0o" + literal.Value[1:]
	pass.Report(analysis.Diagnostic{
		Pos:      literal.Pos(),
		End:      literal.End(),
		Category: legacyOctalMessage.Id,
		Message: legacyOctalMessage.Render(map[string]string{
			"literal":    literal.Value,
			"suggestion": suggestion,
		}),
		SuggestedFixes: []analysis.SuggestedFix{{
			Message: "Write " + literal.Value + " as " + suggestion,
			TextEdits: []analysis.TextEdit{{
				Pos:     literal.Pos(),
				End:     literal.End(),
				NewText: []byte(suggestion),
			}},
		}},
	})
}
