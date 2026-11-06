// Package consistency_no_label is the Go engine's rule that code jumps in the open, with no labels.
//
// It is the Go form of no-labels: a label makes control leave a loop, or land on a line, somewhere a reader is not
// looking, and the reader has to find the label to know where execution goes next. A function that returns says the
// same thing where it happens, and so does a named boolean the loop tests. The common Go shape, a labeled for around
// a select whose case breaks out of both, is the same jump: the loop moved into a function of its own returns
// instead.
package consistency_no_label

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-label"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_label",
	Doc: "Flags every labeled statement at its label, the target of a labeled break or continue and of a goto " +
		"alike, because a jump to a label lands where a reader is not looking.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. A label is syntax, so the rule reads no types.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var labelMessage = policy.MessageOf(Name, "label")

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// Every labeled statement is a jump target whatever it labels, a loop, a select, a block or an empty
		// statement at the end of a function, and the walk reaches those inside function literals too.
		ast.Inspect(file, func(node ast.Node) bool {
			labeled, isLabeled := node.(*ast.LabeledStmt)
			if !isLabeled {
				return true
			}
			pass.Report(analysis.Diagnostic{
				Pos:      labeled.Label.Pos(),
				End:      labeled.Label.End(),
				Category: labelMessage.Id,
				Message:  labelMessage.Render(map[string]string{"label": labeled.Label.Name}),
			})
			return true
		})
	}
	return nil, nil
}
