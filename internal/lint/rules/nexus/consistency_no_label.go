package nexus

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// consistencyNoLabelText is the rule's message, whose wording lives in `policy/messages/consistency-no-label.json`,
// shared with the Go engine's twin.
var consistencyNoLabelText = policy.MessageOf("nexus/consistency-no-label", "label")

// ConsistencyNoLabel bans labels: every labeled statement is reported once, on its label.
//
//	valid:   for (const row of grid) { break }
//	valid:   const point = { outer: 1 }
//	invalid: outer: for (const row of grid) { continue outer }   one finding, on `outer`
//	invalid: block: { break block }                              one finding, on `block`
//
// A label makes control leave a loop somewhere a reader is not looking, and the reader has to find the label to know
// where execution goes next. A function that returns says the same thing where it happens, and so does a named
// boolean the loop tests.
//
// Labels are banned in every language, with one finding per label, at the label's declaration (Kirk, 2026-11-06,
// #4b337jk), which is the Go engine's cohere-go/consistency-no-label judgment, so the two report the same thing in
// the same place with the same message. ESLint's `no-labels`, which this replaces in the style set, covered the whole
// labeled statement, the loop included, and reported each `break` and `continue` naming the label as well, so one
// label read as three findings. The jumps are not findings here: removing the label is the one repair, and it
// removes them with it.
//
// Every labeled statement is reported, whatever it labels and wherever it sits, nested functions included, so there
// is no `allowLoop` or `allowSwitch`: an option exempting one kind would let a project's engines disagree.
//
// No fix: removing a label means restructuring the control flow it names, which is a rewrite rather than a spelling
// change.
var ConsistencyNoLabel = rule.Rule[rule.NoOptions]{
	Name: "nexus/consistency-no-label",
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		return rule.Listeners{
			ast.KindLabeledStatement: func(node *ast.Node) {
				labeled := node.AsLabeledStatement()
				// The parser recovers every label it reads to an identifier, so a missing one is a tree this rule
				// was never shown, and reporting nothing there beats indexing into it.
				if labeled == nil || labeled.Label == nil {
					return
				}
				ruleContext.ReportNode(labeled.Label, rule.Message{
					Id:          consistencyNoLabelText.Id,
					Description: consistencyNoLabelText.Render(map[string]string{"label": labeled.Label.Text()}),
				})
			},
		}
	},
}
