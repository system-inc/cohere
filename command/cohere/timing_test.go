package main

import (
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

// TestPairsWithEqualNodesAndUnequalCostAreFlagged covers the shape that found the attribution bug.
//
// Two rules offered exactly the same node count and differing by orders of magnitude means one is
// carrying work the other reads for free. Requiring three or more rules would have missed the case
// that motivated this note: it was found on a pair, 3,407 nodes each, 83x apart.
func TestPairsWithEqualNodesAndUnequalCostAreFlagged(t *testing.T) {
	timings := program.NewTimings([]string{"expensive", "cheap"})
	setCost(timings, "expensive", 100*time.Millisecond, 3407)
	setCost(timings, "cheap", time.Millisecond, 3407)

	rendered := renderTimings(timings, 200*time.Millisecond)

	if !strings.Contains(rendered, "differ") {
		t.Fatalf("a pair with equal nodes and 100x cost was not flagged:\n%s", rendered)
	}
	if !strings.Contains(rendered, "3407") {
		t.Fatalf("the note did not name the shared node count:\n%s", rendered)
	}
}

// TestSimilarCostsAreNotFlagged is the half that keeps the note meaningful.
//
// Rules registered for the same kind and costing about the same are unremarkable. A note that fired
// on every pair sharing a node count would be noise, and noise in a diagnostic is how a real signal
// stops being read.
func TestSimilarCostsAreNotFlagged(t *testing.T) {
	timings := program.NewTimings([]string{"first", "second"})
	setCost(timings, "first", 10*time.Millisecond, 3407)
	setCost(timings, "second", 8*time.Millisecond, 3407)

	if rendered := renderTimings(timings, 100*time.Millisecond); strings.Contains(rendered, "differ") {
		t.Fatalf("two comparably-priced rules were flagged as sharing work:\n%s", rendered)
	}
}

// TestDifferentNodeCountsAreNotCompared keeps the note from pairing rules that never registered for
// the same kinds, where a cost difference means nothing.
func TestDifferentNodeCountsAreNotCompared(t *testing.T) {
	timings := program.NewTimings([]string{"many-nodes", "few-nodes"})
	setCost(timings, "many-nodes", 100*time.Millisecond, 600000)
	setCost(timings, "few-nodes", time.Millisecond, 3407)

	if rendered := renderTimings(timings, 200*time.Millisecond); strings.Contains(rendered, "differ") {
		t.Fatalf("rules with unrelated node counts were compared:\n%s", rendered)
	}
}

// TestSharedFillIsReported proves the cost lands on the derivation rather than vanishing.
func TestSharedFillIsReported(t *testing.T) {
	timings := program.NewTimings([]string{"a-rule"})
	setCost(timings, "a-rule", 10*time.Millisecond, 100)
	timings.RecordSharedFill("a-rule", "nexus.allComments", 120*time.Millisecond)

	rendered := renderTimings(timings, 200*time.Millisecond)
	if !strings.Contains(rendered, "nexus.allComments") {
		t.Fatalf("the shared derivation was not named:\n%s", rendered)
	}
	if !strings.Contains(rendered, "paid once per file") {
		t.Fatalf("the note did not explain what a shared cost is:\n%s", rendered)
	}
}

func setCost(timings *program.Timings, name string, cost time.Duration, nodes int) {
	timings.SetForTest(name, cost, nodes)
}

func renderTimings(timings *program.Timings, wall time.Duration) string {
	builder := &strings.Builder{}
	printTimings(builder, timings, wall)
	return builder.String()
}

// TestTheNamedRulesAreTheRulesWhoseCostsAreQuoted is the assertion the first version of this file
// was missing, and the omission let a real bug through.
//
// The original note printed names[0] and names[len-1], which are list positions, while the costs
// came from a scan for the minimum and maximum. With two rules those coincide, so every fixture
// passed. With three they come apart: the line named a 50ms rule, quoted 400ms, and the rule that
// actually cost 400ms did not appear at all.
//
// A fixture asserting that the note fired is not a fixture asserting the note is right. The earlier
// tests checked for the substring "differ" and for one rule name, both present in a line that was
// wrong about everything else. This one requires the quoted numbers to belong to the named rules.
func TestTheNamedRulesAreTheRulesWhoseCostsAreQuoted(t *testing.T) {
	timings := program.NewTimings([]string{"aaa-middling", "mmm-dearest", "zzz-cheapest"})
	setCost(timings, "aaa-middling", 50*time.Millisecond, 3407)
	setCost(timings, "mmm-dearest", 400*time.Millisecond, 3407)
	setCost(timings, "zzz-cheapest", time.Millisecond, 3407)

	rendered := renderTimings(timings, time.Second)

	line := ""
	for _, candidate := range strings.Split(rendered, "\n") {
		if strings.Contains(candidate, "differ") {
			line = candidate
			break
		}
	}
	if line == "" {
		t.Fatalf("three rules at one node count and 400x apart produced no note:\n%s", rendered)
	}

	// The dearest rule must be named, because it is the one a reader should go look at.
	if !strings.Contains(line, "mmm-dearest") {
		t.Fatalf("the note quotes the dearest cost but does not name the dearest rule:\n  %s", line)
	}
	if !strings.Contains(line, "zzz-cheapest") {
		t.Fatalf("the note does not name the cheapest rule:\n  %s", line)
	}
	// And the rule that is neither must not appear, or the line points somewhere misleading.
	if strings.Contains(line, "aaa-middling") {
		t.Fatalf("the note names a middling rule as though it were an extreme:\n  %s", line)
	}
	if !strings.Contains(line, "400ms") || !strings.Contains(line, "1.0ms") {
		t.Fatalf("the note does not quote both extremes:\n  %s", line)
	}
}

// TestCoverageStatesWhatTheTableDoesNotMeasure guards the line that keeps the table honest.
//
// The rows measure rule listeners and per-file setup. Nothing measures the walk that offers nodes
// to them, and on the ahra tree that walk is the majority of the phase: 285ms of rule time against
// 1,024ms wall clock single-threaded, so the table covers roughly 28 percent. A reader who sums the
// share column gets 100 percent and concludes the phase is explained.
//
// Unlike the notes above, this takes only two durations and has no ordering invariant, so calling
// it directly is the real path rather than a bypass of one.
func TestCoverageStatesWhatTheTableDoesNotMeasure(t *testing.T) {
	var out strings.Builder
	printCoverage(&out, 285*time.Millisecond, 1024*time.Millisecond)
	rendered := out.String()

	if !strings.Contains(rendered, "28%") {
		t.Fatalf("coverage did not state the share the rows account for: %q", rendered)
	}
	if !strings.Contains(rendered, "72%") {
		t.Fatalf("coverage did not state the share nothing measures: %q", rendered)
	}
	if !strings.Contains(rendered, "traversal") {
		t.Fatalf("coverage did not name what the unmeasured remainder is: %q", rendered)
	}
}

// TestCoverageRefusesToQuoteAShareWhenTimeIsSummedAcrossWorkers is the half that matters more,
// because parallel is the default mode.
//
// Attributed time is summed across workers while wall clock overlaps them, so in a parallel run
// rule time slightly exceeds wall clock. Printing that as a coverage percentage would say the table
// accounts for 104 percent of the phase, which reads as "rules are everything and traversal is
// free." Traversal is not free; it parallelizes almost perfectly and so collapses out of wall clock
// rather than being cheap. A ratio that cannot mean what it appears to mean must not be printed as
// though it does.
func TestCoverageRefusesToQuoteAShareWhenTimeIsSummedAcrossWorkers(t *testing.T) {
	var out strings.Builder
	printCoverage(&out, 314*time.Millisecond, 301*time.Millisecond)
	rendered := out.String()

	if strings.Contains(rendered, "%") {
		t.Fatalf("a share was quoted for a run whose time is summed across workers: %q", rendered)
	}
	if !strings.Contains(rendered, "single-threaded") {
		t.Fatalf("coverage did not point at the mode that can answer the question: %q", rendered)
	}
	if !strings.Contains(rendered, "traversal") {
		t.Fatalf("coverage did not say traversal is unmeasured: %q", rendered)
	}
}

// TestCoverageSaysNothingRatherThanComputingFromZero covers the degenerate input.
//
// A zero phase duration has no ratio to state. Printing a coverage claim derived from it would be
// worse than the silence it replaced, since a fabricated number is harder to notice than a missing
// line.
func TestCoverageSaysNothingRatherThanComputingFromZero(t *testing.T) {
	var out strings.Builder
	printCoverage(&out, 285*time.Millisecond, 0)
	if out.String() != "" {
		t.Fatalf("coverage printed a claim computed from a zero phase duration: %q", out.String())
	}

	out.Reset()
	printCoverage(&out, 0, 1024*time.Millisecond)
	if out.String() != "" {
		t.Fatalf("coverage printed a claim with no attributed time to divide: %q", out.String())
	}
}

// TestTheTableItselfCarriesTheCoverageLine pins the wiring, not just the function.
//
// The three tests above call printCoverage directly, which verifies a fragment and reads as though
// it verifies the behavior. It does not: deleting the call from printTimings leaves all three green
// while the table goes back to implying it explains the whole phase. Guard present, fixtures
// passing, behavior gone.
//
// That failure shape was reported by @system_cohere_lint_fix an hour before this was written, in
// their own package and against their own fixtures, so it is a measured pattern in this codebase
// rather than a hypothetical. It costs one test to exclude, driven through the same renderTimings
// seam the other table assertions use.
func TestTheTableItselfCarriesTheCoverageLine(t *testing.T) {
	timings := program.NewTimings([]string{"a-rule"})
	setCost(timings, "a-rule", 285*time.Millisecond, 100)

	rendered := renderTimings(timings, 1024*time.Millisecond)
	if !strings.Contains(rendered, "coverage:") {
		t.Fatalf("the table did not carry a coverage line:\n%s", rendered)
	}
	if !strings.Contains(rendered, "traversal") {
		t.Fatalf("the table's coverage line did not name the unmeasured remainder:\n%s", rendered)
	}
}

// TestPerNodeSharedFillsCollapseToOneRow holds that a derivation keyed per node reports as one
// line rather than one line per node.
//
// A cache key identifies a cache entry, which is not a unit anybody wants to read. The HIR cache
// keys per function by kind and source offset, correctly, and `--timing` rendered 11,150 rows for
// it against three real ones: 17,000 lines of output, two thirds of it a single derivation reported
// one function at a time, every row reading 0.00ms and none of them actionable. Collapsed, that
// derivation is 1,256ms and the largest shared cost in the run.
//
// The single-entry assertions matter as much as the collapsed one. A fix that summed everything
// under one heading would pass a test that only counted rows, and would destroy the three
// distinct entries this table exists to separate.
func TestPerNodeSharedFillsCollapseToOneRow(t *testing.T) {
	timings := program.NewTimings([]string{"a-rule"})
	setCost(timings, "a-rule", 10*time.Millisecond, 100)

	timings.RecordSharedFill("a-rule", "hir.Function:175:1001", 3*time.Millisecond)
	timings.RecordSharedFill("a-rule", "hir.Function:175:2002", 4*time.Millisecond)
	timings.RecordSharedFill("a-rule", "hir.Function:175:3003", 5*time.Millisecond)
	timings.RecordSharedFill("a-rule", "comments.All", 120*time.Millisecond)

	rendered := renderTimings(timings, 200*time.Millisecond)

	if strings.Count(rendered, "shared: hir.Function") != 1 {
		t.Fatalf("the per-node derivation should report as exactly one row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "across 3 entries") {
		t.Fatalf("the collapsed row should say how many entries it stands for:\n%s", rendered)
	}
	if !strings.Contains(rendered, "12.0ms") && !strings.Contains(rendered, "12ms") {
		t.Fatalf("the collapsed row should carry the summed cost of 12ms:\n%s", rendered)
	}

	// A derivation with one entry keeps the plain wording: the count would be noise on a row that
	// stands for exactly itself.
	if !strings.Contains(rendered, "shared: comments.All cost") {
		t.Fatalf("a single-entry derivation should still be named plainly:\n%s", rendered)
	}
	if strings.Contains(rendered, "comments.All cost 120ms across") {
		t.Fatalf("a single-entry derivation should not carry an entry count:\n%s", rendered)
	}
}
