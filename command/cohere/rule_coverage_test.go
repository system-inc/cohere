package main

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/program"
	"github.com/system-inc/cohere/internal/rule"
)

// The coverage note distinguishes a rule nobody wired from a rule that is configured and satisfied.
//
// Both states produce zero findings and both used to print "listened to no files", so a passing
// rule read exactly like a dead one. They are opposite defects: one means the tree is clean of what
// the rule catches, the other means nothing was ever checked.
//
// The pair was separable only because a second line about missing config happened to print for the
// unwired case, and that line does not always appear. A guard that works by accident of which
// sentence prints is not a guard, which is why this asserts the note itself rather than the pair.
func TestCoverageNoteSeparatesUnwiredFromSatisfied(t *testing.T) {
	rules := []rule.Rule{{Name: "satisfied-rule"}, {Name: "unwired-rule"}}
	coverage := program.Coverage{
		RulesOffered:   map[string]int{"satisfied-rule": 3407},
		RulesListening: map[string]int{},
	}

	var out strings.Builder
	writeRuleCoverage(&out, rules, coverage)
	rendered := out.String()

	// The sentence says "registered no listener" rather than "declined", because five rules here do
	// all their work in Run and return nil. For those, "it ran and looked, and nothing matched" is
	// true of the walk and false of the rule, and the two readings send a reader to different places.
	if !strings.Contains(rendered, "satisfied-rule registered no listener on any of the 3407 files") {
		t.Fatalf("a satisfied rule did not report the files it was offered:\n%s", rendered)
	}
	if !strings.Contains(rendered, "unwired-rule was offered no files") {
		t.Fatalf("an unwired rule did not report that nothing wired it:\n%s", rendered)
	}

	// The two must not be describable by one sentence, which is the defect this replaced.
	for _, line := range strings.Split(strings.TrimSpace(rendered), "\n") {
		if strings.Contains(line, "satisfied-rule") && strings.Contains(line, "offered no files") {
			t.Fatalf("a satisfied rule was described as unwired: %q", line)
		}
		if strings.Contains(line, "unwired-rule") && strings.Contains(line, "registered no listener") {
			t.Fatalf("an unwired rule was described as satisfied: %q", line)
		}
	}
}

// A rule that listened to files is not silent and must not be named at all.
//
// Without this the note could satisfy the test above by naming every rule, which is the
// flags-everything failure that makes a detector useless in the opposite direction.
func TestCoverageNoteSaysNothingAboutARuleThatListened(t *testing.T) {
	// The rule reports findings as well as listening. Leaving RulesReporting empty would make this
	// a rule that watched and found nothing, which is a real third case with its own line, and the
	// claim here is narrower: a rule doing its job is not named.
	rules := []rule.Rule{{Name: "working-rule"}}
	coverage := program.Coverage{
		RulesOffered:   map[string]int{"working-rule": 3407},
		RulesListening: map[string]int{"working-rule": 412},
		RulesReporting: map[string]int{"working-rule": 7},
	}

	var out strings.Builder
	writeRuleCoverage(&out, rules, coverage)
	if out.String() != "" {
		t.Fatalf("a rule that listened to files was named in the coverage note: %q", out.String())
	}
}

// A rule that watched files and reported nothing is the third case, and it was invisible.
//
// The two cases above it are about wiring: nothing offered the rule files, or it declined the ones
// it got. This one looked at real code and had nothing to say, which is either a clean tree or a
// rule that cannot see. Two false positives shipped past a full fixture pair tonight and were caught
// only by running against the tree, so the distinction is worth a line of output rather than a habit.
func TestARuleThatWatchedAndFoundNothingIsCounted(t *testing.T) {
	rules := []rule.Rule{{Name: "watched-and-quiet"}, {Name: "found-something"}}
	coverage := program.Coverage{
		RulesOffered:   map[string]int{"watched-and-quiet": 3407, "found-something": 3407},
		RulesListening: map[string]int{"watched-and-quiet": 3407, "found-something": 3407},
		RulesReporting: map[string]int{"found-something": 12},
	}

	var out strings.Builder
	writeRuleCoverage(&out, rules, coverage)

	if !strings.Contains(out.String(), "1 rules watched files and reported nothing") {
		t.Fatalf("a rule that watched and reported nothing was not counted:\n%s", out.String())
	}
	if strings.Contains(out.String(), "watched-and-quiet was offered no files") {
		t.Errorf("a rule that listened was described as unwired:\n%s", out.String())
	}
}

// The other direction: a run where every rule reported something says nothing about this case.
//
// Without it, a counter that incremented unconditionally would pass the test above while claiming
// every rule was quiet, which is worse than not counting: it would tell a reader to distrust a run
// that had nothing wrong with it.
func TestARunWhereEveryRuleReportedCountsNoQuietRules(t *testing.T) {
	rules := []rule.Rule{{Name: "found-something"}}
	coverage := program.Coverage{
		RulesOffered:   map[string]int{"found-something": 3407},
		RulesListening: map[string]int{"found-something": 3407},
		RulesReporting: map[string]int{"found-something": 12},
	}

	var out strings.Builder
	writeRuleCoverage(&out, rules, coverage)

	if strings.Contains(out.String(), "watched files and reported nothing") {
		t.Errorf("a run where every rule reported claimed a quiet rule:\n%s", out.String())
	}
}
