package consistency_no_any

import (
	"strings"
	"testing"
)

// fixedHereSource writes an any that its own file fixes, a variable given recover's value, which another package's
// declaration types, and then hold's parameter, an any only a call elsewhere fixes, so the candidates are the first
// fixed in its file and the second not.
const fixedHereSource = "package flows\n\n" +
	"func handle() {\n" +
	"\tvar cause any = recover()\n" +
	"\t_ = cause\n" +
	"}\n\n" +
	"func hold(value any) {}\n"

// fixedHereOnlySource writes only the any its own file fixes.
const fixedHereOnlySource = "package flows\n\n" +
	"func handle() {\n" +
	"\tvar cause any = recover()\n" +
	"\t_ = cause\n" +
	"}\n"

// elsewhereSource writes no any and hands a value on assignments times, then hands hold recover's value when
// callsHold is set, which fixes hold's parameter from this file alone.
func elsewhereSource(assignments int, callsHold bool) string {
	call := ""
	if callsHold {
		call = "\thold(recover())\n"
	}
	return "package flows\n\n" +
		"func run() int {\n" +
		"\tcount := 1\n" +
		"\tvar total int\n" +
		strings.Repeat("\ttotal = count\n", assignments) +
		call +
		"\treturn total\n" +
		"}\n"
}

// recordedFlows counts every flow the analysis holds, each value fed or defined from a declaration as well as each
// declaration marked fixed.
func recordedFlows(flows *flowAnalysis) int {
	count := len(flows.sinkFixed) + len(flows.sourceFixed)
	for _, fed := range flows.feeds {
		count += len(fed)
	}
	for _, from := range flows.definedFrom {
		count += len(from)
	}
	return count
}

// The walk takes the files holding a candidate first and stops once every candidate is fixed, so a package whose any
// its own file fixes records as many flows however many values another file hands on, though that file comes first.
// A walk in the files' own order, or one that went on past the last candidate fixed, records the other file's 512
// assignments too.
func TestTheWalkStopsOnceEveryCandidateIsFixed(test *testing.T) {
	test.Parallel()
	few, reportedFew := checkedPackageOf(test, elsewhereSource(1, false), fixedHereOnlySource).run()
	many, reportedMany := checkedPackageOf(test, elsewhereSource(512, false), fixedHereOnlySource).run()
	if len(reportedFew) != 0 || len(reportedMany) != 0 {
		test.Errorf("want the fixed any unreported, got %d and %d findings", len(reportedFew), len(reportedMany))
	}
	if recordedFlows(few.flows) != recordedFlows(many.flows) {
		test.Errorf("with one value handed on elsewhere %d flows were recorded, with 512 %d, where the two must be "+
			"the same", recordedFlows(few.flows), recordedFlows(many.flows))
	}
}

// The walk stops only once every candidate is fixed, not the first: hold's any is fixed only by a call in a file
// holding no candidate, walked after the file holding both, so a walk that stopped at the first candidate fixed, or
// never walked the files holding none, would report it.
func TestTheWalkGoesOnWhileACandidateIsUnfixed(test *testing.T) {
	test.Parallel()
	state, reported := checkedPackageOf(test, elsewhereSource(512, true), fixedHereSource).run()
	if len(state.candidates) != 2 {
		test.Fatalf("want the two anys as candidates, got %d", len(state.candidates))
	}
	if len(reported) != 0 {
		test.Errorf("want both anys fixed, got %d findings", len(reported))
	}
}
