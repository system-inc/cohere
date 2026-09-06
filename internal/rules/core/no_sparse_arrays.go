package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
)

var messageUnexpectedSparseArray = rule.Message{
	Id: "unexpectedSparseArray",
	Description: "This array literal has a hole: two commas with nothing between them. A hole is " +
		"not undefined, it is an absent index, and the difference is visible exactly where it is " +
		"least expected. `map` and `forEach` skip holes while `for...of` and spread produce " +
		"undefined for them, so the same array reads as two different lengths of data depending " +
		"on how it is walked. Almost every hole is a typo for one comma. If the absence is " +
		"deliberate, write undefined and say so.",
}

// NoSparseArrays flags an array literal containing a hole.
//
//	valid:   [1, 2, 3]
//	valid:   [1, undefined, 3]
//	valid:   [1, 2, 3, ]
//	invalid: [1, , 3]
//	invalid: [, 2]
//
// A trailing comma is not a hole and does not report. `[1, 2, 3, ]` has three elements, which is
// the form every formatter in this stack emits, so flagging it would put this rule in a fight with
// the formatter over syntax that means nothing.
//
// No fix. The rule cannot know whether the author meant one comma or an explicit undefined, and
// those produce arrays of different lengths. Guessing would silently change the data.
var NoSparseArrays = rule.Rule{
	Name: "no-sparse-arrays",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindArrayLiteralExpression: func(node *ast.Node) {
				literal := node.AsArrayLiteralExpression()
				if literal == nil || literal.Elements == nil {
					return
				}
				// A hole parses as an omitted expression. A trailing comma produces no element at
				// all, which is why it does not reach this loop and does not report.
				for _, element := range literal.Elements.Nodes {
					if element.Kind == ast.KindOmittedExpression {
						ctx.ReportNode(node, messageUnexpectedSparseArray)
						return
					}
				}
			},
		}
	},
}
