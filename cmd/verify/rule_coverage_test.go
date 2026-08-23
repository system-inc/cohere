package main

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/program"
	"github.com/system-inc/verify/internal/rule"
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
	rules := []rule.Rule{{Name: "working-rule"}}
	coverage := program.Coverage{
		RulesOffered:   map[string]int{"working-rule": 3407},
		RulesListening: map[string]int{"working-rule": 412},
	}

	var out strings.Builder
	writeRuleCoverage(&out, rules, coverage)
	if out.String() != "" {
		t.Fatalf("a rule that listened to files was named in the coverage note: %q", out.String())
	}
}
