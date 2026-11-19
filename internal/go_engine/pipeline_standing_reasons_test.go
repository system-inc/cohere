package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// standingReason is one module of testdata/standing-reasons, the auditor's six of #bzdng05 made build pairs
// (#3rcmc3h): `target`, in shared.go, is the same in every build, and what it turns on is declared twice, the
// auditor's B in a file only the build tagged audit_b compiles and its A in a `!audit_b` sibling the host compiles.
// The pair is a tag's and not a platform's because a platform's build changes what the standard library declares:
// os/exec.Cmd's name bit is set on windows, so a `target` naming it is reached by its signature before anything this
// unit pins, and a function declared in a platform's file is another object there, reached by its name. On the host's
// platform under a tag, nothing but the module's own pair differs.
//
// The constant modules declare `other` itself in the pair, a typed constant whose folded value differs, and the body
// reach must reach `target` by its Constant reason. The callee modules keep the auditor's `other` in shared.go, where
// it is the same declaration in both builds, and have it call `stop`, which the pair declares returning on the host
// and panicking under the tag: `other`'s no-return fact differs though nothing `target` names does, so the body reach
// leaves `target` unreached and the facts' reach must take it out of the standing set for `other`'s no-return.
type standingReason struct {
	module     string
	bodyReach  bodyReachReason
	factsReach fact_reach.Reason
}

// standingReasons are the six modules, two of a package constant's folded value and four of a same-package callee
// that stops returning, ctrlflow's no-return decision and the representation's cut after the call.
var standingReasons = []standingReason{
	{module: "constant-sql", bodyReach: reachedConstant},
	{module: "constant-command", bodyReach: reachedConstant},
	{module: "callee-flow", factsReach: "no-return"},
	{module: "callee-drain", factsReach: "no-return"},
	{module: "callee-ir", factsReach: "no-return"},
	{module: "callee-recursion", factsReach: "no-return"},
}

// runStandingReason runs the engine over a copy of the standing reasons module named with the registry's rules, under
// the body reach census with `--no-cache --no-fix --build-dependence`, with the standing bodies' switch as switched,
// and returns the pipeline, its stream, normalized, and the copy's root.
func runStandingReason(test *testing.T, module string, switched bool) (*pipeline, string, string) {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", "standing-reasons", module))
	engine, stream, _ := runOverRegistered(test, root, exitFindings, rules.All(), func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
		engine.standingBodies = switched
	}, "--no-cache", "--no-fix", "--build-dependence")
	return engine, normalize(stream, root), root
}

// taggedRun is engine's build tagged audit_b, which fails the test when the run checked none.
func taggedRun(test *testing.T, engine *pipeline) buildRun {
	test.Helper()
	at := slices.IndexFunc(engine.buildRuns, func(run buildRun) bool {
		return slices.Contains(run.build.Tags, "audit_b")
	})
	if at < 0 {
		test.Fatalf("the run checked no build tagged audit_b among its %d", len(engine.buildRuns))
	}
	return engine.buildRuns[at]
}

// targetLeft is why `target` did not stand in run: the body reach census's reason for its body, and the reason the
// facts' reach took it out of the standing set with, each empty where that reach did not, and whether it stands once
// the facts settled. A body the census never judged fails the test, since nothing could then be said of it.
func targetLeft(test *testing.T, run buildRun) (bodyReachReason, fact_reach.Reason, bool) {
	test.Helper()
	at := slices.IndexFunc(run.bodyReach.bodies, func(body reachedBody) bool { return body.name == "target" })
	if at < 0 {
		test.Fatalf("the tagged build's census judged no body named target")
	}
	body := run.bodyReach.bodies[at]
	place := fact_reach.Place{File: body.file, Offset: body.offset}
	var factsReason fact_reach.Reason
	stands := false
	for _, settled := range run.result.FactReachSettled {
		packageSettled, isResult := settled.(*standing_facts.Result)
		if !isResult || packageSettled.Settlement == nil {
			continue
		}
		stands = stands || packageSettled.Settlement.Standing[place]
		for _, removal := range packageSettled.Settlement.Removed {
			if removal.Place == place {
				factsReason = removal.Reason
			}
		}
	}
	return body.reason, factsReason, stands
}

// sharedFindings are run's own findings in shared.go, the file of `target`, with root written as rawFindings writes it.
func sharedFindings(run buildRun, root string) []string {
	return slices.DeleteFunc(rawFindings(run, root), func(found string) bool {
		return !strings.Contains(found, "File:/project/shared.go ")
	})
}

// Neither a callee that stops returning nor a constant whose value differs leaves a body standing with the host's
// findings (#3rcmc3h): over each standing reason module, `target` does not stand in the tagged build, for the reason
// its module expects, and the tagged build's own findings, and the stream's, with the standing bodies' switch on are
// those with it off. shared.go finds otherwise in the tagged build than on the host in each, so a `target` left
// standing with the host's findings would fail the comparison.
func TestNoTargetStandsWhoseCalleeStopsReturningOrWhoseConstantDiffers(test *testing.T) {
	test.Parallel()

	for _, each := range standingReasons {
		test.Run(each.module, func(test *testing.T) {
			test.Parallel()
			off, offStream, offRoot := runStandingReason(test, each.module, false)
			on, onStream, onRoot := runStandingReason(test, each.module, true)
			if len(off.buildRuns) != 2 || len(on.buildRuns) != 2 {
				test.Fatalf("want the host's build and the tagged one in both runs, got %d and %d runs",
					len(off.buildRuns), len(on.buildRuns))
			}
			onTagged, offTagged := taggedRun(test, on), taggedRun(test, off)
			bodyReason, factsReason, stands := targetLeft(test, onTagged)
			if stands || bodyReason != each.bodyReach || factsReason != each.factsReach {
				test.Errorf("in the tagged build target stands %t, reached by %q and taken out by the facts' reach "+
					"for %q, want it not standing, reached by %q and taken out for %q", stands, bodyReason,
					factsReason, each.bodyReach, each.factsReach)
			}
			onFound, offFound := rawFindings(onTagged, onRoot), rawFindings(offTagged, offRoot)
			if !slices.Equal(onFound, offFound) {
				test.Errorf("the tagged build's own findings with the switch on are\n%s\nwant those with it off\n%s",
					strings.Join(onFound, "\n"), strings.Join(offFound, "\n"))
			}
			if !slices.Equal(findingLines(onStream), findingLines(offStream)) {
				test.Errorf("the findings with the switch on are\n%q\nwant those with it off\n%q",
					findingLines(onStream), findingLines(offStream))
			}
			if host, tagged := sharedFindings(on.buildRuns[0], onRoot), sharedFindings(onTagged, onRoot); slices.Equal(
				host, tagged) {
				test.Errorf("shared.go finds the same in the tagged build as on the host, so the module proves "+
					"nothing:\n%s", strings.Join(host, "\n"))
			}
		})
	}
}
