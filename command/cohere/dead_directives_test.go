package main

import (
	"testing"

	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/rules/core"
	"github.com/system-inc/cohere/internal/types/program"
)

// Exercise the relint path used after fixes and by stdin. The same directive is dead with its
// rule selected, untouched without that rule, and live when a debugger is planted under it.
func TestFixRelintRemovesOnlyDeadDisableNames(test *testing.T) {
	test.Parallel()
	for _, fixture := range []struct {
		name, source, fixed string
		rules               []rule.Rule
	}{
		{
			"dead",
			"// cohere-disable-next-line no-debugger\nconst a = 1;\n",
			"\nconst a = 1;\n",
			[]rule.Rule{core.NoDebugger},
		},
		{
			"unselected",
			"// cohere-disable-next-line no-debugger\nconst a = 1;\n",
			"// cohere-disable-next-line no-debugger\nconst a = 1;\n",
			[]rule.Rule{core.NoSelfCompare},
		},
		{
			"live",
			"// cohere-disable-next-line no-debugger\ndebugger;\n",
			"// cohere-disable-next-line no-debugger\ndebugger;\n",
			[]rule.Rule{core.NoDebugger},
		},
		{
			"blanket with skipped type-aware rule",
			"/* cohere-disable */\nconst a = 1;\n",
			"/* cohere-disable */\nconst a = 1;\n",
			[]rule.Rule{core.NoDebugger, {Name: "test-type-aware", NeedsTypeChecker: true}},
		},
		{
			"mixed",
			"// cohere-disable-next-line no-self-compare, no-debugger -- needed\ndebugger;\n",
			"// cohere-disable-next-line no-debugger -- needed\ndebugger;\n",
			[]rule.Rule{core.NoDebugger, core.NoSelfCompare},
		},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			test.Parallel()
			proposals, proposalsForTextError := proposalsForText(
				"/Probe.ts",
				fixture.source,
				&program.Graph{},
				fixture.rules,
			)
			if proposalsForTextError != nil {
				test.Fatal(proposalsForTextError)
			}
			wantProposals := 0
			if fixture.source != fixture.fixed {
				wantProposals = 1
			}
			if len(proposals) != wantProposals {
				test.Fatalf("got %d initial proposals, want %d", len(proposals), wantProposals)
			}
			fixed, fixTextError := edit.FixText(
				"/Probe.ts",
				fixture.source,
				func(fileName, text string) ([]edit.Proposal, error) {
					return proposalsForText(fileName, text, &program.Graph{}, fixture.rules)
				},
				2,
			)
			if fixTextError != nil {
				test.Fatal(fixTextError)
			}
			if fixed.Text != fixture.fixed {
				test.Fatalf("fixed %q, want %q", fixed.Text, fixture.fixed)
			}
		})
	}
}
