package go_engine

import (
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
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

// printf finds a field a wrapper where a function literal assigned to it, `x.Logf = func(...) {...}`, forwards to one,
// its fact on the field (wrapper_search.go's assignedVariable, through the field's selection). In both fixtures api's
// shared file declares Logger, whose Logf field holds a print-shaped function, and a platform file alone assigns a
// literal forwarding to fmt.Printf to a Logger's Logf: the host's in the -host fixture, windows's in the -extra one. So
// the field is a wrapper in exactly that build. Target calls Logf on a Logger it is handed and names nothing else, so
// the reach leaves it standing, and on the host's findings it would invent windows a printf finding in the first and
// lose windows's in the second. The field's fact names it and takes Target out, and the differential fails nothing.
func TestAPrintfWrapperHeldByAFieldTakesItsUserOut(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		fixture string
		module  string
		// onHost is whether the host's build holds the assignment, and windows's does not; else the reverse.
		onHost bool
	}{
		{"fact-printf-field-host", "example.com/printffieldhost", true},
		{"fact-printf-field-extra", "example.com/printffieldextra", false},
	} {
		test.Run(each.fixture, func(test *testing.T) {
			test.Parallel()

			settled := settleFactFixture(test, each.fixture, each.module+"/reader")
			if reason := settled.bodies["Target"].reason; reason != "" {
				test.Fatalf("the reach reached Target for %q, want it unreached, so that only the facts can take it "+
					"out", reason)
			}
			// The field's key names it by its objectpath, Logger's first field.
			settled.requireRemovedBy(test, "Target", "Logger.UF0", "printf wrapper")
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

// A field of a generic type is a wrapper as an ordinary one is (seats' review-1, must 1): printf gathers the literal
// assigned through GenericLogger[any].Logf by the literal's own signature, func(string, ...any), and exports its fact
// on the generic field, whose declared type, func(string, ...T), has no printf function's shape until T is any. The
// record names the generic field, so the host's side has to admit it by the shape any instance of it could take
// (printf.PrintShaped), else it compares nothing and Target stands on the host's findings, inventing windows a printf
// finding in the -host fixture and losing windows's in the -extra one.
func TestAPrintfWrapperHeldByAGenericFieldTakesItsUserOut(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		fixture string
		module  string
		// onHost is whether the host's build holds the assignment, and windows's does not; else the reverse.
		onHost bool
	}{
		{"fact-printf-generic-field-host", "example.com/printfgenericfieldhost", true},
		{"fact-printf-generic-field-extra", "example.com/printfgenericfieldextra", false},
	} {
		test.Run(each.fixture, func(test *testing.T) {
			test.Parallel()

			settled := settleFactFixture(test, each.fixture, each.module+"/reader")
			if reason := settled.bodies["Target"].reason; reason != "" {
				test.Fatalf("the reach reached Target for %q, want it unreached, so that only the facts can take it "+
					"out", reason)
			}
			// The field's key names it by its objectpath, GenericLogger's first field.
			settled.requireRemovedBy(test, "Target", "GenericLogger.UF0", "printf wrapper")
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

// printf finds a function literal assigned to another package's variable a wrapper where it forwards to one, as it does
// one assigned to its own package's (wrapper_search.go's gather and assignedVariable), and calls of the variable read
// that kind in the assigning package. But it exports a fact only on its own package's objects (propagate), so the kind
// is the package's alone, in no build's facts, and no comparison of facts can see it differ (seats' review-1, must 2).
// A field of another package's type is the same (assignedVariable, through the field's selection). In both fixtures
// reader gives api.Logf and an api.Logger's Logf field, in a shared file, a literal each forwarding to impl.Emit, which
// forwards to fmt.Printf in one build alone, and gives api.Tracef, in a platform file of its own alone, a literal
// forwarding to impl.Printf, a wrapper everywhere: the host's in the -host fixture, windows's in the -extra one.
// Target, FieldTarget and TraceTarget call one each and name nothing else, so the reach leaves them standing, with
// api's facts of all three empty in both builds, and on the host's findings each would invent windows a printf finding
// in the first and lose windows's in the second, and neither leg of the differential failed. Each variable or field of
// another package of a print shape a body uses is named for that kind now (fact_reach.PushedObject.Local), so each
// target leaves, and the differential fails nothing; and the second leg fails a standing user of one, which it does
// here where the naming is taken away.
func TestAPrintfWrapperFoundForAnImportedVariableTakesItsUserOut(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		fixture string
		module  string
		// onHost is whether the host's build makes both variables wrappers in reader, and windows's does not; else the
		// reverse.
		onHost bool
	}{
		{"fact-printf-imported-host", "example.com/printfimportedhost", true},
		{"fact-printf-imported-extra", "example.com/printfimportedextra", false},
	} {
		test.Run(each.fixture, func(test *testing.T) {
			test.Parallel()

			settled := settleFactFixture(test, each.fixture, each.module+"/reader")
			for user, callee := range map[string]string{
				"Target": "Logf", "FieldTarget": "Logger.UF0", "TraceTarget": "Tracef",
			} {
				if reason := settled.bodies[user].reason; reason != "" {
					test.Fatalf("the reach reached %s for %q, want it unreached, so that only the facts can take it "+
						"out", user, reason)
				}
				settled.requireRemovedBy(test, user, callee, "local printf wrapper")
			}
			if printfIn(settled.host.result.Findings, "reader.go") != each.onHost ||
				printfIn(settled.run.result.Findings, "reader.go") == each.onHost {
				test.Errorf("the targets' calls are printf findings on the host %t and on windows %t, want the host's "+
					"%t and windows's the other", printfIn(settled.host.result.Findings, "reader.go"),
					printfIn(settled.run.result.Findings, "reader.go"), each.onHost)
			}
			requireNoDifferentialFailure(test, settled)
			requireOwnFindingsAlike(test, each.fixture)
		})
	}
}

// Two of the build's objects render one identity of another package only where two packages of one path are in the
// build's closure (seats' review-1's question). The second leg once took whichever of them a map's order met first, so
// a standing user of the identity passed or failed by chance; it reads such a match as neither object's fact now, and
// fails each time. It is asked many times, since each call ranges its map anew.
func TestTheSecondLegReadsAnIdentityTwoObjectsShareAsNeithers(test *testing.T) {
	test.Parallel()

	// variable is a package-level variable Logf of a package of its own, every one at the path example.com/api.
	variable := func() *types.Var {
		api := types.NewPackage("example.com/api", "api")
		logf := types.NewVar(token.NoPos, api, "Logf", types.Typ[types.Int])
		api.Scope().Insert(logf)
		return logf
	}
	first, second := variable(), variable()
	renderer := build_reads.NewIdentityRenderer(token.NewFileSet(), nil)
	identity := renderer.Object(first)
	key, isKnown := factReachObject(identity)
	if !isKnown {
		test.Fatalf("want Logf's identity known, got %s", identity)
	}
	place := fact_reach.Place{File: "reader.go", Offset: 10}
	hosted := &factReachPackage{users: map[fact_reach.Object][]fact_reach.Place{key: {place}},
		pushedObjects: []fact_reach.PushedObject{{Object: key, Identity: identity,
			Pushed: map[string]string{printf.Analyzer.Name: "printfWrapper"}}}}
	captured := &check.FactReachPackage{Pushed: map[string]map[types.Object]string{
		printf.Analyzer.Name: {first: "printfWrapper", second: ""},
	}}
	for range 32 {
		differential := &buildBodyDifferential{standingCompared: map[string]int{}, standingFailed: map[string]int{}}
		var failures []string
		differential.compareStandingPushed(hosted, map[fact_reach.Place]bool{place: true}, captured,
			&packages.Package{PkgPath: "example.com/reader"}, renderer,
			func(_ fact_reach.Place, name string, kind string, host string, built string) {
				failures = append(failures, name+" "+kind+": host "+host+", build "+built)
			})
		if len(failures) != 1 || !strings.Contains(failures[0], "one of 2 objects") {
			test.Fatalf("the second leg failed %q, want one failure reading the identity as neither object's", failures)
		}
	}
}

// A body that only takes a print-shaped object of its own package as a value, `_ = logger.Logf`, has printf look no
// fact up of it in either build (seats' review-1, a should). It once left for "unknown printf wrapper", as a function
// printf never looked up does. But the package's own search is the one place a fact of its own object is made, and
// every object it makes one of is among those it holds, so one it never looked up holds no fact in the build, and,
// the host's fact empty too, Target stands, its variable, field and interface method compared equal. The second leg
// compares the three as Target stands, and fails nothing.
func TestAPrintShapedObjectOfItsOwnNoCallNamesLeavesItsUserStanding(test *testing.T) {
	test.Parallel()

	settled := settleFactFixture(test, "fact-printf-unread", "example.com/printfunread/reader")
	if reason := settled.bodies["Target"].reason; reason != "" {
		test.Fatalf("the reach reached Target for %q, want it unreached, so that only the facts can take it out",
			reason)
	}
	if removal, removed := settled.removal(test, "Target"); removed {
		test.Errorf("Target left the standing set by %q for %q, want it standing", removal.Object, removal.Reason)
	}
	requireNoDifferentialFailure(test, settled)
}

// The second leg alone (seats' review-1, a should): every fixture above takes its target out before the differential
// runs, so none reaches compareStandingPushed's failures, and once the facts' reach compares what the second leg
// reads, no fixture can: a fact that differs names its object, and its users leave. So the leg is asked here of a
// host's side and a build's capture made by hand, a user of api.Logf standing or not, each fact the host's or not.
func TestTheSecondLegFailsAStandingUserOfAPushedObjectThatDiffers(test *testing.T) {
	test.Parallel()

	api := types.NewPackage("example.com/api", "api")
	logf := types.NewVar(token.NoPos, api, "Logf", types.Typ[types.Int])
	api.Scope().Insert(logf)
	renderer := build_reads.NewIdentityRenderer(token.NewFileSet(), nil)
	identity := renderer.Object(logf)
	key, isKnown := factReachObject(identity)
	if !isKnown {
		test.Fatalf("want Logf's identity known, got %s", identity)
	}
	place := fact_reach.Place{File: "reader.go", Offset: 10}
	for _, each := range []struct {
		name     string
		hosted   string
		built    map[types.Object]string
		local    bool
		stands   bool
		failures []string
	}{
		{"equal", "printfWrapper", map[types.Object]string{logf: "printfWrapper"}, false, true, nil},
		{"lost", "printfWrapper", map[types.Object]string{}, false, true,
			[]string{`example.com/api.Logf () printf: host "printfWrapper", build ""`}},
		{"invented", "", map[types.Object]string{logf: "printfWrapper"}, false, true,
			[]string{`example.com/api.Logf () printf: host "", build "printfWrapper"`}},
		{"no standing user", "printfWrapper", map[types.Object]string{}, false, false, nil},
		{"local", "", map[types.Object]string{}, true, true,
			[]string{"example.com/api.Logf () local printf wrapper: host a kind in no fact, build unread"}},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()

			hosted := &factReachPackage{users: map[fact_reach.Object][]fact_reach.Place{key: {place}},
				pushedObjects: []fact_reach.PushedObject{{Object: key, Identity: identity,
					Pushed: map[string]string{printf.Analyzer.Name: each.hosted}, Local: each.local}}}
			captured := &check.FactReachPackage{Pushed: map[string]map[types.Object]string{
				printf.Analyzer.Name: each.built,
			}}
			differential := &buildBodyDifferential{standingCompared: map[string]int{}, standingFailed: map[string]int{}}
			var failures []string
			differential.compareStandingPushed(hosted, map[fact_reach.Place]bool{place: each.stands}, captured,
				&packages.Package{PkgPath: "example.com/reader"}, renderer,
				func(failed fact_reach.Place, name string, kind string, host string, built string) {
					if failed != place {
						test.Errorf("the second leg failed at %v, want the standing user %v", failed, place)
					}
					failures = append(failures, name+" "+kind+": host "+host+", build "+built)
				})
			if !slices.Equal(failures, each.failures) {
				test.Errorf("the second leg failed %q, want %q", failures, each.failures)
			}
		})
	}
}
