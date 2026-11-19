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

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
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

// standingGates are the modules of testdata/standing-gates, each of a package-wide gate (#zvwfe26): in each, windows's
// own file flips or keeps the gate another file's function is judged by, and that function's body is one the
// difference never reaches. Four are the body differential's of SA4029's gate (the seat's h-shadow return): sort-win
// and sort-host flip whether a method of package sort is used, which opens SA4029, beside a shared file that imports
// sort itself; sort-alias-win and sort-alias-host flip it through an alias, from a shared file that imports no sort.
// import-goflags imports go-flags, a stub by its module's replace, in windows's file alone, which lets SA5008 accept a
// repeated `default` key in a struct tag anywhere in the package. The seat's import-unsafe, unusedwrite's gate, and
// import-cobra, SA1015's, wait on the reach reaching a package whose files import differently, on a branch of their
// own.
var standingGates = []string{"sort-win", "sort-host", "sort-alias-win", "sort-alias-host", "import-goflags"}

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

// A body left standing keeps no package-wide gate that another file of windows's build flips: over each standing gate
// module, windows's own findings, and the stream's, with the standing bodies' switch on are those with it off, and on,
// something stood. A rule whose finding in a body hangs on such a gate is Composes in the inputs table, so it is never
// confined, and windows judges the shared body itself, where the host's findings there would be wrong.
func TestAStandingBodyKeepsNoGateAnotherFileOfItsBuildFlips(test *testing.T) {
	test.Parallel()

	for _, module := range standingGates {
		test.Run(module, func(test *testing.T) {
			test.Parallel()
			off, offStream, offRoot := runStandingGate(test, module, false)
			on, onStream, onRoot := runStandingGate(test, module, true)
			if len(off.buildRuns) != 2 || len(on.buildRuns) != 2 {
				test.Fatalf("want the host's build and windows's in both runs, got %d and %d runs", len(off.buildRuns),
					len(on.buildRuns))
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
			if on.buildRuns[1].standingDeclarations == 0 {
				test.Errorf("with the switch on nothing of windows's build stood, so the comparison proves nothing")
			}
		})
	}
}
