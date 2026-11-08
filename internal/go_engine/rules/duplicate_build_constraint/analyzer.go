// Package duplicate_build_constraint is staticcheck's SA4019, two `+build` lines in one file's preamble that hold
// the same terms, which constrain the build no more than one does, ported as a check on syntax that reports through
// the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4019/sa4019.go, whose logic it keeps
// and whose message it reports word for word, at the package clause as the original's short range of the file is.
// The preamble is staticcheck's astutil.Preamble, borrowed as the original uses it: the text of every comment group
// before the package's doc comment, or before the package clause when there is none, block comments among them,
// which the go tool never reads for constraints. A line of it is a constraint when it starts `+build ` with one
// space, and its terms are its fields, each compared as written, so `a,b` and `b,a` are two different terms. Two
// lines are identical when they hold the same terms the same number of times in any order, and each such pair, the
// earlier line first, is one finding, so three identical lines are three. `//go:build` lines are not read, as the
// original reads none. Generated files are filtered, as the original's report asks. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ nowhere they hold.
package duplicate_build_constraint

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"

	"honnef.co/go/tools/go/ast/astutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every file, as the walk enters it.
var Listener = rule.Listener{FileEnter: CheckFile}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4019",
	"SA4019",
	"Flags two `+build` lines in one file holding the same terms, which constrain the build no more than "+
		"one does: staticcheck's SA4019, ported as a check on syntax.",
	&Listener,
)

// constraintPrefix opens a constraint line of the preamble, its one space included, as the original matches it.
const constraintPrefix = "+build "

// CheckFile judges one file: every pair of its constraint lines holding the same terms is reported, the earlier line
// named first.
func CheckFile(context *rule.Context, file *ast.File) {
	constraints := constraintLines(file)
	for first, firstTerms := range constraints {
		for _, secondTerms := range constraints[first+1:] {
			if !sameTerms(firstTerms, secondTerms) {
				continue
			}
			staticcheck_report.Report(context, file, fmt.Sprintf("identical build constraints %q and %q",
				strings.Join(firstTerms, " "), strings.Join(secondTerms, " ")),
				staticcheck_report.FilterGenerated(), staticcheck_report.ShortRange())
		}
	}
}

// constraintLines is the terms of each constraint line of file's preamble, in the order the lines are written.
func constraintLines(file *ast.File) [][]string {
	constraints := [][]string{}
	for line := range strings.SplitSeq(astutil.Preamble(file), "\n") {
		terms, isConstraint := strings.CutPrefix(line, constraintPrefix)
		if !isConstraint {
			continue
		}
		constraints = append(constraints, strings.Fields(terms))
	}
	return constraints
}

// sameTerms is whether first and second hold the same terms the same number of times, in whatever order.
func sameTerms(first []string, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	return slices.Equal(slices.Sorted(slices.Values(first)), slices.Sorted(slices.Values(second)))
}
