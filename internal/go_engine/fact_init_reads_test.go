package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// runInitReads runs the engine over a copy of the fact-init-reads module named with the registry's rules, under the
// body reach census with `--no-cache --no-fix --build-dependence`, with the standing bodies' switch as switched, and
// returns the pipeline, its stream, normalized, and the copy's root.
func runInitReads(test *testing.T, module string, switched bool) (*pipeline, string, string) {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", "fact-init-reads", module))
	engine, stream, _ := runOverRegistered(test, root, exitFindings, rules.All(), func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
		engine.standingBodies = switched
	}, "--no-cache", "--no-fix", "--build-dependence")
	return engine, normalize(stream, root), root
}

// An `init` function's body stays a user of what it names (#30dtg3p). The host's side leaves purity's answer about an
// `init` out, its types' alone, so a package with one is settled in part, and what keeps its `init` from standing
// where it must not is that the body is still a place, a user of each object it names, though its declaration
// declares no object. In each fact-init-reads module `init` discards shared's result, and shared is pure exactly
// where platformValue is, which each platform's file declares: on the host alone in host-pure, and on windows alone
// in windows-pure. shared is the same declaration in every build, so the reach leaves `init` unreached and only the
// facts can take it out: windows's build asks shared's purity, which differs, and `init`, shared's user, leaves for
// it. SA4017 on init's statement is then the build's own to find, on the pure side alone, the first leg fails
// nothing, and with the standing bodies' switch on windows takes the host's findings in quiet and Twice alone and
// finds what it finds with the switch off. Were an `init` no user of what it names, it would stand: windows would
// then take the host's finding on init's statement in host-pure, and lose its own in windows-pure.
func TestAnInitWhoseCalleesPurityDiffersNeverStands(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		module   string
		hostPure bool
	}{
		{"host-pure", true},
		{"windows-pure", false},
	} {
		test.Run(each.module, func(test *testing.T) {
			test.Parallel()
			settled := settleFactFixture(test, filepath.Join("fact-init-reads", each.module), "example.com/alone")
			for _, name := range []string{"init", "shared", "quiet", "Twice"} {
				if _, isListed := settled.bodies[name]; !isListed {
					test.Fatalf("the census lists no body %s", name)
				}
			}
			declared := settled.bodies["init"]
			if declared.reason != "" {
				test.Fatalf("the reach reached init for %q, want it unreached, so that only the facts can take it out",
					declared.reason)
			}
			if settled.settlement.Standing[fact_reach.Place{File: declared.file, Offset: declared.offset}] {
				test.Errorf("init stands on the host's findings, want it taken out as shared's user")
			}
			settled.requireRemovedBy(test, "init", "shared", fact_reach.DiffersReason("function_purity"))
			for _, name := range []string{"quiet", "Twice"} {
				body := settled.bodies[name]
				if !settled.settlement.Standing[fact_reach.Place{File: body.file, Offset: body.offset}] {
					test.Errorf("%s does not stand, want it standing, so that the package is settled in part", name)
				}
			}
			discards := func(findings []check.Finding) bool {
				return slices.ContainsFunc(findings, func(finding check.Finding) bool {
					return finding.Rule == "cohere-go/SA4017" && filepath.Base(finding.File) == "alone.go" &&
						finding.Line > declared.line && finding.Line < settled.bodies["shared"].line
				})
			}
			host, windows := discards(settled.host.result.Findings), discards(settled.run.result.Findings)
			if host != each.hostPure || windows == each.hostPure {
				test.Errorf("init's discarded call is a finding on the host %t and on windows %t, want it on the host "+
					"alone %t and else on windows alone", host, windows, each.hostPure)
			}
			differential := settled.run.bodyDifferential
			if differential == nil {
				test.Fatal("windows's build ran no body differential")
			}
			if len(differential.failures) > 0 {
				test.Errorf("windows's first leg failed\n%s\nwant no body standing whose findings differ",
					strings.Join(differential.failures, "\n"))
			}

			off, offStream, offRoot := runInitReads(test, each.module, false)
			on, onStream, onRoot := runInitReads(test, each.module, true)
			if len(off.buildRuns) != 2 || len(on.buildRuns) != 2 {
				test.Fatalf("want the host's build and windows's in both runs, got %d and %d runs", len(off.buildRuns),
					len(on.buildRuns))
			}
			if on.buildRuns[1].standingDeclarations != 2 || off.buildRuns[1].standingDeclarations != 0 {
				test.Errorf("windows took the host's findings in %d declarations with the switch on and %d off, want "+
					"quiet's and Twice's, 2, and 0", on.buildRuns[1].standingDeclarations,
					off.buildRuns[1].standingDeclarations)
			}
			onWindows, offWindows := rawFindings(on.buildRuns[1], onRoot), rawFindings(off.buildRuns[1], offRoot)
			if !slices.Equal(onWindows, offWindows) {
				test.Errorf("windows's own findings with the switch on are\n%s\nwant those with it off\n%s",
					strings.Join(onWindows, "\n"), strings.Join(offWindows, "\n"))
			}
			if !slices.Equal(findingLines(onStream), findingLines(offStream)) {
				test.Errorf("the findings with the switch on are\n%q\nwant those with it off\n%q",
					findingLines(onStream), findingLines(offStream))
			}
		})
	}
}
