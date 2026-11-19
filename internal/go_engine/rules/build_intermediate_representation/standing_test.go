package build_intermediate_representation

import (
	"go/ast"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// plantedStands is a rule.StandingBodies in which every function declaration stands, or none does, and which confines
// no rule and holds no findings: all Analyzer reads of one is whether a function of the package stands.
type plantedStands struct {
	stands bool
}

// Stands is whether every declaration stands.
func (planted plantedStands) Stands(string, int) bool {
	return planted.stands
}

// Confines is false, for every rule.
func (plantedStands) Confines(string) bool {
	return false
}

// Findings is none held, for every rule and file.
func (plantedStands) Findings(string, string) ([]rule.InheritedFinding, bool) {
	return nil, false
}

// Took records nothing, since no rule takes anything of a plantedStands.
func (plantedStands) Took(string, string, int, int) {
	// No rule is confined, so none tells it anything.
}

// standingOptions is the checker's options that hand every pass a plantedStands in which every declaration stands, or
// none does.
func standingOptions(stands bool) *checker.Options {
	return &checker.Options{
		StandingBodies: func(string, any) rule.StandingBodies { return plantedStands{stands: stands} },
		Standing:       rule.Standing,
	}
}

// unbuiltAmong is the name of each of functions, and of the anonymous functions they hold, that has no blocks.
func unbuiltAmong(functions []*intermediate_representation.Function) []string {
	unbuilt := []string{}
	for _, function := range withAnonymous(functions) {
		if len(function.Blocks) == 0 {
			unbuilt = append(unbuilt, function.RelativeString(nil))
		}
	}
	return unbuilt
}

// In a pass where a function of the package stands, an extra build's, Analyzer creates the package and builds none of
// it, the synthetic initializer included, on both of run's paths, and a reader of the whole package finds it built
// whole all the same: SourceFunctions and Package each build every function, anonymous ones included, before they
// answer. A pass that holds standing bodies in which nothing of the package stands is built by Analyzer's own action,
// as a pass that holds none is (TestEveryFunctionIsBuiltBeforeAReaderStarts).
func TestAPackageWhereAFunctionStandsIsBuiltByItsFirstWholeReader(test *testing.T) {
	test.Parallel()
	type reading struct {
		// declared is how many declared functions the package holds, and builtAsStarted how many of them had blocks
		// as the reader started, before it read anything through the result.
		declared       int
		builtAsStarted int
		// functions is how many functions the package holds once read whole, anonymous ones included, and unbuilt
		// those of them with no blocks then.
		functions int
		unbuilt   []string
	}
	probe := func(name string, readWhole func(result *Result)) *analysis.Analyzer {
		return &analysis.Analyzer{
			Name:       name,
			Doc:        "counts the functions built as the reader started and once it read the whole package",
			Requires:   []*analysis.Analyzer{Analyzer},
			ResultType: reflect.TypeFor[reading](),
			Run: func(pass *analysis.Pass) (any, error) {
				result := mustResultOf(pass)
				// Read off the package itself, so a build the result's own methods do would not count.
				read := reading{declared: len(result.irPackage.Functions)}
				for _, function := range result.irPackage.Functions {
					if function.Blocks != nil {
						read.builtAsStarted++
					}
				}
				readWhole(result)
				read.functions = len(withAnonymous(result.irPackage.Functions))
				read.unbuilt = unbuiltAmong(result.irPackage.Functions)
				return read, nil
			},
		}
	}
	shared := standingOptions(true)
	shared.Resolver, shared.Resolved = newSharingResolver(), rule.Resolved
	for _, testCase := range []struct {
		name           string
		options        *checker.Options
		reader         *analysis.Analyzer
		builtByItsRead bool
	}{
		{
			name: "created alone, read through SourceFunctions", options: standingOptions(true),
			reader:         probe("source_functions_probe", func(result *Result) { result.SourceFunctions() }),
			builtByItsRead: true,
		},
		{
			name: "created alone, read through Package", options: standingOptions(true),
			reader:         probe("package_probe", func(result *Result) { result.Package() }),
			builtByItsRead: true,
		},
		{
			name: "shared by the resolver, read through SourceFunctions", options: shared,
			reader:         probe("shared_probe", func(result *Result) { result.SourceFunctions() }),
			builtByItsRead: true,
		},
		{
			name: "nothing stands, read through nothing", options: standingOptions(false),
			reader: probe("unread_probe", func(*Result) { /* Reads nothing, so only Analyzer's action builds. */ }),
		},
	} {
		read, isReading := rootResults(test, testCase.options, testCase.reader)[testCase.reader].(reading)
		if !isReading {
			test.Fatalf("%s, the probe returned no reading", testCase.name)
		}
		if read.declared < 8 || read.functions <= read.declared {
			test.Fatalf("%s, the fixture held %d declared functions and %d with the anonymous ones, too few to hold "+
				"the build to", testCase.name, read.declared, read.functions)
		}
		expectedAsStarted := read.declared
		if testCase.builtByItsRead {
			expectedAsStarted = 0
		}
		if read.builtAsStarted != expectedAsStarted {
			test.Errorf("%s, as the reader started %d of %d declared functions were built, want %d", testCase.name,
				read.builtAsStarted, read.declared, expectedAsStarted)
		}
		if len(read.unbuilt) != 0 {
			test.Errorf("%s, once the reader had read the package %q of %d functions had no blocks, where the whole "+
				"package was expected built", testCase.name, read.unbuilt, read.functions)
		}
	}
}

// partlyRead is what a probe of SourceFunctionsBut read: the functions it listed, by name and in order, those of them
// with a body and no blocks, how many declared functions the package holds, and of the declared functions it left,
// how many there were, those that were built and those that hold an anonymous function, read off the package itself.
type partlyRead struct {
	listed        []string
	listedUnbuilt []string
	declared      int
	left          int
	leftBuilt     []string
	leftHolding   []string
}

// partlyReading is an analyzer that reads the package through SourceFunctionsBut, leaving the declared functions leaves
// holds for, and returns a partlyRead.
func partlyReading(leaves func(declared *intermediate_representation.Function) bool) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:       "partly_reading",
		Doc:        "reads the package less the functions it leaves",
		Requires:   []*analysis.Analyzer{Analyzer},
		ResultType: reflect.TypeFor[partlyRead](),
		Run: func(pass *analysis.Pass) (any, error) {
			result := mustResultOf(pass)
			read := partlyRead{declared: len(result.irPackage.Functions)}
			for function := range result.SourceFunctionsBut(leaves) {
				read.listed = append(read.listed, function.RelativeString(nil))
				// A function declared with no body, as one written in assembly is, has no blocks however it is built.
				declaration, isDeclared := function.Syntax().(*ast.FuncDecl)
				if len(function.Blocks) == 0 && (!isDeclared || declaration.Body != nil) {
					read.listedUnbuilt = append(read.listedUnbuilt, function.RelativeString(nil))
				}
			}
			for _, function := range result.irPackage.Functions {
				if !leaves(function) {
					continue
				}
				read.left++
				if function.Blocks != nil {
					read.leftBuilt = append(read.leftBuilt, function.RelativeString(nil))
				}
				if len(function.AnonymousFunctions) > 0 {
					read.leftHolding = append(read.leftHolding, function.RelativeString(nil))
				}
			}
			return read, nil
		},
	}
}

// SourceFunctionsBut, on a package Analyzer left unbuilt, lists SourceFunctions less each declared function it is told
// to leave and the anonymous functions declared in one, in SourceFunctions' order, every one listed built, and it
// builds none of those left, whose anonymous functions are then never created. The fixture's left functions hold a
// closure with a closure inside it and a generic function's closure, and its kept ones the synthetic initializer with
// its closures.
func TestSourceFunctionsButBuildsAndListsOnlyWhatItKeeps(test *testing.T) {
	test.Parallel()
	leaves := func(declared *intermediate_representation.Function) bool {
		return declared.Name() == "increment" || declared.Name() == "mapped"
	}
	everything := &analysis.Analyzer{
		Name:       "whole_order",
		Doc:        "names every source function in order",
		Requires:   []*analysis.Analyzer{Analyzer},
		ResultType: reflect.TypeFor[[]string](),
		Run: func(pass *analysis.Pass) (any, error) {
			names := []string{}
			for function := range mustResultOf(pass).SourceFunctions() {
				names = append(names, function.RelativeString(nil))
			}
			return names, nil
		},
	}
	// Read in a checker of its own, so the whole read builds nothing in the package the partial read is held to.
	whole, isWhole := rootResults(test, &checker.Options{}, everything)[everything].([]string)
	if !isWhole {
		test.Fatal("the whole read returned no names")
	}
	expected := []string{}
	for _, name := range whole {
		if !strings.Contains(name, "increment") && !strings.Contains(name, "mapped") {
			expected = append(expected, name)
		}
	}
	reader := partlyReading(leaves)
	read, isRead := rootResults(test, standingOptions(true), reader)[reader].(partlyRead)
	if !isRead {
		test.Fatal("the partial read returned no reading")
	}
	if len(expected) < 8 || len(whole)-len(expected) < 5 || read.left != 2 {
		test.Fatalf("the fixture read as %d functions, %d of them kept, with %d declared functions left, too few to "+
			"hold the partial read to: %q", len(whole), len(expected), read.left, whole)
	}
	if !slices.Equal(read.listed, expected) {
		test.Errorf("SourceFunctionsBut listed\n%s\nwant SourceFunctions less the functions left and the anonymous "+
			"ones inside them\n%s", strings.Join(read.listed, "\n"), strings.Join(expected, "\n"))
	}
	if len(read.listedUnbuilt) != 0 {
		test.Errorf("SourceFunctionsBut listed %q with no blocks, want every function it lists built",
			read.listedUnbuilt)
	}
	if len(read.leftBuilt) != 0 || len(read.leftHolding) != 0 {
		test.Errorf("of the functions left, %q were built and %q hold an anonymous function, want none built, and "+
			"so no anonymous function created", read.leftBuilt, read.leftHolding)
	}
}

// A package with functions enough to spread over several workers is built in part as surely as a small one: over the
// standard library's math, with one function left, SourceFunctionsBut builds every other declared function and leaves
// that one, on several workers at once.
func TestSourceFunctionsButBuildsALargePackageOnSeveralWorkers(test *testing.T) {
	test.Parallel()
	if runtime.GOMAXPROCS(0) < 2 {
		test.Fatalf("GOMAXPROCS is %d, and a build spreads over at most one worker per core", runtime.GOMAXPROCS(0))
	}
	reader := partlyReading(func(declared *intermediate_representation.Function) bool {
		return declared.Name() == "Hypot"
	})
	read, isRead := rootResultsOver(test, "math", standingOptions(true),
		reader)[reader].(partlyRead)
	if !isRead {
		test.Fatal("the partial read returned no reading")
	}
	if read.declared-read.left < partBuildThreshold || read.left != 1 {
		test.Fatalf("math read as %d declared functions with %d left, where at least %d kept, enough to spread over "+
			"several workers, and one left were expected", read.declared, read.left, partBuildThreshold)
	}
	if len(read.listedUnbuilt) != 0 || len(read.leftBuilt) != 0 {
		test.Errorf("of math's functions, %q of the %d listed had no blocks and the one left was built: %q, want "+
			"every listed one built and the one left not", read.listedUnbuilt, len(read.listed), read.leftBuilt)
	}
}
