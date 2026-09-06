package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
)

var messageUnexpectedDebugger = rule.Message{
	Id: "unexpectedDebugger",
	Description: "A debugger statement stops execution when devtools are open and does nothing " +
		"otherwise, so it is a breakpoint written into the source. Shipped, it halts the page for " +
		"anyone with devtools open and is invisible to everyone else, which is the worst pairing " +
		"of symptoms for reproducing a report. Set the breakpoint in the debugger instead.",
}

// NoDebugger flags a debugger statement.
//
//	valid:   console.log(value)
//	invalid: debugger;
//	invalid: if(condition) debugger;
//
// This is the cheapest rule in the catalog and one of the most valuable, because the failure it
// prevents is asymmetric: a stray debugger costs the author nothing (their devtools are open, the
// pause is expected) and costs a user with devtools open a frozen page. The author is the least
// likely person to notice.
//
// The fix removes the statement rather than commenting it out. There is no form of this statement
// that belongs in committed code, so there is nothing to preserve.
var NoDebugger = rule.Rule{
	Name: "no-debugger",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindDebuggerStatement: func(node *ast.Node) {
				ctx.ReportNodeWithFixes(node, messageUnexpectedDebugger, ctx.RemoveNode(node))
			},
		}
	},
}
