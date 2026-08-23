package main

import (
	"strings"
	"testing"
	"time"

	"github.com/system-inc/verify/internal/program"
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
