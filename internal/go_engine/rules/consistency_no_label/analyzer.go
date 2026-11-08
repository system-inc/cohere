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
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-label"

// listener hears every labeled statement of the syntax dispatcher's walk.
var listener = rule.Listener{Nodes: []ast.Node{(*ast.LabeledStmt)(nil)}, Node: judgeLabeled}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_label", "Flags every labeled statement at its label, "+
	"the target of a labeled break or continue and of a goto alike, because a jump to a label lands where a reader "+
	"is not looking.", &listener)

// Rule is this rule's entry in the Go engine's registry. A label is syntax, so the rule reads no types.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer, Listener: &listener}

var labelMessage = policy.MessageOf(Name, "label")

// judgeLabeled reports a labeled statement at its label. Every labeled statement is a jump target whatever it labels,
// a loop, a select, a block or an empty statement at the end of a function, and the walk reaches those inside
// function literals too.
func judgeLabeled(context *rule.Context, node ast.Node, _ []ast.Node) {
	labeled, isLabeled := node.(*ast.LabeledStmt)
	if !isLabeled {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos:      labeled.Label.Pos(),
		End:      labeled.Label.End(),
		Category: labelMessage.Id,
		Message:  labelMessage.Render(map[string]string{"label": labeled.Label.Name}),
	})
}
