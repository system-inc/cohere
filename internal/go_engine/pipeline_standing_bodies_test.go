package go_engine

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// standingBodiesFixture is a module of one package whose PlantedKept body windows's difference never reaches and whose
// PlantedReached body it does, through platformName, which each build declares in a file of its own.
var standingBodiesFixture = filepath.Join("testdata", "standing-bodies")

// standingProbeName is the standing probe's rule name.
const standingProbeName = rule.Namespace + "correctness-probe-standing"

// heardLiterals counts the planted literals the standing probe heard over a run, every build's: by their source, and
// in each file a pass parsed, which two builds' passes never share.
type heardLiterals struct {
	lock   sync.Mutex
	heard  map[string]int
	inFile map[*token.File]int
}

// standingProbe is a house rule on a syntax dispatcher of its own, so tests running at once never share its counts,
// whose listener reports each string literal holding "planted", which it hears only in a body that does not stand,
// and each function whose name starts with Planted, at its name, which it hears even where the body stands, saying
// how many planted literals it heard in the file before it. That count is the rule's own reading of the file, so in a
// build that leaves a body standing, a function after it reads fewer than the host's did, and only the host's finding
// there is right: it is the rule's own finding that the swap drops (rule.OutsideStanding).
func standingProbe(heard *heardLiterals) rule.Rule {
	listener := &rule.Listener{
		Nodes: []ast.Node{(*ast.FuncDecl)(nil), (*ast.BasicLit)(nil)},
		Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
			tokenFile := context.FileSet.File(node.Pos())
			heard.lock.Lock()
			defer heard.lock.Unlock()
			switch typed := node.(type) {
			case *ast.FuncDecl:
				if strings.HasPrefix(typed.Name.Name, "Planted") {
					context.Report(analysis.Diagnostic{Pos: typed.Name.Pos(), End: typed.Name.End(),
						Category: "plantedFunction", Message: fmt.Sprintf("a planted function, after %d planted "+
							"literals in its file", heard.inFile[tokenFile])})
				}
			case *ast.BasicLit:
				if typed.Kind != token.STRING || !strings.Contains(typed.Value, "planted") {
					return
				}
				heard.heard[typed.Value]++
				heard.inFile[tokenFile]++
				context.Report(analysis.Diagnostic{Pos: typed.Pos(), End: typed.End(), Category: "plantedLiteral",
					Message: "a planted literal"})
			}
		},
	}
	analyzer := dispatch.NewSyntax().RuleAnalyzer(standingProbeName, "probe_standing",
		"reports each planted function and literal", listener)
	return rule.Rule{Name: standingProbeName, Origin: rule.OriginHouse, Analyzer: analyzer, Listener: listener}
}

// representationProbeName is the representation probe's rule name.
const representationProbeName = rule.Namespace + "correctness-probe-representation"

// heldRepresentations are the packages in the representation each build's walk over it handed the representation
// probe a function of, by the base name of the file platformName is declared in, which each build declares in a file
// of its own: standing_other.go for the host's, standing_windows.go for windows's.
type heldRepresentations struct {
	lock     sync.Mutex
	packages map[string]*intermediate_representation.Package
}

// representationProbe is a house rule on a dispatcher over the representation of its own, which reports nothing and
// keeps the package of platformName, the function each build's walk hands it (heldRepresentations). That package is the
// one the build's own build_intermediate_representation result holds, which every dispatcher over the representation
// on the package walks, so what of it is built once the run is over is what the build built. Declared PerUnit, it is
// confined as the registry's rules on the representation are, so it builds no more of the package than they do.
func representationProbe(held *heldRepresentations) rule.Rule {
	listener := &rule.Listener{IntermediateRepresentation: &rule.IntermediateRepresentationListener{
		Function: func(context *rule.Context, function *intermediate_representation.Function) {
			if function.Name() != "platformName" {
				return
			}
			held.lock.Lock()
			defer held.lock.Unlock()
			held.packages[filepath.Base(context.FileSet.Position(function.Pos()).Filename)] = function.Package
		},
	}}
	analyzer := dispatch.NewIntermediateRepresentation().RuleAnalyzer(representationProbeName,
		"probe_representation", "keeps the package each build's walk over the representation hands it", listener)
	return rule.Rule{Name: representationProbeName, Origin: rule.OriginHouse, Analyzer: analyzer, Listener: listener}
}

// runStanding runs the engine over a copy of the standing-bodies fixture with the registry's rules, the standing probe
// and the representation probe, both declared PerUnit, under the body reach census with `--no-cache --no-fix
// --build-dependence`, every build checked as a plain run's are, with the standing bodies' switch as switched, keeping
// each build's package in the representation in held, and returns the pipeline, its stream, normalized, the literals
// the probe heard over every build, and the copy's root.
func runStanding(
	test *testing.T,
	switched bool,
	held *heldRepresentations,
) (*pipeline, string, map[string]int, string) {
	test.Helper()
	heard := &heardLiterals{heard: map[string]int{}, inFile: map[*token.File]int{}}
	root := copyFixture(test, standingBodiesFixture)
	engine, stream, _ := runOverRegistered(test, root, exitFindings,
		append(rules.All(), standingProbe(heard), representationProbe(held)),
		func(engine *pipeline) {
			engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
			engine.standingBodies = switched
			engine.plantedInputs = map[string]rules.Declared{standingProbeName: {Inputs: rules.InputsPerUnit},
				representationProbeName: {Inputs: rules.InputsPerUnit}}
		}, "--no-cache", "--no-fix", "--build-dependence")
	heard.lock.Lock()
	defer heard.lock.Unlock()
	return engine, normalize(stream, root), maps.Clone(heard.heard), root
}

// rawFindings are run's own findings, every rule's, before the merge, sorted, with root, the run's copy of the fixture,
// written as /project.
func rawFindings(run buildRun, root string) []string {
	found := make([]string, 0, len(run.result.Findings))
	for _, finding := range run.result.Findings {
		found = append(found, strings.ReplaceAll(fmt.Sprintf("%+v", finding), root, "/project"))
	}
	slices.Sort(found)
	return found
}

// builtIn is whether each declared function of the package held under filename has blocks, by its name, and
// none where no build's walk handed the probe that file's platformName.
func builtIn(held *heldRepresentations, filename string) map[string]bool {
	held.lock.Lock()
	defer held.lock.Unlock()
	built := map[string]bool{}
	if irPackage := held.packages[filename]; irPackage != nil {
		for _, function := range irPackage.Functions {
			built[function.Name()] = function.Blocks != nil
		}
	}
	return built
}

// windowsOutcome is windows's own outcome in engine's run over root, after its own //cohere:ignore comments
// (outcomeOf), as sorted lines with root written /project: each finding kept, each one a directive withheld, and each
// directive finding, an unused directive's among them. ran are every rule the run's registry holds.
func windowsOutcome(engine *pipeline, root string) []string {
	ran := []string{standingProbeName, representationProbeName}
	for _, registered := range rules.All() {
		ran = append(ran, registered.Name)
	}
	outcome := engine.outcomeOf(engine.buildRuns[1], ran)
	lines := []string{}
	for _, each := range outcome.kept {
		lines = append(lines, fmt.Sprintf("kept %+v", each))
	}
	for _, each := range outcome.withheld {
		lines = append(lines, fmt.Sprintf("withheld %+v", each))
	}
	for _, each := range outcome.directiveFindings {
		lines = append(lines, fmt.Sprintf("directive %+v", each))
	}
	for index := range lines {
		lines[index] = strings.ReplaceAll(lines[index], root, "/project")
	}
	slices.Sort(lines)
	return lines
}

// The standing bodies' switch leaves a body standing only on the census path: on, in a run judging its builds'
// dependence that asked for the body reach census or the body differential, and in no other run, the switch's own
// default, off, included.
func TestTheStandingBodiesSwitchLeavesBodiesStandingOnTheCensusPathAlone(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		name     string
		engine   *pipeline
		standing bool
	}{
		{"off by default, on the census path", &pipeline{bodyReachCensus: "reach.txt",
			dependence: &hostDependence{}}, false},
		{"on, under the census", &pipeline{standingBodies: true, bodyReachCensus: "reach.txt",
			dependence: &hostDependence{}}, true},
		{"on, under the body differential", &pipeline{standingBodies: true, bodyDifferential: true,
			dependence: &hostDependence{}}, true},
		{"on, with neither census nor differential", &pipeline{standingBodies: true,
			dependence: &hostDependence{}}, false},
		{"on, under the census, judging no build's dependence", &pipeline{standingBodies: true,
			bodyReachCensus: "reach.txt"}, false},
	} {
		if standing := each.engine.leavesBodiesStanding(); standing != each.standing {
			test.Errorf("%s: the run leaves bodies standing %t, want %t", each.name, standing, each.standing)
		}
	}
}

// A rule is confined for the standing bodies when it is dispatched and the inputs table declares it PerUnit or
// FileSyntax, and no other is: over the registry, every confined rule has a listener and one of the two inputs, a
// listener of a package-wide rule and a PerUnit rule that walks its package itself are not confined, and a rule a test
// planted is confined by the inputs the test declares for it.
func TestARuleIsConfinedWhenDispatchedAndDeclaredPerUnitOrFileSyntax(test *testing.T) {
	test.Parallel()

	declared, declaredError := rules.DeclaredInputs()
	if declaredError != nil {
		test.Fatal(declaredError)
	}
	planted := plantedProbe(&enteredFiles{entered: map[string]int{}})
	registered := append(rules.All(), planted)
	engine := &pipeline{plantedInputs: map[string]rules.Declared{plantedProbeName: {Inputs: rules.InputsPerUnit}}}
	confined := engine.standingConfinedRules(registered)

	listenerComposes, walkerPerUnit := "", ""
	for _, each := range registered {
		inputs := declared[each.Name].Inputs
		perUnitOrFileSyntax := inputs == rules.InputsPerUnit || inputs == rules.InputsFileSyntax
		if confined[each.Name] != (each.Listener != nil && perUnitOrFileSyntax) && each.Name != plantedProbeName {
			test.Errorf("%s is confined %t, with a listener %t and declared %s", each.Name, confined[each.Name],
				each.Listener != nil, inputs)
		}
		switch {
		case each.Listener != nil && inputs == rules.InputsComposes && listenerComposes == "":
			listenerComposes = each.Name
		case each.Listener == nil && inputs == rules.InputsPerUnit && walkerPerUnit == "":
			walkerPerUnit = each.Name
		}
	}
	if listenerComposes == "" || walkerPerUnit == "" || confined[listenerComposes] || confined[walkerPerUnit] {
		test.Errorf("want a dispatched package-wide rule and a PerUnit rule that walks itself, neither confined, got "+
			"%q and %q", listenerComposes, walkerPerUnit)
	}
	if !confined[plantedProbeName] {
		test.Errorf("the planted probe, declared PerUnit by the test, is not confined")
	}
	if unplanted := (&pipeline{}).standingConfinedRules([]rule.Rule{planted}); unplanted[plantedProbeName] {
		test.Errorf("the planted probe is confined with no inputs declared for it")
	}
}

// The findings differential of the standing bodies: two whole runs over the standing-bodies fixture under the body
// reach census, the switch off and on, find the same, the stream's findings and windows's own before the merge alike,
// and on, something stood. With the switch on, windows's build leaves PlantedKept's and PlantedKeptAfter's bodies to
// the host's findings: the probe, confined, never hears PlantedKept's literal, and so reads PlantedKeptAfter as after
// one literal where the host read two, and in both declarations reports the host's findings in place of its own,
// counted as two standing declarations with three findings, which windows's build record says. PlantedReached, which
// the difference reaches, is heard and judged in both runs. Off, nothing is confined, every body is heard, and the
// record writes no standing count. A //cohere:ignore with a reason inside PlantedKept's standing body acts on windows's
// build exactly as on a body it heard: the literal's finding, the host's taken, is withheld, the directive is never
// unused, and windows's findings, withheld ones and directive findings after its own suppression are the same either
// way.
func TestStandingBodiesFindWhatEveryBodyHeardFinds(test *testing.T) {
	test.Parallel()

	offHeld := &heldRepresentations{packages: map[string]*intermediate_representation.Package{}}
	onHeld := &heldRepresentations{packages: map[string]*intermediate_representation.Package{}}
	off, offStream, offHeard, offRoot := runStanding(test, false, offHeld)
	on, onStream, onHeard, onRoot := runStanding(test, true, onHeld)

	if len(off.buildRuns) != 2 || len(on.buildRuns) != 2 {
		test.Fatalf("want the host's build and windows's in both runs, got %d and %d runs", len(off.buildRuns),
			len(on.buildRuns))
	}
	if !slices.Equal(findingLines(onStream), findingLines(offStream)) {
		test.Errorf("the findings with the switch on are\n%q\nwant those with it off\n%q", findingLines(onStream),
			findingLines(offStream))
	}
	onWindows, offWindows := rawFindings(on.buildRuns[1], onRoot), rawFindings(off.buildRuns[1], offRoot)
	if !slices.Equal(onWindows, offWindows) {
		test.Errorf("windows's own findings with the switch on are\n%s\nwant those with it off\n%s",
			strings.Join(onWindows, "\n"), strings.Join(offWindows, "\n"))
	}
	onOutcome, offOutcome := windowsOutcome(on, onRoot), windowsOutcome(off, offRoot)
	if !slices.Equal(onOutcome, offOutcome) {
		test.Errorf("windows's outcome after its suppression with the switch on is\n%s\nwant that with it off\n%s",
			strings.Join(onOutcome, "\n"), strings.Join(offOutcome, "\n"))
	}
	withheldLiteral := slices.ContainsFunc(onOutcome, func(line string) bool {
		return strings.HasPrefix(line, "withheld ") && strings.Contains(line, "a planted literal")
	})
	unusedDirective := slices.ContainsFunc(onOutcome, func(line string) bool {
		return strings.HasPrefix(line, "directive ") && strings.Contains(line, "MessageId:unused")
	})
	if !withheldLiteral || unusedDirective {
		test.Errorf("with the switch on windows withheld PlantedKept's literal: %t, and read a directive unused: %t, "+
			"want the literal withheld and no directive unused, in\n%s", withheldLiteral, unusedDirective,
			strings.Join(onOutcome, "\n"))
	}
	if on.buildRuns[1].standingDeclarations != 2 || on.buildRuns[1].standingFindings != 3 {
		test.Errorf("windows's build took the host's findings in %d standing declarations, %d findings, want 2 and 3",
			on.buildRuns[1].standingDeclarations, on.buildRuns[1].standingFindings)
	}
	if !on.standingConfined[standingProbeName] || off.standingConfined != nil ||
		off.buildRuns[1].standingDeclarations != 0 {
		test.Errorf("the probe is confined %t with the switch on, and with it off the run confined %v and windows "+
			"took %d standing declarations, want the probe confined on and nothing off",
			on.standingConfined[standingProbeName], off.standingConfined, off.buildRuns[1].standingDeclarations)
	}
	// The record says how much stood, and with the switch off writes neither count.
	onRecord, _ := windowsRecord(test, on, onStream)
	if onRecord.StandingDeclarations != 2 || onRecord.StandingFindings != 3 {
		test.Errorf("windows's build record counts %d standing declarations and %d findings, want 2 and 3",
			onRecord.StandingDeclarations, onRecord.StandingFindings)
	}
	if _, offWritten := windowsRecord(test, off, offStream); strings.Contains(offWritten, `"standing`) {
		test.Errorf("with the switch off windows's build record writes a standing count: %s", offWritten)
	}
	// The saving the standing bodies buy with the registry's rules: in windows's package in the representation, as its
	// build held it, PlantedKept, which stands, was never built with the switch on, and was with it off, while
	// PlantedReached, which the difference reaches, was built both ways.
	onBuilt, offBuilt := builtIn(onHeld, "standing_windows.go"), builtIn(offHeld, "standing_windows.go")
	if onBuilt["PlantedKept"] || !offBuilt["PlantedKept"] || !onBuilt["PlantedReached"] || !offBuilt["PlantedReached"] {
		test.Errorf("in windows's representation PlantedKept was built %t with the switch on and %t off, and "+
			"PlantedReached %t and %t, want PlantedKept built off alone and PlantedReached both ways",
			onBuilt["PlantedKept"], offBuilt["PlantedKept"], onBuilt["PlantedReached"], offBuilt["PlantedReached"])
	}
	kept, reached := `"planted kept"`, `" planted reached"`
	if offHeard[kept] != 2 || offHeard[reached] != 2 || onHeard[kept] != 1 || onHeard[reached] != 2 {
		test.Errorf("the probe heard %v with the switch off and %v on, want both literals by both builds off, and on "+
			"PlantedKept's by the host's alone", offHeard, onHeard)
	}
}

// standingGate is one module of testdata/standing-gates (#zvwfe26), each a rule's package-wide gate: the other build's
// own file, windows's in every module but the two of abbreviated-closure, whose other build is the tag audit_b's, or
// the host's own file opens or closes the gate that judges a body in shared.go, or in example_test.go, which both
// builds compile and whose declaration, stood, is one the other build's difference never reaches. stood names it as
// the census does. The finding the gate decides there is named by its rule, its line in file, a fragment of its
// message, and fix, where not empty, a fragment one of its suggestions holds in its message or an edit's text; finder
// is the build that finds it, "host" or "build" for the other, the second finding nothing of the rule on that line
// that matches, or "neither", where the gate opens in both builds. standing is how many declarations the other build
// took the host's findings in with the switch on.
//
// imports is whether what opens or closes the gate is an import that one build's file alone holds in the gated
// declaration's own package. The reach reaches such a package whole, Imports (#bed0mnk), so stood cannot stand there:
// the other build judges it itself, by that reason, whatever the inputs table declares for the rule, and the module
// holds a quiet package beside it whose Stable stands in its place, so its run with the switch on still leaves a body
// to the host's findings.
//
// What such a row no longer shows by a standing body: the other build judges the reached declaration itself, so its
// findings read the same whatever the inputs table declares for the rule, and a rule flipped to PerUnit there would
// pass every one of them. So the row holds the table's line directly: confined is whether its rule is in the run's
// confined set (pipeline.standingConfined), as a dispatched rule declared PerUnit is and one declared Composes is not.
// SA5008's rows are confined, the imports reason being what carries its gate (#dp67trx). That line is what the imports
// reason will be rebuilt from, the rules' own table (version (b), #bed0mnk), and a standing body shows it again then.
// The finding is still held to the build that opens the gate, with the switch on as off.
type standingGate struct {
	module   string
	stood    string
	file     string
	rule     string
	line     int
	fragment string
	fix      string
	finder   string
	standing int
	imports  bool
	confined bool
}

// standingGates are the modules, two for each gate the table holds Composes for, or held for until a reason carried
// it: the thing that opens or closes it in the other build's file alone (-win), and in the host's alone (-host). In
// each pair one module's finding is the host's alone, which the other build would invent were the body left to the
// host for the rule, and the other's is the other build's alone, which it would lose.
//   - SA4029 by a method of sort used through an alias (sort-alias-*), from a shared file that imports no sort, and by
//     a bare import of sort (sort-import-*): either puts sort's slice types in the package's type index.
//   - SA5008 by an import of go-flags (import-goflags-*), a stub by the module's replace, after which a repeated
//     `default` key in a struct tag is accepted anywhere in the package. SA5008 is PerUnit, its gate carried by the
//     imports reason, and the gate reads the import's path, not its literal's text (#dp67trx): an import spelled with
//     an escape, `go\x2dflags`, opens it in its own build alone (import-goflags-escaped-win), and one build's escaped
//     import beside the other's plain one, which leave the imports equal and Target standing, opens it in both, so
//     neither finds it and the other build takes no finding of the host's that its own gate would refuse
//     (import-goflags-escaped-both).
//   - tests by the package's scope, `ExampleFoo` referring to an unknown identifier unless some file declares Foo
//     (example-*), by its imports, `ExampleBuffer` unless some file imports bytes (example-import-*), and by the
//     members of the type an example names, `ExampleFoo_Bar` referring to an unknown field or method unless some file
//     declares a method Bar of Foo, a type both builds declare (example-member-*).
//   - consistency-no-abbreviated-identifier by the package's scope, `callerContext` being the name it suggests for a
//     context parameter only while no file declares it (abbreviated-scope-*), by a use of sort, after which it leaves
//     a method named Len alone (abbreviated-sort-*), and by the package's whole import closure, whose packages' names
//     it keeps free: the package imports dep alone in both builds, and dep imports context under the tag alone
//     (abbreviated-closure) or in every build but the tag's (abbreviated-closure-host), so `context` is the name it
//     suggests for a local ctx only in the build whose dep does not import it. Of each module's two bodies Target
//     alone stands: dep's own imports differ, so dep's Value is reached with its package.
//   - S1018, S1025 and stringintconv by a universe name the package may declare in any file: S1018 offers copy() for a
//     sliding loop only while copy is the builtin (copy-scope-*), S1025 a conversion only while string is
//     (sprintf-scope-*), and stringintconv imports fmt under its own name only while no file declares fmt
//     (stringint-scope-*).
//   - ST1016 by every file's methods of one type, a method naming its receiver otherwise in one build's file alone
//     (receiver-names-*). It is the one gate here that hears no body: Target, which stands, holds a local Thing, a
//     closure and a function value whose named parameters are one, each shaped like a receiver, and the pinned
//     message counts none of them, so a rule that read bodies would count them and lose the pin (#dp67trx,
//     TestReceiverNamesHearNoBody). Target names nothing of the package's Thing, whose method set differs and would
//     reach it by Named, and no method, whose printf wrapper fact the other build never looks up and which so takes
//     it out of the standing set.
//
// Four of the pairs flip their gate by an import in a file of the gated declaration's own package, a bare one or one
// a use takes: sort-import-*, import-goflags-* but import-goflags-escaped-both, example-import-* and
// abbreviated-sort-* (standingGate.imports). There
// the reach leaves nothing of the package standing, and what stands is the quiet package's one body.
var standingGates = []standingGate{
	{module: "sort-alias-win", stood: "target", file: "shared.go", rule: "SA4029", line: 5,
		fragment: "sort.IntSlice is a type, not a function", finder: "build", standing: 2},
	{module: "sort-alias-host", stood: "target", file: "shared.go", rule: "SA4029", line: 5,
		fragment: "sort.IntSlice is a type, not a function", finder: "host", standing: 2},
	{module: "sort-import-win", stood: "target", file: "shared.go", rule: "SA4029", line: 5,
		fragment: "sort.IntSlice is a type, not a function", finder: "build", standing: 1, imports: true},
	{module: "sort-import-host", stood: "target", file: "shared.go", rule: "SA4029", line: 5,
		fragment: "sort.IntSlice is a type, not a function", finder: "host", standing: 1, imports: true},
	{module: "import-goflags-win", stood: "Target", file: "shared.go", rule: "SA5008", line: 5,
		fragment: `duplicate struct tag "default"`, finder: "host", standing: 1, imports: true, confined: true},
	{module: "import-goflags-host", stood: "Target", file: "shared.go", rule: "SA5008", line: 5,
		fragment: `duplicate struct tag "default"`, finder: "build", standing: 1, imports: true, confined: true},
	{module: "import-goflags-escaped-win", stood: "Target", file: "shared.go", rule: "SA5008", line: 5,
		fragment: `duplicate struct tag "default"`, finder: "host", standing: 1, imports: true, confined: true},
	{module: "import-goflags-escaped-both", stood: "Target", file: "shared.go", rule: "SA5008", line: 5,
		fragment: `duplicate struct tag "default"`, finder: "neither", standing: 1},
	{module: "example-win", stood: "ExampleFoo", file: "example_test.go", rule: "tests", line: 3,
		fragment: "ExampleFoo refers to unknown identifier: Foo", finder: "host", standing: 2},
	{module: "example-host", stood: "ExampleFoo", file: "example_test.go", rule: "tests", line: 3,
		fragment: "ExampleFoo refers to unknown identifier: Foo", finder: "build", standing: 2},
	{module: "example-import-win", stood: "ExampleBuffer", file: "example_test.go", rule: "tests", line: 3,
		fragment: "ExampleBuffer refers to unknown identifier: Buffer", finder: "host", standing: 1, imports: true},
	{module: "example-import-host", stood: "ExampleBuffer", file: "example_test.go", rule: "tests", line: 3,
		fragment: "ExampleBuffer refers to unknown identifier: Buffer", finder: "build", standing: 1, imports: true},
	{module: "example-member-win", stood: "ExampleFoo_Bar", file: "example_test.go", rule: "tests", line: 3,
		fragment: "ExampleFoo_Bar refers to unknown field or method: Foo.Bar", finder: "host", standing: 2},
	{module: "example-member-host", stood: "ExampleFoo_Bar", file: "example_test.go", rule: "tests", line: 3,
		fragment: "ExampleFoo_Bar refers to unknown field or method: Foo.Bar", finder: "build", standing: 2},
	{module: "abbreviated-scope-win", stood: "target", file: "shared.go", rule: "consistency-no-abbreviated-identifier",
		line: 5, fragment: `Use "callerContext"`, finder: "host", standing: 1},
	{module: "abbreviated-scope-host", stood: "target", file: "shared.go",
		rule: "consistency-no-abbreviated-identifier", line: 5, fragment: `Use "callerContext"`, finder: "build",
		standing: 1},
	{module: "abbreviated-sort-win", stood: "Items.Len", file: "shared.go",
		rule: "consistency-no-abbreviated-identifier", line: 5, fragment: `Identifier "Len"`, finder: "host",
		standing: 1, imports: true},
	{module: "abbreviated-sort-host", stood: "Items.Len", file: "shared.go",
		rule: "consistency-no-abbreviated-identifier", line: 5, fragment: `Identifier "Len"`, finder: "build",
		standing: 1, imports: true},
	{module: "abbreviated-closure", stood: "Target", file: "shared.go",
		rule: "consistency-no-abbreviated-identifier", line: 9, fragment: `Use "context"`, finder: "host", standing: 1},
	{module: "abbreviated-closure-host", stood: "Target", file: "shared.go",
		rule: "consistency-no-abbreviated-identifier", line: 9, fragment: `Use "context"`, finder: "build",
		standing: 1},
	{module: "copy-scope-win", stood: "target", file: "shared.go", rule: "S1018", line: 4,
		fragment: "should use copy() instead of loop", fix: "Use copy() instead of loop", finder: "host", standing: 1},
	{module: "copy-scope-host", stood: "target", file: "shared.go", rule: "S1018", line: 4,
		fragment: "should use copy() instead of loop", fix: "Use copy() instead of loop", finder: "build", standing: 1},
	{module: "sprintf-scope-win", stood: "target", file: "shared.go", rule: "S1025", line: 8,
		fragment: "should use a simple conversion", fix: "Replace with conversion to string", finder: "host",
		standing: 1},
	{module: "sprintf-scope-host", stood: "target", file: "shared.go", rule: "S1025", line: 8,
		fragment: "should use a simple conversion", fix: "Replace with conversion to string", finder: "build",
		standing: 1},
	{module: "stringint-scope-win", stood: "target", file: "shared.go", rule: "stringintconv", line: 4,
		fragment: "conversion from int to string", fix: "fmt.Sprint", finder: "host", standing: 1},
	{module: "stringint-scope-host", stood: "target", file: "shared.go", rule: "stringintconv", line: 4,
		fragment: "conversion from int to string", fix: "fmt.Sprint", finder: "build", standing: 1},
	{module: "receiver-names-win", stood: "Target", file: "shared.go", rule: "ST1016", line: 5,
		fragment: `same receiver name (seen 1x "thing", 2x "t")`, finder: "build", standing: 1},
	{module: "receiver-names-host", stood: "Target", file: "shared.go", rule: "ST1016", line: 5,
		fragment: `same receiver name (seen 1x "thing", 2x "t")`, finder: "host", standing: 1},
}

// runStandingGate runs the engine over a copy of the standing gate module named with the registry's rules, under the
// body reach census with `--no-cache --no-fix --build-dependence`, with the standing bodies' switch as switched, and
// returns the pipeline, its stream, normalized, and the copy's root.
func runStandingGate(test *testing.T, module string, switched bool) (*pipeline, string, string) {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", "standing-gates", module))
	engine, stream, _ := runOverRegistered(test, root, exitFindings, rules.All(), func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
		engine.standingBodies = switched
	}, "--no-cache", "--no-fix", "--build-dependence")
	return engine, normalize(stream, root), root
}

// findsGate is whether run's own findings, before the merge, hold gate's: its rule, at its line in its file, with its
// fragment in the message, and its fix, where it names one, in a suggestion's message or an edit's text.
func findsGate(run buildRun, gate standingGate) bool {
	return slices.ContainsFunc(run.result.Findings, func(finding check.Finding) bool {
		return finding.Rule == rule.Namespace+gate.rule && filepath.Base(finding.File) == gate.file &&
			finding.Line == gate.line && strings.Contains(finding.Message, gate.fragment) &&
			(gate.fix == "" || slices.ContainsFunc(finding.Suggestions, func(suggestion check.Suggestion) bool {
				return strings.Contains(suggestion.Message, gate.fix) ||
					slices.ContainsFunc(suggestion.Edits, func(edit check.Edit) bool {
						return strings.Contains(edit.Text, gate.fix)
					})
			}))
	})
}

// stoodIn is whether the declaration named stood, in file, stands in run once the facts settled: the body reach left
// it unreached, and no differing fact took it out, in any package of the build, a test variant's among them.
func stoodIn(run buildRun, stood string, file string) bool {
	for _, body := range run.bodyReach.bodies {
		if body.name != stood || filepath.Base(body.file) != file || body.reason != "" {
			continue
		}
		place := fact_reach.Place{File: body.file, Offset: body.offset}
		for _, settled := range run.result.FactReachSettled {
			packageSettled, isResult := settled.(*standing_facts.Result)
			if isResult && packageSettled.Settlement != nil && packageSettled.Settlement.Standing[place] {
				return true
			}
		}
	}
	return false
}

// reachedByImports is whether run's census holds the declaration named stood, in file, and reaches it by its package's
// imports differing in every package of the build that holds it.
func reachedByImports(run buildRun, stood string, file string) bool {
	held := false
	for _, body := range run.bodyReach.bodies {
		if body.name != stood || filepath.Base(body.file) != file {
			continue
		}
		if body.reason != reachedImports {
			return false
		}
		held = true
	}
	return held
}

// A body left standing keeps no package-wide gate that another file of its build flips (#zvwfe26): over each standing
// gate module, the gated declaration stands in the other build with the switch on, the gate's finding is in the build
// that opens it and not in the other, with the switch on as off, and the other build's own findings, and the
// stream's, with the switch on are those with it off. A rule whose finding in a body hangs on such a gate is Composes
// in the inputs table, so it is never confined, and the other build judges the standing body itself for it, where the
// host's findings there would be wrong. Where an import in the declaration's own package flips the gate, the reach
// has reached the package whole already (#bed0mnk), so the declaration does not stand, the other build judges it
// whatever the table declares, the module's quiet package is what stands, and the rule is held out of the run's
// confined set in the standing body's place (standingGate.imports).
func TestAStandingBodyKeepsNoGateAnotherFileOfItsBuildFlips(test *testing.T) {
	test.Parallel()

	for _, gate := range standingGates {
		test.Run(gate.module, func(test *testing.T) {
			test.Parallel()
			off, offStream, offRoot := runStandingGate(test, gate.module, false)
			on, onStream, onRoot := runStandingGate(test, gate.module, true)
			if len(off.buildRuns) != 2 || len(on.buildRuns) != 2 {
				test.Fatalf("want the host's build and the other in both runs, got %d and %d runs", len(off.buildRuns),
					len(on.buildRuns))
			}
			switch {
			case gate.imports:
				if !reachedByImports(on.buildRuns[1], gate.stood, gate.file) {
					test.Errorf("with the switch on %s is not reached by %s in the other build, its package's "+
						"imports differing", gate.stood, reachedImports)
				}
				if !stoodIn(on.buildRuns[1], "Stable", "quiet.go") {
					test.Error("with the switch on the quiet package's Stable did not stand in the other build, so " +
						"the module's run leaves nothing standing")
				}
				// Nothing above reads the inputs table for a reached declaration, so its line for the rule is held
				// here: a rule it declares Composes is never confined, and one it declares PerUnit is. A run that
				// confined nothing at all has no set to read the rule's absence from.
				if on.standingConfined == nil || on.standingConfined[rule.Namespace+gate.rule] != gate.confined {
					test.Errorf("with the switch on the run confines %s for the standing bodies %t, or has no "+
						"confined set, want %t, as the inputs table declares it", gate.rule,
						on.standingConfined[rule.Namespace+gate.rule], gate.confined)
				}
			case !stoodIn(on.buildRuns[1], gate.stood, gate.file):
				test.Errorf("with the switch on %s did not stand in the other build, so the module holds nothing of "+
					"the gate", gate.stood)
			}
			// The settled set is settled with the switch off too, so what stood in effect is the count of declarations
			// the other build took the host's findings in: the module's own with the switch on, none with it off.
			if on.buildRuns[1].standingDeclarations != gate.standing || off.buildRuns[1].standingDeclarations != 0 {
				test.Errorf("the other build took the host's findings in %d declarations with the switch on and %d "+
					"off, want %d and 0", on.buildRuns[1].standingDeclarations,
					off.buildRuns[1].standingDeclarations, gate.standing)
			}
			for _, each := range []struct {
				switched string
				engine   *pipeline
			}{{"off", off}, {"on", on}} {
				host, other := findsGate(each.engine.buildRuns[0], gate), findsGate(each.engine.buildRuns[1], gate)
				if host != (gate.finder == "host") || other != (gate.finder == "build") {
					test.Errorf("with the switch %s %s at %s:%d %q is found by the host %t and by the other build "+
						"%t, want it found by %s", each.switched, gate.rule, gate.file, gate.line, gate.fragment, host,
						other, gate.finder)
				}
			}
			onOther, offOther := rawFindings(on.buildRuns[1], onRoot), rawFindings(off.buildRuns[1], offRoot)
			if !slices.Equal(onOther, offOther) {
				test.Errorf("the other build's own findings with the switch on are\n%s\nwant those with it off\n%s",
					strings.Join(onOther, "\n"), strings.Join(offOther, "\n"))
			}
			if !slices.Equal(findingLines(onStream), findingLines(offStream)) {
				test.Errorf("the findings with the switch on are\n%q\nwant those with it off\n%q",
					findingLines(onStream), findingLines(offStream))
			}
		})
	}
}

// receiverNameLines are ST1016's finding lines of stream, sorted.
func receiverNameLines(stream string) []string {
	return slices.DeleteFunc(findingLines(stream), func(line string) bool {
		return !strings.Contains(line, `"`+rule.Namespace+`ST1016"`)
	})
}

// ST1016 hears no function body (#dp67trx): it reads each file's top-level type declarations, each type's method set
// and each receiver's name and file, every one a declaration, so a body stripped from a build's type check changes
// nothing it finds. Over the receiver-names modules, whose standing Target holds everything in a body that looks like
// a receiver, a plain run, one judging no build's dependence, finds the module's one finding, its message counting
// the methods' receivers alone, and the census path finds exactly that with the switch off and with it on
// (standingGates holds where and in which build). Nothing is stripped yet, so what holds a rule that reads bodies
// today is the message: one that counted Target's parameters finds another, in both builds.
func TestReceiverNamesHearNoBody(test *testing.T) {
	test.Parallel()

	for _, module := range []string{"receiver-names-win", "receiver-names-host"} {
		test.Run(module, func(test *testing.T) {
			test.Parallel()
			root := copyFixture(test, filepath.Join("testdata", "standing-gates", module))
			_, plainStream, _ := runOverRegistered(test, root, exitFindings, rules.All(), nil, "--no-cache",
				"--no-fix")
			plain := receiverNameLines(normalize(plainStream, root))
			_, offStream, _ := runStandingGate(test, module, false)
			_, onStream, _ := runStandingGate(test, module, true)
			if len(plain) != 1 || !strings.Contains(plain[0], `(seen 1x \"thing\", 2x \"t\")`) {
				test.Errorf("a plain run found %d of ST1016's findings, want the module's one, counting the methods' "+
					"receivers alone:\n%s", len(plain), strings.Join(plain, ""))
			}
			for _, each := range []struct {
				switched string
				stream   string
			}{{"off", offStream}, {"on", onStream}} {
				if found := receiverNameLines(each.stream); !slices.Equal(found, plain) {
					test.Errorf("ST1016's findings with the switch %s are\n%s\nwant a plain run's\n%s", each.switched,
						strings.Join(found, ""), strings.Join(plain, ""))
				}
			}
		})
	}
}
