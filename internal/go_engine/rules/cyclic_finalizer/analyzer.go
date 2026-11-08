// Package cyclic_finalizer is staticcheck's SA5005, a finalizer that closes over the object it finalizes, so the object
// is never unreachable and the finalizer never runs, ported onto staticcheck's intermediate representation as a
// rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5005/sa5005.go, whose logic it keeps and
// whose message it reports word for word, through staticcheck_report.Report, where staticcheck's report puts it. The
// message names where the finalizer's function starts as staticcheck_report.DisplayPosition shows it, the kit's port of
// the report.DisplayPosition the original calls. Which argument of runtime.SetFinalizer is the object and which the
// finalizer it reads from staticcheck's knowledge package, as data. The original misses a finalizer that is the first
// closure over its object, since the IR lifts every load of a variable made before the first closure capturing it, so
// the call reads the value stored and no load; parity's literalFirst shows it, and the port keeps the miss. The
// original walks every source function of a package in one run, but keeps nothing from one function to the next: each
// call to runtime.SetFinalizer is judged alone, by its two arguments. So the port judges one function at a time and
// finds what the original finds. The original stays the oracle: the parity tests beside this file run both over the
// same packages, and they differ nowhere.
package cyclic_finalizer

import (
	"fmt"

	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA5005",
	"SA5005",
	"Flags a finalizer that closes over the object it finalizes: staticcheck's SA5005, ported onto its intermediate "+
		"representation.",
	&Listener,
)

// messageFormat is the original's message, word for word, around where the finalizer's function starts.
const messageFormat = "the finalizer closes over the object, preventing the finalizer from ever running (at %s)"

// Check judges one function: each call to runtime.SetFinalizer whose object is a variable the function holds, loaded
// from its allocation, and whose finalizer is a closure binding that very allocation, once for each such binding.
func Check(context *rule.Context, function *ir.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			site, isCallSite := instruction.(ir.CallInstruction)
			if !isCallSite {
				continue
			}
			callee := site.Common().StaticCallee()
			if callee == nil || callee.RelString(nil) != "runtime.SetFinalizer" {
				continue
			}
			object := withoutInterface(site.Common().Args[knowledge.Arg("runtime.SetFinalizer.obj")])
			load, isLoad := object.(*ir.Load)
			if !isLoad {
				continue
			}
			// The object must be a variable of the function, loaded from where it lives, the allocation a closure
			// binds.
			allocation, isAllocation := load.X.(*ir.Alloc)
			if !isAllocation {
				continue
			}
			finalizer := withoutInterface(site.Common().Args[knowledge.Arg("runtime.SetFinalizer.finalizer")])
			closure, isClosure := finalizer.(*ir.MakeClosure)
			if !isClosure {
				continue
			}
			for _, binding := range closure.Bindings {
				if binding != allocation {
					continue
				}
				shown := staticcheck_report.DisplayPosition(context.Fset, closure.Fn.Pos())
				staticcheck_report.Report(context, site, fmt.Sprintf(messageFormat, shown))
			}
		}
	}
}

// withoutInterface is value before its conversion to an interface, runtime.SetFinalizer's parameters being of type
// any, or value itself when it was never converted.
func withoutInterface(value ir.Value) ir.Value {
	if conversion, isConversion := value.(*ir.MakeInterface); isConversion {
		return conversion.X
	}
	return value
}
