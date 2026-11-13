package check

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"hash/fnv"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// The unit-isolation test (#h350vqr) holds every rule the side table declares rules.InputsPerUnit or
// rules.InputsFileSyntax to its declaration: over a package, a variant that replaces the bodies of a seeded random half
// of its function units by a terminating stub, the stub block 4 checks with, must give the same findings as the whole
// package inside every unit it kept. A rule whose finding in one unit reads another unit's body goes red, named with
// the unit.
//
// The variant sees the whole package's facts and answers, never ones recomputed from a stub, so the only difference a
// kept unit can see is another unit's syntax and types:
//   - every answer the fact resolver gives about the package's objects is the whole run's (fact_resolver.Held). A
//     kind judges its package's functions together, reading a same-package callee's verdict without asking, so the
//     answers are held for every function, kept ones too, which a kept caller then reads exactly as it did whole;
//   - ctrlflow's no-return, which it reads from each body in the package and never from a fact, is carried by the
//     stub's form: panic(0) for a function the whole run found never returns, and (panic)(0) for one that returns,
//     which go/types counts as terminating as it counts panic(0), and in which ctrlflow sees no static callee, so a
//     call may return;
//   - the only other facts an isolated rule's analyzers learn are staticcheck's deprecation facts, read from doc
//     comments, which a stub keeps. TestTheIsolatedRulesLearnOnlyFactsTheVariantKeeps holds that list closed.
//
// It is the guard, not the proof: soundness is the flip's census, every platform judged once shared and once whole,
// byte-identical on every tree. A green isolation test is never read as soundness.

// unitIsolationSeeds are the seeds each fixture package's variants stub by, one variant each.
var unitIsolationSeeds = []uint64{1, 2, 3}

// isolatedFactAnalyzers are the analyzers below an isolated rule that learn facts, each with why a variant's facts
// are the whole run's: ctrlflow's no-return is carried by the stub's form, and staticcheck's deprecation facts are read
// from doc comments, which a stub keeps.
var isolatedFactAnalyzers = map[string]bool{"ctrlflow": true, "fact_deprecated": true}

// isolatedRules are the registry's rules the test isolates, every rule the side table (rules.DeclaredInputs)
// declares PerUnit or FileSyntax.
func isolatedRules(test *testing.T) []rule.Rule {
	test.Helper()
	declared, declaredError := rules.DeclaredInputs()
	if declaredError != nil {
		test.Fatal(declaredError)
	}
	var isolated []rule.Rule
	for _, registered := range rules.All() {
		inputs := declared[registered.Name].Inputs
		if inputs == rules.InputsPerUnit || inputs == rules.InputsFileSyntax {
			isolated = append(isolated, registered)
		}
	}
	return isolated
}

// unitAt is a place in a file both runs parsed, by the file's name and the offset in it, which names the same unit or
// object in the whole package and in every variant, since a stub moves no byte.
type unitAt struct {
	file   string
	offset int
}

// positionAt is position's place, false for no position.
func positionAt(fileSet *token.FileSet, position token.Pos) (unitAt, bool) {
	tokenFile := fileSet.File(position)
	if !position.IsValid() || tokenFile == nil {
		return unitAt{}, false
	}
	return unitAt{file: tokenFile.Name(), offset: tokenFile.Offset(position)}, true
}

// functionUnit is one function declared with a body: its name as a failure says it, and where it starts and ends.
type functionUnit struct {
	name  string
	start unitAt
	end   int
}

// unitHolds is whether place lies inside unit.
func unitHolds(unit functionUnit, place unitAt) bool {
	return place.file == unit.start.file && place.offset >= unit.start.offset && place.offset < unit.end
}

// functionUnits are every function the package declares with a body, sorted by file and offset.
func functionUnits(analyzed *packages.Package) []functionUnit {
	var units []functionUnit
	for _, file := range analyzed.Syntax {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			start, _ := positionAt(analyzed.Fset, function.Pos())
			end, _ := positionAt(analyzed.Fset, function.End())
			name := function.Name.Name
			if function.Recv != nil && len(function.Recv.List) > 0 {
				name = types.ExprString(function.Recv.List[0].Type) + "." + name
			}
			units = append(units, functionUnit{name: name, start: start, end: end.offset})
		}
	}
	slices.SortFunc(units, func(left functionUnit, right functionUnit) int {
		return cmp.Or(cmp.Compare(left.start.file, right.start.file), cmp.Compare(left.start.offset,
			right.start.offset))
	})
	return units
}

// isolationFinding is one finding as both runs can compare it: the rule, where it starts and ends, and its message.
type isolationFinding struct {
	rule    string
	at      unitAt
	end     int
	message string
}

// findingLine is finding as one line, which two runs' findings are compared by.
func findingLine(finding isolationFinding) string {
	return fmt.Sprintf("%s %s:%d-%d %q", finding.rule, filepath.Base(finding.at.file), finding.at.offset,
		finding.end, finding.message)
}

// heldAnswerKey is one answer of the whole run, as a variant asks it: the package's ID, the kind and the object's
// place.
type heldAnswerKey struct {
	packageIdentifier string
	kind              string
	at                unitAt
}

// isolationRun is one run of the isolated rules over some packages: each package's findings, the rules that crashed
// on it, and the places of its functions ctrlflow found never return.
type isolationRun struct {
	findings map[*packages.Package][]isolationFinding
	crashed  map[*packages.Package]map[string]string
	noReturn map[unitAt]bool
}

// runIsolated runs the isolated rules over analyzed, with held's hand on the answers, none when held is nil, and is
// each package's findings,
// the rules that crashed on it, and the places of the functions ctrlflow found never return.
func runIsolated(isolated []rule.Rule, analyzed []*packages.Package, held fact_resolver.Held) (*isolationRun, error) {
	ruleOf := make(map[*analysis.Analyzer]rule.Rule, len(isolated))
	analyzers := make([]*analysis.Analyzer, 0, len(isolated))
	for _, registered := range isolated {
		ruleOf[registered.Analyzer] = registered
		analyzers = append(analyzers, registered.Analyzer)
	}
	if wrapError := wrapRuns(analyzers); wrapError != nil {
		return nil, wrapError
	}
	options := DispatchOptions()
	resolver := fact_resolver.New(nil)
	if held != nil {
		resolver.HoldAnswers(held)
	}
	options.Resolver = resolver
	graph, analyzeError := checker.Analyze(analyzers, analyzed, options)
	if analyzeError != nil {
		return nil, analyzeError
	}
	run := &isolationRun{findings: map[*packages.Package][]isolationFinding{},
		crashed: map[*packages.Package]map[string]string{}, noReturn: map[unitAt]bool{}}
	for action := range graph.All() {
		if action.Analyzer == ctrlflow.Analyzer && slices.Contains(analyzed, action.Package) {
			for _, fact := range action.AllObjectFacts() {
				if fact.Object.Pkg() != action.Package.Types {
					continue
				}
				if at, isPlaced := positionAt(action.Package.Fset, fact.Object.Pos()); isPlaced {
					run.noReturn[at] = true
				}
			}
		}
		registered, isRule := ruleOf[action.Analyzer]
		if !isRule || !action.IsRoot {
			continue
		}
		if action.Err != nil {
			if run.crashed[action.Package] == nil {
				run.crashed[action.Package] = map[string]string{}
			}
			run.crashed[action.Package][registered.Name] = action.Err.Error()
			continue
		}
		for _, diagnostic := range action.Diagnostics {
			at, isPlaced := positionAt(action.Package.Fset, diagnostic.Pos)
			if !isPlaced {
				continue
			}
			finding := isolationFinding{rule: registered.Name, at: at, end: at.offset, message: diagnostic.Message}
			if end, isEnded := positionAt(action.Package.Fset, diagnostic.End); isEnded && end.file == at.file {
				finding.end = end.offset
			}
			run.findings[action.Package] = append(run.findings[action.Package], finding)
		}
	}
	return run, nil
}

// stubCall is the terminating call a stubbed body holds, at position: panic(0), or (panic)(0) when returns, so
// ctrlflow, which finds no static callee in it, reads the stub as a body that may return, as the whole run read the
// function's own.
func stubCall(position token.Pos, returns bool) *ast.BlockStmt {
	var callee ast.Expr = &ast.Ident{NamePos: position, Name: "panic"}
	if returns {
		callee = &ast.ParenExpr{Lparen: position, X: callee, Rparen: position}
	}
	call := &ast.CallExpr{Fun: callee, Lparen: position, Rparen: position,
		Args: []ast.Expr{&ast.BasicLit{ValuePos: position, Kind: token.INT, Value: "0"}}}
	return &ast.BlockStmt{Lbrace: position, List: []ast.Stmt{&ast.ExprStmt{X: call}}}
}

// stubCaused is whether a type error at place, saying message, is one only stubbing the units in stubbed causes: an
// import nothing kept uses, or a variable or a return inside a stubbed unit.
func stubCaused(place unitAt, message string, stubbed []functionUnit, imports []unitAt) bool {
	// An import named apart is "imported as name and not used".
	if strings.Contains(message, "imported") && strings.HasSuffix(message, "and not used") {
		return slices.Contains(imports, place)
	}
	if !strings.Contains(message, "declared and not used") && !strings.Contains(message, "missing return") {
		return false
	}
	return slices.ContainsFunc(stubbed, func(unit functionUnit) bool { return unitHolds(unit, place) })
}

// variantOf is original with the bodies of stubbed replaced by stubs, parsed again from its files and type-checked
// again in its file set, named apart from it by seed: a stub panics, and returns as ctrlflow reads it exactly when
// the whole run found the function returns (stubCall). Its type errors are original's, the errors only the stubs
// cause filtered out by place (stubCaused); any other is an error naming it, a variant the stub cannot keep
// type-checking.
func variantOf(original *packages.Package, seed uint64, stubbed []functionUnit, noReturn map[unitAt]bool) (
	*packages.Package, error) {
	var syntax []*ast.File
	var imports []unitAt
	for _, file := range original.Syntax {
		name := original.Fset.File(file.FileStart).Name()
		contents, readError := os.ReadFile(name)
		if readError != nil {
			return nil, readError
		}
		parsed, parseError := parser.ParseFile(original.Fset, name, contents, parser.AllErrors|parser.ParseComments)
		if parseError != nil {
			return nil, fmt.Errorf("parsing %s again: %w", name, parseError)
		}
		for _, imported := range parsed.Imports {
			if at, isPlaced := positionAt(original.Fset, imported.Pos()); isPlaced {
				imports = append(imports, at)
			}
		}
		for _, declaration := range parsed.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			start, _ := positionAt(original.Fset, function.Pos())
			if !slices.ContainsFunc(stubbed, func(unit functionUnit) bool { return unit.start == start }) {
				continue
			}
			named, _ := positionAt(original.Fset, function.Name.Pos())
			function.Body = stubCall(function.Body.Lbrace, !noReturn[named])
		}
		syntax = append(syntax, parsed)
	}
	expected := map[string]bool{}
	for _, typeError := range original.TypeErrors {
		if at, isPlaced := positionAt(original.Fset, typeError.Pos); isPlaced {
			expected[fmt.Sprintf("%s:%d %s", at.file, at.offset, typeError.Msg)] = true
		}
	}
	var unexpected []string
	variantTypes := types.NewPackage(original.PkgPath, original.Name)
	configuration := &types.Config{
		Importer: importerFunction(func(path string) (*types.Package, error) {
			if path == "unsafe" {
				return types.Unsafe, nil
			}
			if imported := original.Imports[path]; imported != nil && imported.Types != nil {
				return imported.Types, nil
			}
			return nil, fmt.Errorf("%s imports %s, which its load never checked", original.ID, path)
		}),
		Error: func(found error) {
			var typeError types.Error
			if !errors.As(found, &typeError) {
				unexpected = append(unexpected, found.Error())
				return
			}
			at, isPlaced := positionAt(original.Fset, typeError.Pos)
			if isPlaced && (expected[fmt.Sprintf("%s:%d %s", at.file, at.offset, typeError.Msg)] ||
				stubCaused(at, typeError.Msg, stubbed, imports)) {
				return
			}
			unexpected = append(unexpected, typeError.Error())
		},
		Sizes:       original.TypesSizes,
		FakeImportC: true,
	}
	if original.Module != nil && original.Module.GoVersion != "" {
		configuration.GoVersion = "go" + original.Module.GoVersion
	}
	info := &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		FileVersions: map[*ast.File]string{},
	}
	// Ignored: every error went to configuration.Error, which judged it above.
	_ = types.NewChecker(configuration, original.Fset, variantTypes, info).Files(syntax)
	if len(unexpected) > 0 {
		return nil, fmt.Errorf("stubbing %d units of %s leaves type errors no stub explains: %q", len(stubbed),
			original.ID, unexpected)
	}
	variant := *original
	variant.ID = fmt.Sprintf("%s (variant %d)", original.ID, seed)
	variant.Types, variant.TypesInfo, variant.Syntax = variantTypes, info, syntax
	return &variant, nil
}

// usesCgo is whether analyzed is a cgo package, whose files the load gave the cgo tool's translation of, which a
// variant parsed again from the files as written would not match.
func usesCgo(analyzed *packages.Package) bool {
	if !slices.Equal(analyzed.GoFiles, analyzed.CompiledGoFiles) {
		return true
	}
	for _, file := range analyzed.Syntax {
		for _, imported := range file.Imports {
			if imported.Path.Value == `"C"` {
				return true
			}
		}
	}
	return false
}

// stubbedHalf is a seeded random half of units, rounded up, the same for the same package and seed in every run.
func stubbedHalf(units []functionUnit, packageIdentifier string, seed uint64) []functionUnit {
	hash := fnv.New64a()
	// Ignored: a hash's Write never fails.
	_, _ = hash.Write([]byte(packageIdentifier))
	shuffled := slices.Clone(units)
	random := rand.New(rand.NewPCG(seed, hash.Sum64()))
	random.Shuffle(len(shuffled), func(left int, right int) {
		shuffled[left], shuffled[right] = shuffled[right], shuffled[left]
	})
	return shuffled[:(len(shuffled)+1)/2]
}

// keptFindings are findings outside every stubbed unit, as one line each, counted.
func keptFindings(findings []isolationFinding, stubbed []functionUnit) map[string]int {
	kept := map[string]int{}
	for _, finding := range findings {
		if !slices.ContainsFunc(stubbed, func(unit functionUnit) bool { return unitHolds(unit, finding.at) }) {
			kept[findingLine(finding)]++
		}
	}
	return kept
}

// unitNamed is the unit of units holding place, by name, or the file's own level when none does.
func unitNamed(units []functionUnit, place unitAt) string {
	for _, unit := range units {
		if unitHolds(unit, place) {
			return unit.name
		}
	}
	return "the file's level"
}

// isolationCoverage is what one fixture's isolation compared: packages, variants, function units stubbed and kept,
// and findings compared in kept units.
type isolationCoverage struct {
	packages      int
	variants      int
	stubbedUnits  int
	keptUnits     int
	findingsKept  int
	skipped       []string
	disagreements []string
}

// isolatePackages runs the isolated rules over whole, and then over one variant per seed of each package fresh holds,
// the same packages from a load of their own, since a run lets go of its dependencies' syntax and a second run on
// them could learn no fact there; every variant runs in one run. It is what it compared and every disagreement: a
// finding in a kept unit that one run has and the other has not, a rule that crashed on a variant alone, and an
// answer a variant would have computed from a stub.
func isolatePackages(isolated []rule.Rule, whole []*packages.Package, fresh []*packages.Package, seeds []uint64) (
	isolationCoverage, error) {
	var coverage isolationCoverage
	var candidates []*packages.Package
	for _, candidate := range whole {
		switch {
		case usesCgo(candidate):
			coverage.skipped = append(coverage.skipped, candidate.ID+": it has cgo files, which a variant cannot "+
				"parse as its load translated them")
		case candidate.Types != nil && candidate.Types.Scope().Lookup("panic") != nil:
			// The stub is the builtin's panic, which a package declaring its own panic shadows, so no stub could
			// call it there.
			coverage.skipped = append(coverage.skipped, candidate.ID+": it declares its own panic, which shadows "+
				"the builtin the stub calls")
		case len(functionUnits(candidate)) == 0:
			coverage.skipped = append(coverage.skipped, candidate.ID+": it declares no function with a body")
		default:
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return coverage, nil
	}
	freshOf := map[string]*packages.Package{}
	for _, freshPackage := range fresh {
		freshOf[freshPackage.ID] = freshPackage
	}
	answered := &answeredWhole{analyzed: map[*types.Package]*packages.Package{}, answers: map[heldAnswerKey][]byte{}}
	for _, candidate := range candidates {
		answered.analyzed[candidate.Types] = candidate
	}
	wholeRun, wholeError := runIsolated(isolated, candidates, answered)
	if wholeError != nil {
		return coverage, fmt.Errorf("running the rules whole: %w", wholeError)
	}
	coverage.packages = len(candidates)
	held := &answeredFromWhole{whole: answered.answers, variants: map[*types.Package]heldVariant{}}
	var variants []*packages.Package
	type variantOfSeed struct {
		seed     uint64
		original *packages.Package
		stubbed  []functionUnit
	}
	madeOf := map[*packages.Package]variantOfSeed{}
	for _, seed := range seeds {
		for _, candidate := range candidates {
			freshPackage := freshOf[candidate.ID]
			if freshPackage == nil {
				return coverage, fmt.Errorf("the second load holds no %s", candidate.ID)
			}
			units := functionUnits(candidate)
			stubbed := stubbedHalf(units, candidate.ID, seed)
			variant, variantError := variantOf(freshPackage, seed, stubbed, wholeRun.noReturn)
			if variantError != nil {
				return coverage, fmt.Errorf("seed %d: %w", seed, variantError)
			}
			held.variants[variant.Types] = heldVariant{fileSet: variant.Fset, originalIdentifier: candidate.ID}
			variants = append(variants, variant)
			madeOf[variant] = variantOfSeed{seed: seed, original: candidate, stubbed: stubbed}
			coverage.variants++
			coverage.stubbedUnits += len(stubbed)
			coverage.keptUnits += len(units) - len(stubbed)
		}
	}
	variantRun, variantError := runIsolated(isolated, variants, held)
	if variantError != nil {
		return coverage, fmt.Errorf("running the rules on the variants: %w", variantError)
	}
	for _, miss := range held.misses {
		coverage.disagreements = append(coverage.disagreements, "an answer "+miss)
	}
	for _, variant := range variants {
		made := madeOf[variant]
		units := functionUnits(made.original)
		for _, ruleName := range slices.Sorted(maps.Keys(variantRun.crashed[variant])) {
			if _, crashedWhole := wholeRun.crashed[made.original][ruleName]; !crashedWhole {
				coverage.disagreements = append(coverage.disagreements, fmt.Sprintf("seed %d, %s: %s crashed on the "+
					"variant alone: %s", made.seed, made.original.ID, ruleName, variantRun.crashed[variant][ruleName]))
			}
		}
		wholeKept := keptFindings(wholeRun.findings[made.original], made.stubbed)
		variantKept := keptFindings(variantRun.findings[variant], made.stubbed)
		for _, count := range wholeKept {
			coverage.findingsKept += count
		}
		for _, line := range slices.Sorted(maps.Keys(wholeKept)) {
			if variantKept[line] != wholeKept[line] {
				coverage.disagreements = append(coverage.disagreements, fmt.Sprintf("seed %d, %s, in %s: the whole "+
					"package finds %s %d times, the variant %d", made.seed, made.original.ID,
					unitNamed(units, findingPlace(wholeRun.findings[made.original], line)), line, wholeKept[line],
					variantKept[line]))
			}
		}
		for _, line := range slices.Sorted(maps.Keys(variantKept)) {
			if wholeKept[line] == 0 {
				coverage.disagreements = append(coverage.disagreements, fmt.Sprintf("seed %d, %s, in %s: only the "+
					"variant finds %s", made.seed, made.original.ID,
					unitNamed(units, findingPlace(variantRun.findings[variant], line)), line))
			}
		}
	}
	return coverage, nil
}

// findingPlace is where the finding of findings that prints as line starts.
func findingPlace(findings []isolationFinding, line string) unitAt {
	for _, finding := range findings {
		if findingLine(finding) == line {
			return finding.at
		}
	}
	return unitAt{}
}

// isolationFixture is one fixture the isolation test loads: a directory, the patterns it loads, and its environment,
// `GOPATH` mode for a rule's testdata with a src directory, module mode for a testdata module, as analysistest loads
// each.
type isolationFixture struct {
	name        string
	directory   string
	patterns    []string
	environment []string
}

// isolationFixtures are every rule's testdata under rulesDirectory, each `GOPATH` root and each module, and the test's
// own fixture, sorted by name.
func isolationFixtures(test *testing.T, rulesDirectory string) []isolationFixture {
	test.Helper()
	fixtures := []isolationFixture{gopathFixture(test, "unit_isolation", filepath.Join("testdata", "unit_isolation"))}
	rulesRoot, absoluteError := filepath.Abs(rulesDirectory)
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	walkError := filepath.WalkDir(rulesRoot, func(path string, entry os.DirEntry, walkError error) error {
		if walkError != nil || !entry.IsDir() {
			return walkError
		}
		relative, relativeError := filepath.Rel(rulesRoot, path)
		if relativeError != nil {
			return relativeError
		}
		if entry.Name() == "testdata" {
			if source, statError := os.Stat(filepath.Join(path, "src")); statError == nil && source.IsDir() {
				fixtures = append(fixtures, gopathFixture(test, relative, path))
			}
			return nil
		}
		if !strings.Contains(relative, "testdata") || strings.Contains(relative, filepath.Join("testdata", "src")) {
			return nil
		}
		if _, statError := os.Stat(filepath.Join(path, "go.mod")); statError == nil {
			fixtures = append(fixtures, isolationFixture{name: relative, directory: path, patterns: []string{"./..."},
				environment: []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}})
			return filepath.SkipDir
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	slices.SortFunc(fixtures, func(left isolationFixture, right isolationFixture) int {
		return cmp.Compare(left.name, right.name)
	})
	return fixtures
}

// gopathFixture is the `GOPATH` root directory as a fixture named name, loading every package under each directory
// of its src.
func gopathFixture(test *testing.T, name string, directory string) isolationFixture {
	test.Helper()
	root, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	entries, readError := os.ReadDir(filepath.Join(root, "src"))
	if readError != nil {
		test.Fatal(readError)
	}
	fixture := isolationFixture{name: name, directory: root,
		environment: []string{"GOPATH=" + root, "GO111MODULE=off", "GOWORK=off"}}
	for _, entry := range entries {
		if entry.IsDir() {
			fixture.patterns = append(fixture.patterns, entry.Name()+"/...")
		}
	}
	return fixture
}

// isolateFixture loads fixture and isolates its packages, failing for each disagreement, and is what it compared, or
// false when the fixture does not load, which a rule's own test already fails on.
func isolateFixture(test *testing.T, isolated []rule.Rule, fixture isolationFixture) (isolationCoverage, bool) {
	test.Helper()
	if len(fixture.patterns) == 0 {
		return isolationCoverage{}, false
	}
	loaded, loadError := load.Packages(fixture.directory, fixture.patterns, fixture.environment, nil)
	if loadError != nil {
		test.Logf("%s does not load, so it is left out: %v", fixture.name, loadError)
		return isolationCoverage{}, false
	}
	fresh, freshError := load.Packages(fixture.directory, fixture.patterns, fixture.environment, nil)
	if freshError != nil {
		test.Errorf("%s loads once and not twice: %v", fixture.name, freshError)
		return isolationCoverage{}, true
	}
	coverage, isolateError := isolatePackages(isolated, fixtureOwn(fixture, loaded.Roots),
		fixtureOwn(fixture, fresh.Roots), unitIsolationSeeds)
	if isolateError != nil {
		test.Errorf("%s: %v", fixture.name, isolateError)
		return coverage, true
	}
	for _, disagreement := range coverage.disagreements {
		test.Errorf("%s: %s", fixture.name, disagreement)
	}
	return coverage, true
}

// fixtureOwn are the packages of roots whose files are fixture's own, leaving out a standard library package a
// `GOPATH` fixture's pattern also matches, as encoding/... matches the standard library's encoding/json.
func fixtureOwn(fixture isolationFixture, roots []*packages.Package) []*packages.Package {
	within := fixture.directory + string(filepath.Separator)
	return slices.DeleteFunc(slices.Clone(roots), func(root *packages.Package) bool {
		return len(root.GoFiles) == 0 || !strings.HasPrefix(root.GoFiles[0], within)
	})
}

// unitIsolationVariable names the environment variable that widens the isolation past the test's own fixture: set to
// fixtures, it isolates every rule's, about 140 s on three threads (#h350vqr), seven times what the module's tests can
// give it, so the nightly guard runs it; set to rules, it isolates the fixtures of the rule directories
// unitIsolationRulesVariable names, which cohere-dev land sets to the rule directories a landing touches.
const unitIsolationVariable = "COHERE_UNIT_ISOLATION"

// unitIsolationRulesVariable names the rule directories COHERE_UNIT_ISOLATION=rules isolates the fixtures of,
// comma-separated, each relative to the module's root, as internal/go_engine/rules/vet/printf.
const unitIsolationRulesVariable = "COHERE_UNIT_ISOLATION_RULES"

// selectedFixtures are the fixtures of fixtures the isolation runs over, as setting, COHERE_UNIT_ISOLATION, asks: every
// one for fixtures; the test's own and each under a rule directory rules names, relative to moduleRoot, for rules; and
// the test's own alone otherwise. A rule directory that holds no fixture is an error, since a landing that named it
// would pass having isolated nothing of it.
func selectedFixtures(fixtures []isolationFixture, setting string, rules string, moduleRoot string) (
	[]isolationFixture, error) {
	if setting == "fixtures" {
		return fixtures, nil
	}
	own := func(fixture isolationFixture) bool { return fixture.name == "unit_isolation" }
	selected := slices.DeleteFunc(slices.Clone(fixtures), func(fixture isolationFixture) bool {
		return !own(fixture)
	})
	if setting != "rules" {
		return selected, nil
	}
	for _, directory := range strings.Split(rules, ",") {
		if directory == "" {
			continue
		}
		within := filepath.Join(moduleRoot, filepath.FromSlash(directory)) + string(filepath.Separator)
		held := 0
		for _, fixture := range fixtures {
			if own(fixture) || !strings.HasPrefix(fixture.directory+string(filepath.Separator), within) {
				continue
			}
			held++
			if !slices.ContainsFunc(selected, func(chosen isolationFixture) bool {
				return chosen.directory == fixture.directory
			}) {
				selected = append(selected, fixture)
			}
		}
		if held == 0 {
			return nil, fmt.Errorf("%s names %s, which holds no fixture", unitIsolationRulesVariable, directory)
		}
	}
	return selected, nil
}

// Every rule the side table declares PerUnit or FileSyntax finds in each unit a variant keeps what it finds there in
// the whole package, over the test's own fixture, whose units call one another, and, with
// COHERE_UNIT_ISOLATION=fixtures, over every rule's fixtures too, or with =rules over those of the rule directories
// COHERE_UNIT_ISOLATION_RULES names, each package under every seed of unitIsolationSeeds: a rule that reads another
// unit's body or initializer goes red, named with the unit.
func TestEveryIsolatedRuleFindsTheSameInTheUnitsAVariantKeeps(test *testing.T) {
	test.Parallel()
	isolated := isolatedRules(test)
	moduleRoot, rootError := filepath.Abs(filepath.Join("..", "..", ".."))
	if rootError != nil {
		test.Fatal(rootError)
	}
	fixtures, selectError := selectedFixtures(isolationFixtures(test, filepath.Join("..", "rules")),
		os.Getenv(unitIsolationVariable), os.Getenv(unitIsolationRulesVariable), moduleRoot)
	if selectError != nil {
		test.Fatal(selectError)
	}
	if len(fixtures) == 1 {
		test.Logf("isolating the test's own fixture alone; %s=fixtures isolates every rule's too, and =rules those "+
			"of the rule directories %s names", unitIsolationVariable, unitIsolationRulesVariable)
	}
	var lock sync.Mutex
	var total isolationCoverage
	fixturesRun := 0
	// Not parallel: the group returns only once every fixture's parallel subtest has, so the summary below reads
	// their totals whole.
	test.Run("fixtures", func(test *testing.T) {
		for _, fixture := range fixtures {
			test.Run(strings.ReplaceAll(fixture.name, string(filepath.Separator), "_"), func(test *testing.T) {
				test.Parallel()
				coverage, loaded := isolateFixture(test, isolated, fixture)
				lock.Lock()
				defer lock.Unlock()
				if loaded {
					fixturesRun++
				}
				total.packages += coverage.packages
				total.variants += coverage.variants
				total.stubbedUnits += coverage.stubbedUnits
				total.keptUnits += coverage.keptUnits
				total.findingsKept += coverage.findingsKept
				total.skipped = append(total.skipped, coverage.skipped...)
			})
		}
	})
	test.Logf("isolated %d rules over %d of %d fixtures: %d packages, %d variants over seeds %v, %d function units "+
		"stubbed and %d kept, %d findings in kept units compared; %d packages left out", len(isolated), fixturesRun,
		len(fixtures), total.packages, total.variants, unitIsolationSeeds, total.stubbedUnits, total.keptUnits,
		total.findingsKept, len(total.skipped))
	if total.findingsKept == 0 || total.keptUnits == 0 {
		test.Error("no finding in a kept unit was compared, so the test proves nothing")
	}
}

// Every analyzer below an isolated rule that learns facts is one isolatedFactAnalyzers says a variant's facts are the
// whole run's for, so a rule that comes to read a fact the variant would recompute from a stub fails here before it
// can pass the isolation test on a fact it never saw held.
func TestTheIsolatedRulesLearnOnlyFactsTheVariantKeeps(test *testing.T) {
	test.Parallel()
	seen := map[*analysis.Analyzer]bool{}
	var walk func(analyzer *analysis.Analyzer)
	walk = func(analyzer *analysis.Analyzer) {
		if seen[analyzer] {
			return
		}
		seen[analyzer] = true
		if len(analyzer.FactTypes) > 0 && !isolatedFactAnalyzers[analyzer.Name] {
			test.Errorf("%s learns facts, which a variant would recompute from its stubs; hold them as the whole "+
				"run's, or declare the rules that read them Composes", analyzer.Name)
		}
		for _, required := range analyzer.Requires {
			walk(required)
		}
	}
	for _, registered := range isolatedRules(test) {
		walk(registered.Analyzer)
		if registered.Listener != nil {
			for _, required := range registered.Listener.Requires {
				walk(required)
			}
		}
	}
}

// pinnedFindings are the findings the pin keeps, each resting on one fact or answer about a callee the variant stubs:
// Unreached's value is never used because halt never returns, Leaks drops its cancel function on a path that exists
// because proceed returns, and Discards drops the result of double, which is pure.
var pinnedFindings = map[string]string{
	rule.Namespace + "SA4006":     "Unreached",
	rule.Namespace + "lostcancel": "Leaks",
	rule.Namespace + "SA4017":     "Discards",
}

// pinnedRules are the isolated rules whose findings the pin keeps.
func pinnedRules(test *testing.T) []rule.Rule {
	test.Helper()
	return slices.DeleteFunc(isolatedRules(test), func(registered rule.Rule) bool {
		return pinnedFindings[registered.Name] == ""
	})
}

// pinnedRun is the pin's variant run: the findings in the units it kept, by unit, and every finding the whole package
// has there.
type pinnedRun struct {
	whole   map[string][]string
	variant map[string][]string
}

// runPinned loads the test's own fixture twice, runs the pinned rules whole and on a variant stubbing halt, proceed
// and double, and is what each found in the units the pin keeps. Faithful, the variant is the isolation test's own:
// each stub returns as ctrlflow reads it exactly when the whole run found its function returns, and every answer is
// the whole run's. Otherwise every stub is panic(0), and the variant computes its answers from the stubs.
func runPinned(test *testing.T, faithful bool) pinnedRun {
	test.Helper()
	fixture := gopathFixture(test, "unit_isolation", filepath.Join("testdata", "unit_isolation"))
	whole, wholeError := load.Packages(fixture.directory, fixture.patterns, fixture.environment, nil)
	fresh, freshError := load.Packages(fixture.directory, fixture.patterns, fixture.environment, nil)
	if wholeError != nil || freshError != nil || len(whole.Roots) != 1 || len(fresh.Roots) != 1 {
		test.Fatalf("loading the fixture: %v, %v", wholeError, freshError)
	}
	pinned := pinnedRules(test)
	answered := &answeredWhole{analyzed: map[*types.Package]*packages.Package{whole.Roots[0].Types: whole.Roots[0]},
		answers: map[heldAnswerKey][]byte{}}
	wholeRun, runError := runIsolated(pinned, whole.Roots, answered)
	if runError != nil {
		test.Fatal(runError)
	}
	units := functionUnits(whole.Roots[0])
	stubbed := slices.DeleteFunc(slices.Clone(units), func(unit functionUnit) bool {
		return unit.name != "halt" && unit.name != "proceed" && unit.name != "double"
	})
	noReturn := wholeRun.noReturn
	var held fact_resolver.Held
	if faithful {
		answers := &answeredFromWhole{whole: answered.answers, variants: map[*types.Package]heldVariant{}}
		held = answers
		defer func() {
			for _, miss := range answers.misses {
				test.Errorf("an answer %s", miss)
			}
		}()
	} else {
		noReturn = map[unitAt]bool{}
		for _, file := range fresh.Roots[0].Syntax {
			for _, declaration := range file.Decls {
				if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
					if named, isPlaced := positionAt(fresh.Roots[0].Fset, function.Name.Pos()); isPlaced {
						noReturn[named] = true
					}
				}
			}
		}
	}
	variant, variantError := variantOf(fresh.Roots[0], 0, stubbed, noReturn)
	if variantError != nil {
		test.Fatal(variantError)
	}
	if answers, isAnswers := held.(*answeredFromWhole); isAnswers {
		answers.variants[variant.Types] = heldVariant{fileSet: variant.Fset, originalIdentifier: whole.Roots[0].ID}
	}
	variantRun, variantError := runIsolated(pinned, []*packages.Package{variant}, held)
	if variantError != nil {
		test.Fatal(variantError)
	}
	inUnits := func(findings []isolationFinding) map[string][]string {
		found := map[string][]string{}
		for _, finding := range findings {
			if unit := unitNamed(units, finding.at); pinnedFindings[finding.rule] == unit {
				found[unit] = append(found[unit], findingLine(finding))
			}
		}
		return found
	}
	return pinnedRun{whole: inUnits(wholeRun.findings[whole.Roots[0]]), variant: inUnits(variantRun.findings[variant])}
}

// The pin (#h350vqr): a kept unit whose finding rests on a fact or answer about a callee the variant stubs keeps the
// finding, the fact or answer held at the whole run's. Unreached keeps SA4006 with halt, which never returns, stubbed;
// Leaks keeps both lostcancel findings with proceed, which returns, stubbed by a panic, which would not; and Discards
// keeps SA4017 with double, which is pure, stubbed. The control runs the same variant with every stub a bare panic(0)
// and answers computed from the stubs, and loses Leaks's and Discards's findings, so the pin holds something a naive
// stub breaks.
func TestAKeptUnitKeepsTheFindingThatRestsOnAStubbedCallee(test *testing.T) {
	test.Parallel()
	faithful := runPinned(test, true)
	for _, unit := range slices.Sorted(maps.Values(pinnedFindings)) {
		if len(faithful.whole[unit]) == 0 {
			test.Errorf("%s: the whole package finds nothing there, so the pin pins nothing", unit)
		}
		if !slices.Equal(faithful.variant[unit], faithful.whole[unit]) {
			test.Errorf("%s: the variant finds %q, the whole package %q", unit, faithful.variant[unit],
				faithful.whole[unit])
		}
	}
	naive := runPinned(test, false)
	for _, unit := range []string{"Leaks", "Discards"} {
		if slices.Equal(naive.variant[unit], naive.whole[unit]) {
			test.Errorf("%s: with bare stubs and answers computed from them the variant still finds %q, so the pin "+
				"holds nothing a naive stub breaks", unit, naive.variant[unit])
		}
	}
}

// treeSampleSize is how many of cohere's own packages the nightly sampled pass isolates, when
// COHERE_UNIT_ISOLATION_SAMPLE does not say.
const treeSampleSize = 8

// treePackageDirectories are the directories under root holding a Go package of cohere's own, as patterns go list
// takes, leaving out testdata, hidden directories and what a symbolic link reaches, sorted.
func treePackageDirectories(test *testing.T, root string) []string {
	test.Helper()
	var directories []string
	walkError := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			if name := entry.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata" ||
				name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, relativeError := filepath.Rel(root, filepath.Dir(path))
		if relativeError != nil {
			return relativeError
		}
		pattern := "./" + filepath.ToSlash(relative)
		if !slices.Contains(directories, pattern) {
			directories = append(directories, pattern)
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	slices.Sort(directories)
	return directories
}

// The sampled pass over cohere's own tree, for the nightly guard (#h350vqr): with COHERE_UNIT_ISOLATION=tree, a sample
// of cohere's packages, COHERE_UNIT_ISOLATION_SAMPLE of them or treeSampleSize, chosen by the seed
// COHERE_UNIT_ISOLATION_SEED names, is isolated as the fixtures are, each package under that one seed, so a night's
// failure is reproduced by running the test again with the seed it logs. The fixtures' test runs in the module's tests;
// this one only when asked, since each package loads its dependencies from source twice.
func TestASampleOfCoheresTreeFindsTheSameInTheUnitsAVariantKeeps(test *testing.T) {
	test.Parallel()
	if os.Getenv(unitIsolationVariable) != "tree" {
		test.Skipf("%s=tree runs the sampled pass over cohere's tree, with the seed %s_SEED names",
			unitIsolationVariable, unitIsolationVariable)
	}
	var seed uint64
	var sampleSize uint64 = treeSampleSize
	if _, scanError := fmt.Sscan(os.Getenv(unitIsolationVariable+"_SEED"), &seed); scanError != nil {
		test.Fatalf("%s_SEED must name the night's seed, a whole number: %v", unitIsolationVariable, scanError)
	}
	if asked := os.Getenv(unitIsolationVariable + "_SAMPLE"); asked != "" {
		if _, scanError := fmt.Sscan(asked, &sampleSize); scanError != nil {
			test.Fatalf("%s_SAMPLE must be a whole number: %v", unitIsolationVariable, scanError)
		}
	}
	root, rootError := filepath.Abs(filepath.Join("..", "..", ".."))
	if rootError != nil {
		test.Fatal(rootError)
	}
	directories := treePackageDirectories(test, root)
	random := rand.New(rand.NewPCG(seed, 0))
	random.Shuffle(len(directories), func(left int, right int) {
		directories[left], directories[right] = directories[right], directories[left]
	})
	sample := directories[:min(int(sampleSize), len(directories))]
	slices.Sort(sample)
	test.Logf("seed %d: isolating %d of cohere's %d packages: %q", seed, len(sample), len(directories), sample)
	tree := isolationFixture{name: "cohere", directory: root, patterns: sample, environment: []string{"GOWORK=off"}}
	whole, wholeError := load.Packages(tree.directory, tree.patterns, tree.environment, nil)
	fresh, freshError := load.Packages(tree.directory, tree.patterns, tree.environment, nil)
	if wholeError != nil || freshError != nil {
		test.Fatalf("loading the sample: %v, %v", wholeError, freshError)
	}
	coverage, isolateError := isolatePackages(isolatedRules(test), whole.Roots, fresh.Roots, []uint64{seed})
	if isolateError != nil {
		test.Fatalf("seed %d: %v", seed, isolateError)
	}
	for _, disagreement := range coverage.disagreements {
		test.Errorf("seed %d: %s", seed, disagreement)
	}
	test.Logf("seed %d: %d packages, %d variants, %d function units stubbed and %d kept, %d findings in kept units "+
		"compared; left out %q", seed, coverage.packages, coverage.variants, coverage.stubbedUnits, coverage.keptUnits,
		coverage.findingsKept, coverage.skipped)
}

// The isolation reads its fixtures from COHERE_UNIT_ISOLATION as cohere-dev land and the nightly guard set it: the
// test's own alone when unset, every fixture for fixtures, and for rules the test's own and each fixture under a rule
// directory COHERE_UNIT_ISOLATION_RULES names, a `GOPATH` root and a module alike, never a sibling's. A named directory
// with no fixture is refused.
func TestTheIsolationRunsTheFixturesTheVariableNames(test *testing.T) {
	test.Parallel()
	root := filepath.Join(string(filepath.Separator), "module")
	rules := filepath.Join(root, "internal", "go_engine", "rules")
	fixtures := []isolationFixture{
		{name: "unit_isolation", directory: filepath.Join(root, "internal", "go_engine", "check", "testdata",
			"unit_isolation")},
		{name: "printf", directory: filepath.Join(rules, "vet", "printf", "testdata")},
		{name: "printf_extra", directory: filepath.Join(rules, "vet", "printf_extra", "testdata")},
		{name: "deprecated_use parity", directory: filepath.Join(rules, "deprecated_use", "testdata", "parity")},
		{name: "deprecated_use differences", directory: filepath.Join(rules, "deprecated_use", "testdata",
			"differences")},
	}
	names := func(selected []isolationFixture) []string {
		var named []string
		for _, fixture := range selected {
			named = append(named, fixture.name)
		}
		return named
	}
	for _, chosen := range []struct {
		setting string
		rules   string
		want    []string
	}{
		{"", "", []string{"unit_isolation"}},
		{"fixtures", "", names(fixtures)},
		{"rules", "internal/go_engine/rules/vet/printf", []string{"unit_isolation", "printf"}},
		{"rules", "internal/go_engine/rules/deprecated_use,internal/go_engine/rules/vet/printf",
			[]string{"unit_isolation", "deprecated_use parity", "deprecated_use differences", "printf"}},
	} {
		selected, selectError := selectedFixtures(fixtures, chosen.setting, chosen.rules, root)
		if selectError != nil || !slices.Equal(names(selected), chosen.want) {
			test.Errorf("%s=%q, %s=%q: want %q, got %q (%v)", unitIsolationVariable, chosen.setting,
				unitIsolationRulesVariable, chosen.rules, chosen.want, names(selected), selectError)
		}
	}
	_, selectError := selectedFixtures(fixtures, "rules", "internal/go_engine/rules/nothing_here", root)
	if selectError == nil {
		test.Error("a rule directory with no fixture was accepted")
	}
}
