package sort_helper_call

import (
	"cmp"
	"go/ast"
	"go/token"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Finding is one call of sort.Sort on a helper slice, with the message the original reports at it.
type Finding struct {
	// call is the call of sort.Sort, reported whole.
	call *ast.CallExpr
	// message is the original's message for the helper the call converts to.
	message string
}

// Findings is the state the check keeps across the functions of the dispatcher's walk over one package, the original's
// allErrors: every finding each function judged, reported once the walk ends. Its zero value is a package where
// nothing has been found, so rule.State, which hands the rule one per package, makes it new.
type Findings struct {
	// all is every finding recorded, in the order recorded, a call in a function literal once for each function
	// around it.
	all []Finding
}

// record adds found, one function's findings, to the run's.
func (findings *Findings) record(found []Finding) {
	findings.all = append(findings.all, found...)
}

// report reports every recorded call once, in the order of the calls, as the original does: sorted by position, and
// a call recorded again by a function around the one it stands in skipped. Two calls of sort.Sort never start at one
// position, since a call that starts where another does calls the other's result, and sort.Sort returns nothing.
func (findings *Findings) report(context *rule.Context) {
	slices.SortFunc(findings.all, func(first Finding, second Finding) int {
		return cmp.Compare(first.call.Pos(), second.call.Pos())
	})
	previous := token.NoPos
	for _, finding := range findings.all {
		if finding.call.Pos() == previous {
			continue
		}
		previous = finding.call.Pos()
		staticcheck_report.Report(context, finding.call, finding.message, staticcheck_report.FilterGenerated())
	}
}
