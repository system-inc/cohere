package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
)

// Two printf wrapper facts that are no function's, each shown on a pair of modules as #7v2bzvd's deprecation was
// (#zqx7gke): one on an interface method, one on a variable holding a function literal. meet leaves out every method
// of an interface and meets a *types.Func alone, so unit B once compared neither, and a body whose only printf callee
// was one of these stood where the reach left it, its printf finding the host's while the build's differed. The host's
// side meets them now (meetPushed), standing_facts compares each one's fact as a function's, and the second leg
// compares each one a standing body uses (compareStandingPushed).
//
// The build's own findings with the standing bodies' switch on were those with it off all the same: printf is a
// Composes rule (rules/rule_inputs.txt), never confined (standingConfinedRules), so it hears every body and keeps its
// own findings. What was wrong is the standing set, which the census counts and a confined printf would trust.

// printfIn is whether findings hold a printf finding in a file named file.
func printfIn(findings []check.Finding, file string) bool {
	return slices.ContainsFunc(findings, func(finding check.Finding) bool {
		return finding.Rule == "cohere-go/printf" && filepath.Base(finding.File) == file
	})
}

// requireOwnFindingsAlike fails unless windows's own findings over fixture are the same with the standing bodies'
// switch off and on.
func requireOwnFindingsAlike(test *testing.T, fixture string) {
	test.Helper()
	var own [2][]string
	for index, switched := range []bool{false, true} {
		root := copyFixture(test, filepath.Join("testdata", fixture))
		engine, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
			engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
			engine.standingBodies = switched
		}, "--no-cache", "--no-fix", "--build-dependence")
		if len(engine.buildRuns) != 2 {
			test.Fatalf("want the host's build and windows's, got %d runs", len(engine.buildRuns))
		}
		own[index] = rawFindings(engine.buildRuns[1], root)
	}
	if !slices.Equal(own[0], own[1]) {
		test.Errorf("windows's own findings with the standing bodies' switch on are\n%s\nwant those with it off\n%s",
			strings.Join(own[1], "\n"), strings.Join(own[0], "\n"))
	}
}

// requireNoDifferentialFailure fails unless windows's body differential, both legs, failed nothing.
func requireNoDifferentialFailure(test *testing.T, settled *factKindsSettled) {
	test.Helper()
	differential := settled.run.bodyDifferential
	if differential == nil {
		test.Fatal("windows's build ran no body differential")
	}
	if len(differential.failures) > 0 {
		test.Errorf("windows's differential failed, the second leg %v of it\n%s\nwant nothing standing that differs",
			differential.standingFailed, strings.Join(differential.failures, "\n"))
	}
}

// printf finds an interface method a wrapper in a file of go1.26 or later where a method implementing it is one, which
// satisfy reads off any assignment of the package (wrapper_search.go's methodImplementations). In both fixtures api's
// shared file declares Logger and printer, whose Logf forwards to fmt.Printf, and a platform file alone assigns a
// printer to a Logger: the host's in the -host fixture, windows's in the -extra one. So Logger.Logf is a wrapper in
// exactly that build. Target calls Logf through a Logger and names nothing else, so the reach leaves it standing, and
// on the host's findings it would invent windows a printf finding in the first and lose windows's in the second.
// Logger.Logf's fact names it and takes Target out, and the differential fails nothing.
func TestAPrintfWrapperFoundThroughAnInterfaceMethodTakesItsUserOut(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		fixture string
		module  string
		// onHost is whether the host's build holds the assignment, and windows's does not; else the reverse.
		onHost bool
	}{
		{"fact-printf-interface-host", "example.com/printfinterfacehost", true},
		{"fact-printf-interface-extra", "example.com/printfinterfaceextra", false},
	} {
		test.Run(each.fixture, func(test *testing.T) {
			test.Parallel()

			settled := settleFactFixture(test, each.fixture, each.module+"/reader")
			if reason := settled.bodies["Target"].reason; reason != "" {
				test.Fatalf("the reach reached Target for %q, want it unreached, so that only the facts can take it "+
					"out", reason)
			}
			settled.requireRemovedBy(test, "Target", "Logf", "printf wrapper")
			if printfIn(settled.host.result.Findings, "reader.go") != each.onHost ||
				printfIn(settled.run.result.Findings, "reader.go") == each.onHost {
				test.Errorf("Target's call is a printf finding on the host %t and on windows %t, want the host's %t "+
					"and windows's the other", printfIn(settled.host.result.Findings, "reader.go"),
					printfIn(settled.run.result.Findings, "reader.go"), each.onHost)
			}
			requireNoDifferentialFailure(test, settled)
			requireOwnFindingsAlike(test, each.fixture)
		})
	}
}

// printf finds a function literal assigned to a variable a wrapper, its fact on the variable, where the literal
// forwards to one (wrapper_search.go's gather and assignedVariable). In both fixtures api's shared file declares three
// such variables, each handing its format and values to emit, which each platform's file declares, forwarding to
// fmt.Printf in one build alone: the host's in the -host fixture, windows's in the -extra one.
//
// Logf declares no type, so the graph gives it what its initializer uses (build_dependence's collectValue), emit
// among them, whose declaration differs: Logf's name bit is set, and the reach reaches Target, which names it, by
// Named. Its findings are the build's own, and nothing more is owed. TypedLogf states its type, and AssignedLogf is
// given its literal in init, so neither takes a bit from emit: the reach leaves TypedTarget and AssignedTarget
// standing, and on the host's findings each would invent windows a printf finding in the first and lose windows's in
// the second. Each variable's fact names it and takes its user out, and the differential fails nothing.
func TestAPrintfWrapperHeldByAVariableTakesItsUserOut(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		fixture string
		module  string
		// onHost is whether the host's emit forwards to fmt.Printf, and windows's does not; else the reverse.
		onHost bool
	}{
		{"fact-printf-literal-host", "example.com/printfliteralhost", true},
		{"fact-printf-literal-extra", "example.com/printfliteralextra", false},
	} {
		test.Run(each.fixture, func(test *testing.T) {
			test.Parallel()

			settled := settleFactFixture(test, each.fixture, each.module+"/stated")
			target := slices.IndexFunc(settled.run.bodyReach.bodies, func(body reachedBody) bool {
				return body.packageIdentifier == each.module+"/reader" && body.name == "Target"
			})
			if target < 0 {
				test.Fatal("the census lists no body Target")
			}
			if body := settled.run.bodyReach.bodies[target]; body.reason != reachedNamed ||
				!strings.HasPrefix(body.driver, each.module+"/api.Logf ") {
				test.Errorf("the reach reached Target for %q, by %q, want it reached by Named, by api.Logf",
					body.reason, body.driver)
			}
			for user, callee := range map[string]string{"TypedTarget": "TypedLogf", "AssignedTarget": "AssignedLogf"} {
				if reason := settled.bodies[user].reason; reason != "" {
					test.Fatalf("the reach reached %s for %q, want it unreached, so that only the facts can take it "+
						"out", user, reason)
				}
				settled.requireRemovedBy(test, user, callee, "printf wrapper")
			}
			for _, file := range []string{"reader.go", "stated.go"} {
				if printfIn(settled.host.result.Findings, file) != each.onHost ||
					printfIn(settled.run.result.Findings, file) == each.onHost {
					test.Errorf("%s's calls are printf findings on the host %t and on windows %t, want the host's %t "+
						"and windows's the other", file, printfIn(settled.host.result.Findings, file),
						printfIn(settled.run.result.Findings, file), each.onHost)
				}
			}
			requireNoDifferentialFailure(test, settled)
			requireOwnFindingsAlike(test, each.fixture)
		})
	}
}
