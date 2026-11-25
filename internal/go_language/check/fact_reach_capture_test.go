package check

import (
	"errors"
	"go/ast"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_reach"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules"
	"github.com/system-inc/cohere/internal/go_language/rules/return_nil_ness"
	"github.com/system-inc/cohere/internal/go_language/rules/standing_presence"
)

// reachFiles are the workspace the capture of the facts' reach is read over. reach holds a cycle above its floor,
// CycleOuter allocating, an unexported function never nil, and an unexported function that never returns. floored
// holds a cycle at its floor, Ping and Pong, both maybe nil. probed is the one package reachProbe, a kind that declares
// no order bound, answers about.
var reachFiles = map[string]string{
	"go.mod": "module example.com/reach\n\ngo 1.24\n",
	"reach/reach.go": `// Package reach is the package whose answers and no-return the capture holds.
package reach

// Node is a link of a list.
type Node struct {
	Next *Node
}

// CycleOuter is never nil, whatever CycleInner returns.
func CycleOuter(node *Node) *Node {
	if node != nil {
		node.Next = CycleInner(node.Next)
	}
	return &Node{}
}

// CycleInner returns what CycleOuter returns.
func CycleInner(node *Node) *Node {
	return CycleOuter(node)
}

// fresh is never nil, and unexported, so no exported fact names it.
func fresh() *Node {
	return &Node{}
}

// stop never returns, and is unexported, so no exported fact names it.
func stop() {
	panic("stop")
}

// Run returns whenever stopping is false, and is never nil.
func Run(stopping bool) *Node {
	if stopping {
		stop()
	}
	return fresh()
}
`,
	"floored/floored.go": `// Package floored holds a cycle at its floor.
package floored

// Node is a link of a list.
type Node struct {
	Next *Node
}

// Ping returns nil at the end of the list, or what Pong returns.
func Ping(node *Node) *Node {
	if node == nil {
		return nil
	}
	return Pong(node.Next)
}

// Pong returns what Ping returns.
func Pong(node *Node) *Node {
	return Ping(node)
}
`,
	"probed/probed.go": `// Package probed is the one package reachProbe answers about.
package probed

// Probed is answered about by reachProbe.
func Probed() int {
	return 1
}
`,
}

// reachProbe is a kind that declares no order bound, registered nowhere, answering true of every function of probed.
var reachProbe = &fact_resolver.Kind{Name: "reach_probe"}

// reachNilnessAnalyzer asks the nilness of the first result of every package-level function of its package that has
// one, so the resolver holds an answer about each of them.
var reachNilnessAnalyzer = &analysis.Analyzer{
	Name:     "reach_nilness",
	Doc:      "asks the nilness of every package-level function's first result",
	Requires: []*analysis.Analyzer{return_nil_ness.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		nilness, isNilness := pass.ResultOf[return_nil_ness.Analyzer].(*return_nil_ness.Result)
		if !isNilness {
			return nil, nil
		}
		for _, name := range pass.Pkg.Scope().Names() {
			if function, isFunction := pass.Pkg.Scope().Lookup(name).(*types.Func); isFunction &&
				function.Signature().Results().Len() > 0 {
				nilness.Nilness(function, 0)
			}
		}
		return nil, nil
	},
}

// reachProbeAnalyzer asks reachProbe of every package-level function of probed, and of nothing elsewhere.
var reachProbeAnalyzer = &analysis.Analyzer{
	Name:     "reach_probe",
	Doc:      "asks a kind with no order bound of every function of probed",
	Requires: []*analysis.Analyzer{ctrlflow.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		if pass.Pkg.Name() != "probed" {
			return nil, nil
		}
		resolver, resolverError := fact_resolver.Of(pass)
		if resolverError != nil {
			return nil, resolverError
		}
		reader := resolver.Package(pass.Pkg)
		for _, name := range pass.Pkg.Scope().Names() {
			if function, isFunction := pass.Pkg.Scope().Lookup(name).(*types.Func); isFunction {
				fact_resolver.Ask(resolver, reader, reachProbe, types.Object(function),
					func(*fact_resolver.Package) bool { return true }, func() bool { return false })
			}
		}
		return nil, nil
	},
}

// reachRules are the rules a capture test runs.
var reachRules = []rule.Rule{houseRule(reachNilnessAnalyzer), houseRule(reachProbeAnalyzer)}

// loadReach writes the reach workspace into a directory of the test's own and loads it.
func loadReach(test *testing.T) []*packages.Package {
	test.Helper()
	root := test.TempDir()
	writeFiles(test, root, reachFiles)
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the reach workspace: %v %+v", loadError, loaded)
	}
	return loaded.Packages
}

// capturedOf is the package of capture whose path is path, failing the test when the capture holds none.
func capturedOf(
	test *testing.T,
	capture *FactReachCapture,
	analyzed []*packages.Package,
	path string,
) *FactReachPackage {
	test.Helper()
	for _, analyzedPackage := range analyzed {
		if analyzedPackage.PkgPath != path {
			continue
		}
		if hosted := capture.Packages[analyzedPackage.ID]; hosted != nil {
			return hosted
		}
	}
	test.Fatalf("the capture holds no package %s, of %d", path, len(capture.Packages))
	return nil
}

// answeredNames are the names of the objects answers are about, by kind, each as "kind name", in order.
func answeredNames(answers []ExportedFact) []string {
	var names []string
	for _, answer := range answers {
		names = append(names, answer.Analyzer+" "+answer.Object.Name())
	}
	return names
}

// boundNames are the names of bound's objects, in order.
func boundNames(bound []types.Object) []string {
	var names []string
	for _, object := range bound {
		names = append(names, object.Name())
	}
	return names
}

// With Options.FactReach the run hands back what each analyzed package's host's side is made of (#aswfm7a), and
// without it nothing. reach holds an answer about the unexported fresh, beside the cycle's and Run's, and its
// no-return names the unexported stop and not Run; its order is known, nil-ness being the one kind answering there,
// and bound by CycleOuter and CycleInner, a cycle above its floor. floored's cycle is at its floor, so its order is
// known and bound by nothing. probed holds reachProbe's answer, a kind with no bound, so its order is not known. Every
// answer of the run is handed once more, each with its package.
func TestTheRunCapturesTheFactsReachUnderItsSwitch(test *testing.T) {
	test.Parallel()
	analyzed := loadReach(test)
	off, offError := Run(analyzed, reachRules, Options{})
	if offError != nil {
		test.Fatalf("running without the switch: %v", offError)
	}
	if off.FactReach != nil {
		test.Errorf("without the switch the run captured %+v, want nothing", off.FactReach)
	}
	on, onError := Run(analyzed, reachRules,
		Options{FactReach: true, FactReachPresence: standing_presence.ReadPackage})
	if onError != nil {
		test.Fatalf("running with the switch: %v", onError)
	}
	if on.FactReach == nil {
		test.Fatal("with the switch the run captured nothing")
	}
	capture := on.FactReach
	if len(capture.Packages) != 3 {
		test.Errorf("the capture holds %d packages, want reach, floored and probed", len(capture.Packages))
	}

	reach := capturedOf(test, capture, analyzed, "example.com/reach/reach")
	wantAnswered := []string{"return_nil_ness CycleOuter", "return_nil_ness CycleInner", "return_nil_ness fresh",
		"return_nil_ness Run"}
	if answered := answeredNames(reach.Answers); !slices.Equal(answered, wantAnswered) {
		test.Errorf("reach's answers are about %q, want %q", answered, wantAnswered)
	}
	for _, answer := range reach.Answers {
		if answer.Package == nil || answer.Package.PkgPath != "example.com/reach/reach" || !answer.Answer ||
			answer.FactType != answerFactType || len(answer.Encoded) == 0 {
			test.Errorf("reach's answer about %s is %+v, want an answer of reach, encoded", answer.Object.Name(),
				answer)
		}
	}
	scope := reach.Answers[0].Object.Pkg().Scope()
	for name, want := range map[string]bool{"stop": true, "Run": false, "fresh": false} {
		function, isFunction := scope.Lookup(name).(*types.Func)
		if !isFunction {
			test.Fatalf("reach declares no function %s", name)
		}
		if noReturn := reach.NoReturn(function); noReturn != want {
			test.Errorf("reach's no-return of %s is %t, want %t", name, noReturn, want)
		}
	}
	if bound := boundNames(reach.OrderBound); !reach.OrderKnown ||
		!slices.Equal(bound, []string{"CycleOuter", "CycleInner"}) {
		test.Errorf("reach's order is known %t and bound by %q, want known and CycleOuter and CycleInner",
			reach.OrderKnown, bound)
	}

	floored := capturedOf(test, capture, analyzed, "example.com/reach/floored")
	if answered := answeredNames(floored.Answers); !slices.Equal(answered,
		[]string{"return_nil_ness Ping", "return_nil_ness Pong"}) {
		test.Errorf("floored's answers are about %q, want Ping and Pong", answered)
	}
	if !floored.OrderKnown || len(floored.OrderBound) != 0 {
		test.Errorf("floored's order is known %t and bound by %q, want known and by nothing", floored.OrderKnown,
			boundNames(floored.OrderBound))
	}

	probed := capturedOf(test, capture, analyzed, "example.com/reach/probed")
	if answered := answeredNames(probed.Answers); !slices.Equal(answered, []string{"reach_probe Probed"}) {
		test.Errorf("probed's answers are about %q, want reachProbe's about Probed", answered)
	}
	if probed.OrderKnown {
		test.Errorf("probed holds an answer of a kind with no order bound, and its order is known, bound by %q",
			boundNames(probed.OrderBound))
	}

	var every []string
	for _, answer := range capture.Answers {
		if answer.Package == nil {
			test.Errorf("the run's answer about %s has no package", answer.Object.Name())
		}
		every = append(every, answer.Analyzer+" "+answer.Object.Name())
	}
	for _, hosted := range []*FactReachPackage{reach, floored, probed} {
		for _, name := range answeredNames(hosted.Answers) {
			if !slices.Contains(every, name) {
				test.Errorf("the run's answers %q hold no %s, which a package's answers hold", every, name)
			}
		}
	}
}

// aloneFiles are the workspace the capture's marks are read over: one package with an `init` function beside double,
// which is pure, and Twice, which discards double's result.
var aloneFiles = map[string]string{
	"go.mod": "module example.com/alone\n\ngo 1.24\n",
	"alone.go": "// Package alone declares an init beside a call whose result nothing reads.\npackage alone\n\n" +
		"// started counts the package's starts.\nvar started int\n\n" +
		"func init() {\n\tstarted++\n}\n\n" +
		"// double is pure.\nfunc double(value int) int {\n\treturn value * 2\n}\n\n" +
		"// Twice discards double's result.\nfunc Twice() int {\n\tdouble(2)\n\treturn started\n}\n",
}

// initProbe is a kind that declares no hook for the answers types alone decide, registered nowhere, answering true of
// every `init` function.
var initProbe = &fact_resolver.Kind{Name: "init_probe"}

// initProbeAnalyzer asks initProbe of every `init` function its package's files declare, the function purity's hook
// marks purity's own answer about.
var initProbeAnalyzer = &analysis.Analyzer{
	Name:     "init_probe",
	Doc:      "asks a kind with no types-alone hook of every init function",
	Requires: []*analysis.Analyzer{ctrlflow.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		resolver, resolverError := fact_resolver.Of(pass)
		if resolverError != nil {
			return nil, resolverError
		}
		reader := resolver.Package(pass.Pkg)
		for _, file := range pass.Files {
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction || function.Recv != nil || function.Name.Name != "init" {
					continue
				}
				if declared, isDeclared := pass.TypesInfo.Defs[function.Name].(*types.Func); isDeclared {
					fact_resolver.Ask(resolver, reader, initProbe, types.Object(declared),
						func(*fact_resolver.Package) bool { return true }, func() bool { return false })
				}
			}
		}
		return nil, nil
	},
}

// captureAlone runs the registry's SA4017, which asks the purity of each call's callee whose results nothing reads,
// and the rules of beside over the alone workspace, with the facts' reach captured, and returns the run's capture and
// the package's.
func captureAlone(test *testing.T, beside ...rule.Rule) (*FactReachCapture, *FactReachPackage) {
	test.Helper()
	root := test.TempDir()
	writeFiles(test, root, aloneFiles)
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the workspace: %v %+v", loadError, loaded)
	}
	var asking []rule.Rule
	for _, registered := range rules.All() {
		if registered.Name == rule.Namespace+"SA4017" {
			asking = append(asking, registered)
		}
	}
	if len(asking) != 1 {
		test.Fatalf("the registry holds %d rules named SA4017, want 1", len(asking))
	}
	on, runError := Run(loaded.Packages, append(asking, beside...),
		Options{FactReach: true, FactReachPresence: standing_presence.ReadPackage})
	if runError != nil {
		test.Fatalf("running with the switch: %v", runError)
	}
	if on.FactReach == nil {
		test.Fatal("with the switch the run captured nothing")
	}
	return on.FactReach, capturedOf(test, on.FactReach, loaded.Packages, "example.com/alone")
}

// markedNames are the names of the objects answers are about, by kind, each as "kind name", in order, with "marked"
// after one marked as its types' alone.
func markedNames(answers []ExportedFact) []string {
	var names []string
	for _, answer := range answers {
		if answer.TypesAlone {
			names = append(names, answer.Analyzer+" "+answer.Object.Name()+" marked")
		} else {
			names = append(names, answer.Analyzer+" "+answer.Object.Name())
		}
	}
	return names
}

// The capture marks each answer its kind says its object's types alone decide (fact_resolver.Kind.TypesAlone), among a
// package's answers and among the run's alike, and no other. Under SA4017, which asks the purity of each call's callee
// whose results nothing reads, the package's initializer calls its `init`, so the run holds purity's answer about a
// function no identity names: marked, since purity answers not pure for every function with no results. Its answer
// about double, whose result Twice discards, turns on double's body and is not marked.
func TestTheCaptureMarksTheAnswersTheirKindSaysTypesAloneDecide(test *testing.T) {
	test.Parallel()
	capture, alone := captureAlone(test)
	want := []string{"function_purity init marked", "function_purity double"}
	if got := markedNames(alone.Answers); !slices.Equal(got, want) {
		test.Errorf("the package's answers are %q, want %q", got, want)
	}
	if got := markedNames(capture.Answers); !slices.Equal(got, want) {
		test.Errorf("the run's answers are %q, want %q", got, want)
	}
}

// A kind that declares no hook has no answer marked, whatever the answer is about (fact_resolver.Kind.TypesAlone: nil
// says no answer of the kind is its types' alone). initProbe's answer about the package's `init` function is handed
// unmarked, among the package's answers and the run's alike, beside purity's about the same function, which purity's
// hook marks. An unmarked answer about an `init` is one the host's side counts unplaced, so the package lints whole
// in every build (go_engine's TestAnAnswerAboutAnInitIsLeftOutByItsMarkAlone): nobody has argued the kind's answer is
// the same in every build, and the capture never takes a missing hook for that argument.
func TestTheCaptureMarksNoAnswerOfAKindThatDeclaresNoHook(test *testing.T) {
	test.Parallel()
	if initProbe.TypesAlone != nil {
		test.Fatal("initProbe declares a hook, so the test holds nothing of a kind without one")
	}
	capture, alone := captureAlone(test, houseRule(initProbeAnalyzer))
	want := []string{"function_purity init marked", "init_probe init", "function_purity double"}
	if got := markedNames(alone.Answers); !slices.Equal(got, want) {
		test.Errorf("the package's answers are %q, want %q", got, want)
	}
	if got := markedNames(capture.Answers); !slices.Equal(got, want) {
		test.Errorf("the run's answers are %q, want %q", got, want)
	}
}

// A package the run restored from the findings cache computed nothing, so the capture holds no side of it, and the
// engine lints it whole: a cold run with the cache captures every package, and the warm run after it, which restores
// them all, none.
func TestARestoredPackageHasNoCapture(test *testing.T) {
	test.Parallel()
	analyzed := loadReach(test)
	caching := testCaching(test, reachRules)
	for _, run := range []struct {
		name string
		want int
	}{{name: "cold", want: 3}, {name: "warm", want: 0}} {
		result, runError := Run(analyzed, reachRules, Options{Cache: NewCacheSession(caching, reachRules),
			FactReach: true, FactReachPresence: standing_presence.ReadPackage})
		if runError != nil {
			test.Fatalf("running %s: %v", run.name, runError)
		}
		if result.FactReach == nil || len(result.FactReach.Packages) != run.want {
			test.Errorf("the %s run captured %+v, want %d packages", run.name, result.FactReach, run.want)
		}
	}
}

// A package the run was handed an unsure answer in has no capture, though the resolver learned it (#5vbs1np, must 4):
// low is restored and the store holds nothing of it, so the nilness of high's Via, which returns low.Fresh's pointer,
// rests on a placeholder, and high is unsure. Neither is captured: low computed nothing, and what high computed may
// rest on the placeholder.
func TestAnUnsurePackageHasNoCapture(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeFiles(test, root, map[string]string{
		"go.mod": "module example.com/unsure\n\ngo 1.24\n",
		"low/low.go": "// Package low is restored.\npackage low\n\n" +
			"// Fresh is never nil.\nfunc Fresh() *int {\n\treturn new(int)\n}\n",
		"high/high.go": "// Package high reads low.\npackage high\n\nimport \"example.com/unsure/low\"\n\n" +
			"// Via returns low.Fresh's pointer.\nfunc Via() *int {\n\treturn low.Fresh()\n}\n",
	})
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the unsure workspace: %v %+v", loadError, loaded)
	}
	byPath := map[string]*packages.Package{}
	for _, loadedPackage := range loaded.Packages {
		byPath[loadedPackage.PkgPath] = loadedPackage
	}
	low, high := byPath["example.com/unsure/low"], byPath["example.com/unsure/high"]
	if low == nil || high == nil {
		test.Fatalf("the load holds %v, want low and high", byPath)
	}
	options := DispatchOptions()
	resolver := fact_resolver.New(answerlessStore{})
	options.Resolver = resolver
	options.Restored = func(restoring *packages.Package) bool { return restoring == low }
	options.Restore = func(*checker.Action) error { return nil }
	if _, analyzeError := analyzeWrapped([]*analysis.Analyzer{reachNilnessAnalyzer}, []*packages.Package{low, high},
		options); analyzeError != nil {
		test.Fatalf("analyzing the unsure workspace: %v", analyzeError)
	}
	if !resolver.Unsure(high.Types) || resolver.Package(high.Types) == nil {
		test.Fatalf("high is unsure %t and learned %t, want both", resolver.Unsure(high.Types),
			resolver.Package(high.Types) != nil)
	}
	capture := captureFactReach(resolver, []*packages.Package{low, high}, nil, nil, BodyRecorder{})
	if len(capture.Packages) != 0 {
		test.Errorf("the capture holds %d packages, want none: low restored and high unsure", len(capture.Packages))
	}
}

// pushingAnalyzer marks every package-level function of its package with markedFact, and fails after when fails is
// set, for the capture's pushed facts: name names it. No run holds two, nor one beside markedAnalyzer, since an
// analyzer may not register a fact type another does.
func pushingAnalyzer(name string, fails bool) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:      name,
		Doc:       "marks every package-level function, and may fail after",
		FactTypes: []analysis.Fact{new(markedFact)},
		Run: func(pass *analysis.Pass) (any, error) {
			for _, name := range pass.Pkg.Scope().Names() {
				if function, isFunction := pass.Pkg.Scope().Lookup(name).(*types.Func); isFunction {
					pass.ExportObjectFact(function, new(markedFact))
				}
			}
			if fails {
				return nil, errors.New("marking " + pass.Pkg.Path() + " failed after")
			}
			return nil, nil
		},
	}
}

// The capture holds no pushed fact of an action that failed, whose facts no importer could trust, though it exported
// them before it failed (#5vbs1np): reach's marks by the failing analyzer, one for each of its package-level functions,
// are not held at all, while the deprecation facts' analyzer, which ran to the end, is held, with none.
func TestTheCaptureHoldsNoPushedFactOfAFailedAction(test *testing.T) {
	test.Parallel()
	analyzed := loadReach(test)
	failing := pushingAnalyzer("pushing_failed", true)
	result, runError := Run(analyzed, []rule.Rule{houseRule(reachNilnessAnalyzer), houseRule(deprecated.Analyzer),
		houseRule(failing)}, Options{FactReach: true, FactReachPresence: standing_presence.ReadPackage})
	if runError != nil {
		test.Fatalf("running with the capture: %v", runError)
	}
	if result.FactReach == nil {
		test.Fatal("the run with the capture captured nothing")
	}
	reach := capturedOf(test, result.FactReach, analyzed, "example.com/reach/reach")
	_, isHeld := reach.Pushed[deprecated.Analyzer.Name]
	if failed, isFailedHeld := reach.Pushed[failing.Name]; !isHeld || isFailedHeld {
		test.Errorf("reach holds the deprecation facts %t and the failed analyzer's %v, want the first alone", isHeld,
			failed)
	}
}

// The run reads presence's bodies of the packages it was given alone (#xeafx79, the reader's S4): user, given, imports
// lower, which the run analyzes for its facts only, and whose side no build is ever handed, so lower's bodies, which
// start a goroutine, are never read.
func TestTheRunReadsTheBodiesOfTheGivenPackagesAlone(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeFiles(test, root, map[string]string{
		"go.mod": "module example.com/given\n\ngo 1.24\n",
		"lower/lower.go": "// Package lower is a dependency of user.\npackage lower\n\n// Spin starts spun.\n" +
			"func Spin() { go spun() }\n\nfunc spun() {}\n",
		"user/user.go": "// Package user is the package the run is given.\npackage user\n\n" +
			"import \"example.com/given/lower\"\n\n// Use spins lower.\nfunc Use() { lower.Spin() }\n",
	})
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the given workspace: %v %+v", loadError, loaded)
	}
	var given []*packages.Package
	for _, loadedPackage := range loaded.Packages {
		if loadedPackage.PkgPath == "example.com/given/user" {
			given = append(given, loadedPackage)
		}
	}
	if len(given) != 1 {
		test.Fatalf("the workspace loaded %d packages of user, want one", len(given))
	}
	result, runError := Run(given, reachRules,
		Options{FactReach: true, FactReachPresence: standing_presence.ReadPackage})
	if runError != nil || result.FactReach == nil {
		test.Fatalf("running with the switch: %v", runError)
	}
	if read := result.FactReach.BodiesRead; !slices.Equal(read, []string{given[0].ID}) {
		test.Errorf("the run read the bodies of %v, want %s's alone", read, given[0].ID)
	}
}

// A package whose bodies presence never read is captured unread, end to end through the capture (#xeafx79, round 2's
// S4): with reach's bodies read and floored's not, reach is read, its bodies' record carried, and floored is unread,
// which its side counts unplaced, so every build lints it whole.
func TestAPackageWhoseBodiesWereNeverReadIsCapturedUnread(test *testing.T) {
	test.Parallel()
	analyzed := loadReach(test)
	options := DispatchOptions()
	resolver := fact_resolver.New(nil)
	options.Resolver = resolver
	if _, analyzeError := analyzeWrapped([]*analysis.Analyzer{reachNilnessAnalyzer}, analyzed,
		options); analyzeError != nil {
		test.Fatalf("analyzing the reach workspace: %v", analyzeError)
	}
	byPath := map[string]*packages.Package{}
	for _, analyzedPackage := range analyzed {
		byPath[analyzedPackage.PkgPath] = analyzedPackage
	}
	reach, floored := byPath["example.com/reach/reach"], byPath["example.com/reach/floored"]
	if reach == nil || floored == nil {
		test.Fatalf("the load holds %v, want reach and floored", byPath)
	}
	presence := &factReachPresence{read: map[string]map[fact_reach.Place]fact_reach.BodyRecord{reach.ID: {}},
		markers: map[string][]string{}}
	capture := captureFactReach(resolver, analyzed, nil, presence, BodyRecorder{})
	read, unread := capture.Packages[reach.ID], capture.Packages[floored.ID]
	if read == nil || unread == nil {
		test.Fatalf("the capture holds reach %v and floored %v, want both", read, unread)
	}
	if !read.PresenceRead || unread.PresenceRead {
		test.Errorf("reach is read %t and floored %t, want reach read and floored unread", read.PresenceRead,
			unread.PresenceRead)
	}
}
