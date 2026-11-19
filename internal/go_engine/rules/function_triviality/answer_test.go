package function_triviality_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_triviality"
)

// heardAnswers is a hand on a run's answers that answers nothing and keeps every triviality answer the run computed,
// as the resolver encoded it, by the function's full name.
type heardAnswers struct {
	lock    sync.Mutex
	encoded map[string][]byte
}

func (heard *heardAnswers) Answer(string, types.Object) ([]byte, bool) {
	return nil, false
}

func (heard *heardAnswers) Computed(kindName string, object types.Object, encoded []byte) {
	function, isFunction := object.(*types.Func)
	if kindName != function_triviality.Kind.Name || !isFunction {
		return
	}
	heard.lock.Lock()
	defer heard.lock.Unlock()
	heard.encoded[function.FullName()] = encoded
}

// askedRun is one run over a fixture: its roots, its resolver, every answer its roots' own readers were given, by the
// function's full name, and every answer the resolver computed, as it encoded it.
type askedRun struct {
	roots    []*packages.Package
	resolver *fact_resolver.Resolver
	answers  map[string]function_triviality.Triviality
	encoded  map[string][]byte
}

// askRoots loads patterns from the fixtures and runs a reader on each root that asks the triviality of every function
// the root declares, as the root's own reader, with the root built for its readers as a rule's is.
func askRoots(test *testing.T, patterns ...string) askedRun {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v found no package", patterns)
	}
	probe := &analysis.Analyzer{
		Name: "triviality_probe",
		Doc:  "asks the triviality of every function the package declares, as the package's own reader",
		Requires: []*analysis.Analyzer{function_triviality.Analyzer,
			build_intermediate_representation.Analyzer},
		ResultType: reflect.TypeFor[map[string]function_triviality.Triviality](),
		Run: func(pass *analysis.Pass) (any, error) {
			resolver, resolverError := fact_resolver.Of(pass)
			if resolverError != nil {
				return nil, resolverError
			}
			answers := map[string]function_triviality.Triviality{}
			for _, function := range declaredFunctions(pass.Pkg) {
				answers[function.FullName()] = function_triviality.Ask(resolver, resolver.Package(pass.Pkg), function)
			}
			return answers, nil
		},
	}
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	heard := &heardAnswers{encoded: map[string][]byte{}}
	resolver.HoldAnswers(heard)
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	ran := askedRun{roots: loaded.Roots, resolver: resolver, answers: map[string]function_triviality.Triviality{},
		encoded: heard.encoded}
	for _, action := range graph.Roots {
		answers, isAnswers := action.Result.(map[string]function_triviality.Triviality)
		if action.Err != nil || !isAnswers {
			test.Fatalf("asking in %s: %v", action.Package.ID, action.Err)
		}
		for name, answer := range answers {
			ran.answers[name] = answer
		}
	}
	return ran
}

// declaredFunctions are the functions declared at the package's level, sorted by name.
func declaredFunctions(declaring *types.Package) []*types.Func {
	var functions []*types.Func
	for _, name := range declaring.Scope().Names() {
		if function, isFunction := declaring.Scope().Lookup(name).(*types.Func); isFunction {
			functions = append(functions, function)
		}
	}
	return functions
}

// placesOf are where the functions of analyzed named names are declared, as an answer names a declaration: the file
// as the file set names it and the offset of the declaration's first byte, sorted by file and offset.
func placesOf(test *testing.T, analyzed *packages.Package, names ...string) []function_triviality.DeclarationPlace {
	test.Helper()
	var places []function_triviality.DeclarationPlace
	for _, name := range names {
		found := false
		for _, file := range analyzed.Syntax {
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction || function.Name.Name != name {
					continue
				}
				position := analyzed.Fset.Position(function.Pos())
				places = append(places, function_triviality.DeclarationPlace{File: position.Filename,
					Offset: position.Offset})
				found = true
			}
		}
		if !found {
			test.Fatalf("%s declares no function %s", analyzed.ID, name)
		}
	}
	slices.SortFunc(places, func(left function_triviality.DeclarationPlace,
		right function_triviality.DeclarationPlace) int {
		if left.File != right.File {
			return compareText(left.File, right.File)
		}
		return left.Offset - right.Offset
	})
	return places
}

// compareText orders two texts as the answer's sort does.
func compareText(left string, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	}
	return 0
}

// An answer is IsTrivial's bit and the place of every other top-level declaration whose body the walk read, each
// once, sorted, and of none it refused at once (#h0k644s). Chained reaches third, in another file, through second,
// whose function literal is named by second's own declaration, past leaf, met twice. Abroad ends at made, which calls
// another package's function, so the third it would have called next is not read and not named. RelayedTwice meets
// relay, which calls, a second time, and that meeting is refused. Enclosing's own function literal is its own
// declaration's, so it names nothing, and Instantiated names generic once for its instance and for generic itself.
func TestAnAnswerNamesEveryDeclarationTheWalkRead(test *testing.T) {
	test.Parallel()
	ran := askRoots(test, "entering")
	for _, asked := range []struct {
		name    string
		trivial bool
		entered []string
	}{
		{"Chained", true, []string{"leaf", "second", "third"}},
		{"Abroad", false, []string{"leaf", "made"}},
		{"RelayedTwice", false, []string{"leaf", "relay"}},
		{"Alone", true, nil},
		{"Enclosing", true, nil},
		{"Instantiated", true, []string{"generic"}},
	} {
		answer, isAnswered := ran.answers["entering."+asked.name]
		if !isAnswered {
			test.Errorf("%s was never asked", asked.name)
			continue
		}
		want := function_triviality.Triviality{Trivial: asked.trivial,
			Entered: placesOf(test, ran.roots[0], asked.entered...)}
		if !reflect.DeepEqual(answer, want) {
			test.Errorf("%s: the answer is %+v, want %+v", asked.name, answer, want)
		}
	}
	// third is in a file of its own, so Chained's places name two files.
	chained := ran.answers["entering.Chained"]
	if len(chained.Entered) == 3 && chained.Entered[0].File == chained.Entered[2].File {
		test.Errorf("Chained names one file, %s, for declarations in two", chained.Entered[0].File)
	}
}

// A function literal is named by the top-level declaration that encloses it, however deep it is. Asked is trivial
// through twice, whose function literal, called in place, calls another in place, which alone calls leaf: the answer
// names twice once, for itself and both literals, and leaf, which shows the walk entered the inner literal. twice's
// own answer names only leaf, both literals being its own declaration's.
func TestAnAnswerNamesTheDeclarationEnclosingALiteralInsideALiteral(test *testing.T) {
	test.Parallel()
	ran := askRoots(test, "nested")
	for _, asked := range []struct {
		name    string
		entered []string
	}{
		{"Asked", []string{"twice", "leaf"}},
		{"twice", []string{"leaf"}},
	} {
		want := function_triviality.Triviality{Trivial: true, Entered: placesOf(test, ran.roots[0], asked.entered...)}
		if answer := ran.answers["nested."+asked.name]; !reflect.DeepEqual(answer, want) {
			test.Errorf("%s: the answer is %+v, want %+v", asked.name, answer, want)
		}
	}
}

// A declaration under a //line directive is named as the engine keys a unit: by the file the directive names, as
// token.Position renders it, beside the file that holds the directive, and by the offset of the declaration's first
// byte in the file as parsed, which no directive moves. The want is built from the fixture's own bytes, by no file
// set.
func TestAnAnswerNamesAFunctionUnderALineDirectiveByTheFileTheDirectiveNames(test *testing.T) {
	test.Parallel()
	ran := askRoots(test, "directive")
	directory := filepath.Join(analysistest.TestData(), "src", "directive")
	parsed, readError := os.ReadFile(filepath.Join(directory, "directive.go"))
	if readError != nil {
		test.Fatalf("reading the fixture: %v", readError)
	}
	offset := strings.Index(string(parsed), "func moved")
	if offset < 0 || !strings.Contains(string(parsed), "\n//line other.go:10\nfunc moved") {
		test.Fatal("the fixture no longer declares moved right under its //line directive")
	}
	want := function_triviality.Triviality{Trivial: true, Entered: []function_triviality.DeclarationPlace{
		{File: filepath.Join(directory, "other.go"), Offset: offset},
	}}
	if answer := ran.answers["directive.Asked"]; !reflect.DeepEqual(answer, want) {
		test.Errorf("Asked's answer is %+v, want %+v", answer, want)
	}
}

// An answer goes to JSON and back without loss, as the resolver keeps it: every answer the run computed decodes to the
// answer the reader was given, and encodes again to the same bytes.
func TestAnAnswerEncodesAndDecodesWithoutLoss(test *testing.T) {
	test.Parallel()
	ran := askRoots(test, "entering")
	if len(ran.encoded) != len(ran.answers) || len(ran.encoded) == 0 {
		test.Fatalf("the run computed %d answers for %d questions", len(ran.encoded), len(ran.answers))
	}
	for name, encoded := range ran.encoded {
		var decoded function_triviality.Triviality
		if decodeError := json.Unmarshal(encoded, &decoded); decodeError != nil {
			test.Fatalf("%s: decoding %s: %v", name, encoded, decodeError)
		}
		if !reflect.DeepEqual(decoded, ran.answers[name]) {
			test.Errorf("%s: %s decodes to %+v, where the reader was given %+v", name, encoded, decoded,
				ran.answers[name])
		}
		again, encodeError := json.Marshal(decoded)
		if encodeError != nil || string(again) != string(encoded) {
			test.Errorf("%s: %s encodes again to %s (%v)", name, encoded, again, encodeError)
		}
	}
	const chained = `{"Trivial":true,"Entered":[{"File":"%[1]s/entering.go","Offset":%[2]d},` +
		`{"File":"%[1]s/entering.go","Offset":%[3]d},{"File":"%[1]s/third.go","Offset":%[4]d}]}`
	places := placesOf(test, ran.roots[0], "second", "leaf", "third")
	directory := analysistest.TestData() + "/src/entering"
	want := fmt.Sprintf(chained, directory, places[0].Offset, places[1].Offset, places[2].Offset)
	if got := string(ran.encoded["entering.Chained"]); got != want {
		test.Errorf("Chained's answer is kept as %s, want %s", got, want)
	}
}

// An answer is the one a reader's own walk gives, whichever program its package builds in. As a root, near is built
// for its readers, far made from its types alone, and Crossing's walk ends at far.Nothing, which has no syntax there.
// Below above, near builds in the resolver's own program, where far has its syntax, and the walk refuses far.Nothing
// all the same, since another package declares it: the answer is the root's, and far.Nothing is never built. Its body
// calls deep.Value, of a package near does not import and the resolver's program never created for near, so a build
// of it panics.
func TestAnAnswerIsTheSameWhereOtherPackagesHaveTheirSyntax(test *testing.T) {
	test.Parallel()
	root := askRoots(test, "cut/near")
	want := function_triviality.Triviality{Entered: placesOf(test, root.roots[0], "leaf")}
	if answer := root.answers["cut/near.Crossing"]; !reflect.DeepEqual(answer, want) {
		test.Fatalf("as a root, Crossing's answer is %+v, want %+v", answer, want)
	}
	below := askRoots(test, "cut/above")
	near := below.roots[0].Imports["cut/near"]
	if near == nil || near.Types == nil {
		test.Fatal("above imports no near")
	}
	home := below.resolver.Package(near.Types)
	crossing, isFunction := near.Types.Scope().Lookup("Crossing").(*types.Func)
	if home == nil || !isFunction {
		test.Fatal("the resolver never learned near, or near declares no Crossing")
	}
	// The files are the same on disk, so the places are the root's.
	if answer := function_triviality.Ask(below.resolver, home, crossing); !reflect.DeepEqual(answer, want) {
		test.Errorf("below above, Crossing's answer is %+v, want the root's, %+v", answer, want)
	}
	// Refused, not survived: far.Nothing has its syntax in the program near was judged in, and no blocks.
	far := near.Imports["cut/far"]
	if far == nil || far.Types == nil {
		test.Fatal("near imports no far")
	}
	nothing, isFunction := far.Types.Scope().Lookup("Nothing").(*types.Func)
	if !isFunction {
		test.Fatal("far declares no Nothing")
	}
	built := home.IntermediateRepresentation().Program.FunctionValue(nothing)
	if built == nil || built.Syntax() == nil {
		test.Fatalf("far.Nothing is %v in the program near was judged in, want a function with its syntax", built)
	}
	if built.Blocks != nil {
		test.Error("the walk built far.Nothing, a function another package declares, which it must refuse unbuilt")
	}
}

// The answer about a function declared without a body, from its types alone, is the one computed when its package is
// loaded: trivial, naming nothing, since it has no instruction. Calls, which calls it, is trivial through it and names
// it.
func TestABodilessFunctionAnswersAsItsTypesDo(test *testing.T) {
	test.Parallel()
	ran := askRoots(test, "bodiless")
	assembled, isFunction := ran.roots[0].Types.Scope().Lookup("Assembled").(*types.Func)
	if !isFunction {
		test.Fatal("bodiless declares no Assembled")
	}
	fromTypes := function_triviality.BodilessTriviality(assembled)
	if computed := ran.answers["bodiless.Assembled"]; !reflect.DeepEqual(computed, fromTypes) {
		test.Errorf("Assembled's answer is %+v computed and %+v from its types", computed, fromTypes)
	}
	want := function_triviality.Triviality{Trivial: true, Entered: placesOf(test, ran.roots[0], "Assembled")}
	if calls := ran.answers["bodiless.Calls"]; !reflect.DeepEqual(calls, want) {
		test.Errorf("Calls's answer is %+v, want %+v", calls, want)
	}
}

// A reader asks only of its own package's functions: asked of another package's, the question panics, naming both,
// and computes nothing.
func TestAQuestionAboutAnotherPackagesFunctionPanics(test *testing.T) {
	test.Parallel()
	ran := askRoots(test, "cut/above")
	near := ran.roots[0].Imports["cut/near"]
	crossing, isFunction := near.Types.Scope().Lookup("Crossing").(*types.Func)
	if !isFunction {
		test.Fatal("near declares no Crossing")
	}
	computed := ran.resolver.Counts(function_triviality.Kind).Computed.Load()
	var panicked string
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				panicked = fmt.Sprint(recovered)
			}
		}()
		function_triviality.Ask(ran.resolver, ran.resolver.Package(ran.roots[0].Types), crossing)
	}()
	const want = "function_triviality: cut/above asked of cut/near.Crossing, a function of another package"
	if panicked != want {
		test.Errorf("asking above of near.Crossing panicked with %q, want %q", panicked, want)
	}
	if after := ran.resolver.Counts(function_triviality.Kind).Computed.Load(); after != computed {
		test.Errorf("the refused question judged %d functions", after-computed)
	}
}

// A function of the package that the package's program holds no function for, as an interface's method, has no body
// to walk, and is not trivial, as the walk takes a call with no static callee: the answer is the zero one, naming
// nothing, and judges no function. It is not a function declared without a body, which is built from its declaration
// and trivial (TestABodilessFunctionAnswersAsItsTypesDo).
func TestAFunctionWithNoFunctionInTheProgramIsNotTrivial(test *testing.T) {
	test.Parallel()
	ran := askRoots(test, "abstract")
	stopper, isType := ran.roots[0].Types.Scope().Lookup("Stopper").(*types.TypeName)
	if !isType {
		test.Fatal("abstract declares no Stopper")
	}
	declared, isInterface := stopper.Type().Underlying().(*types.Interface)
	if !isInterface || declared.NumExplicitMethods() != 1 {
		test.Fatal("Stopper is no interface of one method")
	}
	computed := ran.resolver.Counts(function_triviality.Kind).Computed.Load()
	stop := declared.ExplicitMethod(0)
	answer := function_triviality.Ask(ran.resolver, ran.resolver.Package(ran.roots[0].Types), stop)
	if !reflect.DeepEqual(answer, function_triviality.Triviality{}) {
		test.Errorf("%s's answer is %+v, want not trivial, naming nothing", stop.FullName(), answer)
	}
	if after := ran.resolver.Counts(function_triviality.Kind).Computed.Load(); after != computed {
		test.Errorf("the question about %s judged %d functions, want none", stop.FullName(), after-computed)
	}
}
