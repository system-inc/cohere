package check

import (
	"fmt"
	"go/ast"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
)

// recordingKind is one kind the stub differential holds to its flag (fact_resolver.Kind.RecordsEveryJudgment): the
// kind, the directory of its package, whose testdata is the `GOPATH` root of its fixtures, whether its answers are
// about a function's parameters, where a question of a function is one of each parameter, and the directory of its two
// packages in the recording_across fixture, lower and upper, which imports it.
type recordingKind struct {
	kind        *fact_resolver.Kind
	directory   string
	byParameter bool
	across      string
}

// recordingKinds are the kinds the differential runs over, which TestNoAnswerOfARecordingKindRestsOnABodyWithNoAnswer
// holds to be every registered kind that sets the flag.
var recordingKinds = []recordingKind{
	{kind: function_purity.Kind, directory: "function_purity", across: "purity"},
	{kind: odd_slice_argument.Kind, directory: "odd_slice_argument", byParameter: true, across: "odd"},
	{kind: return_nil_ness.Kind, directory: "return_nil_ness", across: "nilness"},
}

// recordingQuestions is one copy of a fixture package and the questions asked of it: the package it copies, what the
// copy is asked for in a failure, and the units whose functions are asked of.
type recordingQuestions struct {
	original *packages.Package
	asked    string
	units    []functionUnit
}

// recordingSeeds are the seeds of the halves each package is asked beside its functions one at a time.
var recordingSeeds = []uint64{1, 2, 3}

// recordingAnswers are the answers of one kind a run holds about each package's own objects, each by the place of its
// object, encoded.
type recordingAnswers map[*types.Package]map[unitAt]string

// askRecording runs a pass over analyzed that asks kind of each function questions names for a package, of the
// function itself or of each of its parameters, through the kind as it registered (fact_resolver.Resolver.Asked), and
// is every answer of the kind the run then holds about each package's own objects, and the places of the functions
// ctrlflow found never return.
func askRecording(
	kind recordingKind,
	analyzed []*packages.Package,
	questions map[*types.Package]recordingQuestions,
) (recordingAnswers, map[unitAt]bool, error) {
	asker := &analysis.Analyzer{
		Name:     "recording_questions",
		Doc:      "asks one kind of the functions a test names, each of its own package",
		Requires: []*analysis.Analyzer{ctrlflow.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			resolver, resolverError := fact_resolver.Of(pass)
			if resolverError != nil {
				return nil, resolverError
			}
			reader := resolver.Package(pass.Pkg)
			if reader == nil {
				return nil, fmt.Errorf("the resolver never learned %s", pass.Pkg.Path())
			}
			for _, file := range pass.Files {
				for _, declaration := range file.Decls {
					function, isFunction := declaration.(*ast.FuncDecl)
					if !isFunction || function.Body == nil {
						continue
					}
					start, _ := positionAt(pass.Fset, function.Pos())
					if !slices.ContainsFunc(questions[pass.Pkg].units, func(unit functionUnit) bool {
						return unit.start == start
					}) {
						continue
					}
					declared, isDeclared := pass.TypesInfo.Defs[function.Name].(*types.Func)
					if !isDeclared {
						continue
					}
					objects := []types.Object{declared}
					if kind.byParameter {
						objects = nil
						for parameter := range declared.Signature().Params().Variables() {
							objects = append(objects, parameter)
						}
					}
					for _, object := range objects {
						if _, _, registered := resolver.Asked(kind.kind.Name, reader, object); !registered {
							return nil, fmt.Errorf("the kind %s is not registered", kind.kind.Name)
						}
					}
				}
			}
			return nil, nil
		},
	}
	var resolver *fact_resolver.Resolver
	run, runError := runIsolatedWith([]rule.Rule{houseRule(asker)}, analyzed, nil, func(options *checker.Options) {
		resolver, _ = options.Resolver.(*fact_resolver.Resolver)
	})
	if runError != nil {
		return nil, nil, runError
	}
	for crashed, messages := range run.crashed {
		return nil, nil, fmt.Errorf("asking %s of %s crashed: %v", kind.kind.Name, crashed.ID, messages)
	}
	if resolver == nil {
		return nil, nil, fmt.Errorf("the run's options held no fact resolver")
	}
	fileSets := map[*types.Package]*packages.Package{}
	for _, each := range analyzed {
		fileSets[each.Types] = each
	}
	answers := recordingAnswers{}
	for _, answer := range resolver.Answered() {
		owner := fileSets[answer.Object.Pkg()]
		if answer.Kind != kind.kind || owner == nil {
			continue
		}
		at, isPlaced := positionAt(owner.Fset, answer.Object.Pos())
		if !isPlaced {
			return nil, nil, fmt.Errorf("the answer about %s of %s has no place", answer.Object.Name(), owner.ID)
		}
		if answers[owner.Types] == nil {
			answers[owner.Types] = map[unitAt]string{}
		}
		answers[owner.Types][at] = string(answer.Encoded)
	}
	return answers, run.noReturn, nil
}

// recordingCoverage is what one kind's differential compared: the copies asked, those of them with a function to stub,
// the functions stubbed, and the answers held to be the same.
type recordingCoverage struct {
	copies   int
	variants int
	stubbed  int
	compared int
}

// stubUnanswered runs one kind's differential over its fixtures: each package is copied once for each of its
// functions, asked alone, and once for each seeded half of them, asked together; every copy is asked in one run, and
// then again, in a second run over a second load, with the body of every function the first run held no answer about
// replaced by a stub (variantOf), which keeps ctrlflow's no-return of it. It is what it compared and every
// disagreement: an answer the first run held that the second holds otherwise, or not at all.
func stubUnanswered(test *testing.T, kind recordingKind) (recordingCoverage, []string) {
	test.Helper()
	fixture := gopathFixture(test, kind.directory, filepath.Join("..", "rules", kind.directory, "testdata"))
	loadOwn := func() map[string]*packages.Package {
		loaded, loadError := load.Packages(fixture.directory, fixture.patterns, fixture.environment, nil)
		if loadError != nil {
			test.Fatalf("loading %s's fixtures: %v", kind.directory, loadError)
		}
		own := map[string]*packages.Package{}
		for _, root := range fixtureOwn(fixture, loaded.Roots) {
			own[root.ID] = root
		}
		return own
	}
	first, second := loadOwn(), loadOwn()
	var coverage recordingCoverage
	var copies []*packages.Package
	questions := map[*types.Package]recordingQuestions{}
	seed := uint64(0)
	for _, identifier := range slices.Sorted(maps.Keys(first)) {
		original := first[identifier]
		units := functionUnits(original)
		if usesCgo(original) || original.Types == nil || original.Types.Scope().Lookup("panic") != nil ||
			len(units) == 0 {
			continue
		}
		asked := map[string][]functionUnit{}
		for _, unit := range units {
			asked[unit.name+" alone"] = []functionUnit{unit}
		}
		for _, halfSeed := range recordingSeeds {
			asked[fmt.Sprintf("the half of seed %d", halfSeed)] = stubbedHalf(units, original.ID, halfSeed)
		}
		for _, name := range slices.Sorted(maps.Keys(asked)) {
			seed++
			copied, copyError := variantOf(original, seed, nil, nil)
			if copyError != nil {
				test.Fatalf("copying %s: %v", original.ID, copyError)
			}
			copies = append(copies, copied)
			questions[copied.Types] = recordingQuestions{original: original, asked: name, units: asked[name]}
		}
	}
	coverage.copies = len(copies)
	wholeAnswers, noReturn, wholeError := askRecording(kind, copies, questions)
	if wholeError != nil {
		test.Fatalf("asking %s of the fixtures whole: %v", kind.kind.Name, wholeError)
	}
	var variants []*packages.Package
	variantQuestions := map[*types.Package]recordingQuestions{}
	wholeOf := map[*types.Package]map[unitAt]string{}
	stubbedOf := map[*types.Package][]functionUnit{}
	for _, copied := range copies {
		asked := questions[copied.Types]
		held := wholeAnswers[copied.Types]
		var stubbed []functionUnit
		for _, unit := range functionUnits(asked.original) {
			isAnswered := false
			for at := range held {
				isAnswered = isAnswered || unitHolds(unit, at)
			}
			if !isAnswered {
				stubbed = append(stubbed, unit)
			}
		}
		if len(held) == 0 || len(stubbed) == 0 {
			// Nothing to compare, or nothing to stub: the copy holds no answer, or one about every function.
			continue
		}
		fresh := second[asked.original.ID]
		if fresh == nil {
			test.Fatalf("the second load holds no %s", asked.original.ID)
		}
		seed++
		variant, variantError := variantOf(fresh, seed, stubbed, noReturn)
		if variantError != nil {
			test.Fatalf("stubbing %s, asked of %s: %v", asked.original.ID, asked.asked, variantError)
		}
		variants = append(variants, variant)
		variantQuestions[variant.Types] = asked
		wholeOf[variant.Types], stubbedOf[variant.Types] = held, stubbed
		coverage.variants++
		coverage.stubbed += len(stubbed)
	}
	variantAnswers, _, variantError := askRecording(kind, variants, variantQuestions)
	if variantError != nil {
		test.Fatalf("asking %s of the stubbed variants: %v", kind.kind.Name, variantError)
	}
	var disagreements []string
	for _, variant := range variants {
		asked := variantQuestions[variant.Types]
		units := functionUnits(asked.original)
		var stubbedNames []string
		for _, unit := range stubbedOf[variant.Types] {
			stubbedNames = append(stubbedNames, unit.name)
		}
		for at, whole := range wholeOf[variant.Types] {
			coverage.compared++
			stubbed, isHeld := variantAnswers[variant.Types][at]
			if isHeld && stubbed == whole {
				continue
			}
			disagreements = append(disagreements, fmt.Sprintf("%s of %s, asked of %s: the answer in %s at %s:%d is "+
				"%s, and %q (held: %t) once the bodies the run held no answer about are stubs, %s", kind.kind.Name,
				asked.original.ID, asked.asked, unitNamed(units, at), filepath.Base(at.file), at.offset, whole, stubbed,
				isHeld, strings.Join(stubbedNames, ", ")))
		}
	}
	slices.Sort(disagreements)
	return coverage, disagreements
}

// The second half of what a kind's flag claims (fact_resolver.Kind.RecordsEveryJudgment), run instead of read: no
// answer the run holds rests on the body of a function it holds no answer about. Over each flagged kind's fixtures,
// every package is asked of each of its functions alone, and of three halves of them, and then asked the same again
// with the body of every function the first run held no answer about replaced by a stub: every answer the first run
// held is held the same by the second. A judgment that looked into a callee's body and made no verdict of it, whose
// record would be as complete, changes an answer here. The stub keeps what ctrlflow said of the function's returning,
// a fact the checker pushes and a reader compares on its own. Every registered kind that sets the flag is run.
func TestNoAnswerOfARecordingKindRestsOnABodyWithNoAnswer(test *testing.T) {
	test.Parallel()
	flagged := fact_resolver.RecordingNames()
	var run []string
	for _, kind := range recordingKinds {
		if kind.kind.RecordsEveryJudgment {
			run = append(run, kind.kind.Name)
		}
	}
	slices.Sort(run)
	if !slices.Equal(flagged, run) || len(run) == 0 {
		test.Fatalf("the registered kinds that say they record every judgment are %q, and the differential runs %q",
			flagged, run)
	}
	for _, kind := range recordingKinds {
		if !kind.kind.RecordsEveryJudgment {
			continue
		}
		test.Run(kind.kind.Name, func(test *testing.T) {
			test.Parallel()
			coverage, disagreements := stubUnanswered(test, kind)
			for _, disagreement := range disagreements {
				test.Error(disagreement)
			}
			if coverage.variants == 0 || coverage.stubbed == 0 || coverage.compared == 0 {
				test.Errorf("the differential asked %d copies, stubbed %d functions over %d of them and compared %d "+
					"answers, want some of each, or it holds nothing", coverage.copies, coverage.stubbed,
					coverage.variants, coverage.compared)
			}
			test.Logf("%s: %d copies asked, %d with a function to stub, %d functions stubbed, %d answers the same",
				kind.kind.Name, coverage.copies, coverage.variants, coverage.stubbed, coverage.compared)
		})
	}
}

// acrossPair is the two packages of one kind's directory in the recording_across fixture, from a load of their own:
// lower, and upper, which imports it.
func acrossPair(test *testing.T, kind recordingKind) (*packages.Package, *packages.Package) {
	test.Helper()
	fixture := gopathFixture(test, "recording_across", filepath.Join("testdata", "recording_across"))
	loaded, loadError := load.Packages(fixture.directory, []string{kind.across + "/..."}, fixture.environment, nil)
	if loadError != nil {
		test.Fatalf("loading %s's two packages: %v", kind.across, loadError)
	}
	var lower *packages.Package
	var upper *packages.Package
	for _, root := range loaded.Roots {
		switch root.PkgPath {
		case kind.across + "/lower":
			lower = root
		case kind.across + "/upper":
			upper = root
		}
	}
	if lower == nil || upper == nil || upper.Imports[kind.across+"/lower"] != lower {
		test.Fatalf("the load of %s holds lower %v and upper %v, want both, upper importing lower", kind.across,
			lower, upper)
	}
	return lower, upper
}

// stubAcross runs one kind's differential across a package boundary, over its two packages in the recording_across
// fixture: upper is asked of every function it declares, in one run, and then again, in a second run over a second
// load, with the body of every function of lower the first run held no answer about replaced by a stub, and upper
// type-checked again over that lower. It is what it compared and every disagreement: an answer about either package's
// objects the first run held that the second holds otherwise, or not at all.
func stubAcross(test *testing.T, kind recordingKind) (recordingCoverage, []string) {
	test.Helper()
	lower, upper := acrossPair(test, kind)
	asked := recordingQuestions{original: upper, asked: "every function of upper", units: functionUnits(upper)}
	wholeAnswers, noReturn, wholeError := askRecording(kind, []*packages.Package{lower, upper},
		map[*types.Package]recordingQuestions{upper.Types: asked})
	if wholeError != nil {
		test.Fatalf("asking %s of %s whole: %v", kind.kind.Name, kind.across, wholeError)
	}
	var stubbed []functionUnit
	var stubbedNames []string
	for _, unit := range functionUnits(lower) {
		isAnswered := false
		for at := range wholeAnswers[lower.Types] {
			isAnswered = isAnswered || unitHolds(unit, at)
		}
		if !isAnswered {
			stubbed = append(stubbed, unit)
			stubbedNames = append(stubbedNames, unit.name)
		}
	}
	freshLower, freshUpper := acrossPair(test, kind)
	lowerVariant, lowerError := variantOf(freshLower, 1, stubbed, noReturn)
	if lowerError != nil {
		test.Fatalf("stubbing %s: %v", freshLower.ID, lowerError)
	}
	// upper as the second load holds it, importing the stubbed lower, which variantOf type-checks it against.
	importing := *freshUpper
	importing.Imports = maps.Clone(freshUpper.Imports)
	importing.Imports[lowerVariant.PkgPath] = lowerVariant
	upperVariant, upperError := variantOf(&importing, 1, nil, nil)
	if upperError != nil {
		test.Fatalf("checking %s over the stubbed lower: %v", freshUpper.ID, upperError)
	}
	variantAnswers, _, variantError := askRecording(kind, []*packages.Package{lowerVariant, upperVariant},
		map[*types.Package]recordingQuestions{upperVariant.Types: asked})
	if variantError != nil {
		test.Fatalf("asking %s of %s over the stubbed lower: %v", kind.kind.Name, kind.across, variantError)
	}
	coverage := recordingCoverage{copies: 1, variants: 1, stubbed: len(stubbed)}
	var disagreements []string
	for _, pair := range [][2]*packages.Package{{lower, lowerVariant}, {upper, upperVariant}} {
		units := functionUnits(pair[0])
		for at, whole := range wholeAnswers[pair[0].Types] {
			coverage.compared++
			stubbedAnswer, isHeld := variantAnswers[pair[1].Types][at]
			if isHeld && stubbedAnswer == whole {
				continue
			}
			disagreements = append(disagreements, fmt.Sprintf("%s, asked of every function of %s: the answer in %s "+
				"of %s at %s:%d is %s, and %q (held: %t) once lower's bodies the run held no answer about are stubs, "+
				"%s", kind.kind.Name, upper.ID, unitNamed(units, at), pair[0].ID, filepath.Base(at.file), at.offset,
				whole, stubbedAnswer, isHeld, strings.Join(stubbedNames, ", ")))
		}
	}
	slices.Sort(disagreements)
	return coverage, disagreements
}

// The second half of what a kind's flag claims, run across a package boundary, where a judgment reaches a callee of
// another package: no answer the run holds rests on the body of a function of another package it holds no answer
// about. For each flagged kind, upper is asked of its functions, each resting on a function of lower, and then asked
// the same again with every function of lower the first run held no answer about a stub: every answer the first run
// held, about either package's objects, is held the same by the second. A judgment that read lower's function in place,
// with no answer of it asked, would leave that function with no answer, and its stub would change upper's.
// TestNoAnswerOfARecordingKindRestsOnABodyWithNoAnswer stubs only a package's own functions, which no such read
// reaches.
func TestNoAnswerOfARecordingKindRestsOnABodyWithNoAnswerAcrossAPackage(test *testing.T) {
	test.Parallel()
	for _, kind := range recordingKinds {
		if !kind.kind.RecordsEveryJudgment {
			continue
		}
		test.Run(kind.kind.Name, func(test *testing.T) {
			test.Parallel()
			coverage, disagreements := stubAcross(test, kind)
			for _, disagreement := range disagreements {
				test.Error(disagreement)
			}
			if coverage.stubbed == 0 || coverage.compared == 0 {
				test.Errorf("the differential stubbed %d functions of lower and compared %d answers, want some of "+
					"each, or it holds nothing", coverage.stubbed, coverage.compared)
			}
			test.Logf("%s: %d functions of lower stubbed, %d answers the same", kind.kind.Name, coverage.stubbed,
				coverage.compared)
		})
	}
}
