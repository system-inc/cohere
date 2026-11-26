package odd_slice_argument_test

import (
	"cmp"
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules/odd_slice_argument"
)

// Over staticcheck's fixture and the port's own, the marks are the definition's however they are asked (#8d9r2rh): the
// resolver's answer about every parameter in reach, each package's own and strings' among them, and the package
// computed whole for the store. In none of these fixtures does the push's
// order leave out a mark the definition makes: demand/cycle's cycle is one the push enters, as the package declares
// it, where every mark is found. So the push marks exactly what the definition does.
func TestTheMarksAreTheDefinitionsOnTheFixtures(test *testing.T) {
	test.Parallel()
	found := holdToDefinition(test, analysistest.TestData(), []string{"CheckEvenSliceLength", "parity",
		"parity/pairs", "differences", "statements", "demand/cycle"}, inTheOrderGiven)
	requireDefined(test, found, map[string]bool{
		"strings.NewReplacer oldnew":           true,
		"CheckEvenSliceLength.fnVariadic s":    false,
		"CheckEvenSliceLength.fnVariadic args": true,
		"CheckEvenSliceLength.fnSlice args":    true,
		"CheckEvenSliceLength.fnIndirect args": true,
		"CheckEvenSliceLength.fn2 bleh":        true,
		"CheckEvenSliceLength.fn2 arr1":        false,
		"parity.notEqualZero values":           true,
		"parity.conditional values":            false,
		"parity.returning values":              false,
		"parity.byThree values":                false,
		"parity.notALength values":             false,
		"parity.aCapacity values":              false,
		"(parity.logger).pairs values":         true,
		"parity.forward values":                true,
		"parity.forwardTwice values":           true,
		"parity.forwardSometimes values":       false,
		"parity.forwardMethod values":          true,
		"parity.calls values":                  true,
		"parity.calls words":                   true,
		"parity.genericArraySink values":       false,
		"parity/pairs.Pairs values":            true,
		"parity/pairs.Forward values":          true,
		"parity/pairs.Unchecked values":        false,
		"differences.needsPairs values":        true,
		"statements.needsPairs values":         true,
		"demand/cycle.Pairs pairs":             true,
		"demand/cycle.Outer values":            true,
		"demand/cycle.Inner values":            true,
	})
	requireDepartures(test, found, nil)
}

// One cycle of calls is marked the same in either declaration order (#8d9r2rh): in order/forward and order/back, a
// slice passed to back, forward or pairs reaches the length check, so the definition holds all three marked in both,
// and so must the rule. The push, the tests' record of staticcheck's walk, departs in
// order/forward alone, on back's slice, which proves the two packages differ where a walk's order bites.
func TestACyclesMarksAreTheDefinitionsInEitherOrder(test *testing.T) {
	test.Parallel()
	found := holdToDefinition(test, analysistest.TestData(), []string{"order/forward", "order/back"},
		inTheOrderGiven)
	requireDefined(test, found, map[string]bool{
		"order/forward.back values":    true,
		"order/forward.forward values": true,
		"order/forward.pairs values":   true,
		"order/back.back values":       true,
		"order/back.forward values":    true,
		"order/back.pairs values":      true,
	})
	requireDepartures(test, found, []string{"order/forward.back values"})
}

// A parameter passed through a stand-in is marked the same wherever its package declares what the stand-in stands for
// (#8d9r2rh): in standin/first and standin/last, with no cycle of calls anywhere, each caller's slice reaches the
// length check through an instantiation, a bound method or a thunk, so the definition holds every slice marked in
// both, and so must the rule. The push departs in standin/first alone, on all three
// callers, which proves each kind of stand-in bites its order.
func TestAStandInsMarksAreTheDefinitionsInEitherOrder(test *testing.T) {
	test.Parallel()
	found := holdToDefinition(test, analysistest.TestData(), []string{"standin/first", "standin/last"},
		inTheOrderGiven)
	want := map[string]bool{}
	for _, variant := range []string{"standin/first", "standin/last"} {
		for _, name := range []string{".throughInstance", ".throughMethodValue", ".throughMethodExpression",
			".generic", ".pairs"} {
			want[variant+name+" values"] = true
		}
		want["("+variant+".holder).method values"] = true
	}
	requireDefined(test, found, want)
	requireDepartures(test, found, []string{
		"standin/first.throughInstance values",
		"standin/first.throughMethodExpression values",
		"standin/first.throughMethodValue values",
	})
}

// A method of a generic type is marked through an instance of the type (#8d9r2rh): in standin/generic each caller
// passes its slice to the method of holder[string], by a call, a method value or a method expression, and the method
// checks it, so the definition holds every slice marked, and so must the rule. The function an instance's call
// reaches stands for the generic method by its origin, and the parameter a pass names is the origin's, of type
// []Element: a walk that read the instance's own object would find no function of the package, and a pass that read
// the instance's parameter, of type []string, would find no mark, and either would leave the callers unmarked. The
// push, reading a callee's parameter off the instance's own signature as staticcheck does, departs on all three.
func TestAGenericTypesMethodIsMarkedThroughAnInstance(test *testing.T) {
	test.Parallel()
	found := holdToDefinition(test, analysistest.TestData(), []string{"standin/generic"}, inTheOrderGiven)
	requireDefined(test, found, map[string]bool{
		"standin/generic.throughInstance values":          true,
		"standin/generic.throughMethodValue values":       true,
		"standin/generic.throughMethodExpression values":  true,
		"(standin/generic.holder[Element]).method values": true,
	})
	requireDepartures(test, found, []string{
		"standin/generic.throughInstance values",
		"standin/generic.throughMethodExpression values",
		"standin/generic.throughMethodValue values",
	})
}

// A mark that must go around a cycle more than once is the definition's (#8d9r2rh): in around every slice of swapped,
// first and second reaches pairs's check, some only a call or two on, so the definition holds all six marked, and so
// must the rule. The push, visiting each function once, departs on four, the three the fixture passes a slice of one
// to and second's left, which it would mark only were second declared before first.
func TestAMarkGoneAroundACycleIsTheDefinitions(test *testing.T) {
	test.Parallel()
	found := holdToDefinition(test, analysistest.TestData(), []string{"around"}, inTheOrderGiven)
	requireDefined(test, found, map[string]bool{
		"around.pairs values":  true,
		"around.swapped left":  true,
		"around.swapped right": true,
		"around.first left":    true,
		"around.first right":   true,
		"around.second left":   true,
		"around.second right":  true,
	})
	requireDepartures(test, found, []string{
		"around.first right",
		"around.second left",
		"around.second right",
		"around.swapped right",
	})
}

// held is what holding packages' marks to the definition found.
type held struct {
	// defined is the definition's mark on each parameter of each function and method in a root's reach, by the
	// function's full name and the parameter's name.
	defined map[string]bool

	// differences is every place the rule's marks part from the definition's, and every parameter the push marks that
	// the definition does not.
	differences []string

	// departures is each parameter the definition holds marked that the push's order leaves unmarked, named as
	// defined names it.
	departures []string

	// compared counts the answers held to the definition.
	compared int
}

// heldPackage is what the probe found on one root: what it held there, every parameter of the root the definition
// holds marked, and each parameter as a failure names it.
type heldPackage struct {
	held
	marked map[types.Object]bool
	names  map[types.Object]string
}

// askOrder puts the parameters a probe asks the resolver of on one root, named by its path, in the order it asks them
// in, which is the order the resolver's walk of each package is entered in.
type askOrder func(root string, parameters []*types.Var)

// inTheOrderGiven asks in the order the probe lists the parameters in: each function in reach by full name, then the
// root's function literals' by position.
func inTheOrderGiven(string, []*types.Var) {
	// Nothing to do: the probe's own order is the order asked for.
}

// holdToDefinition loads the packages patterns name from directory, laid out by import path, and holds the rule's
// marks there to the definition's oracle (odd_slice_argument.DefinitionAnalyzer), failing the test wherever they part
// from it:
//
//   - the resolver's answer about every parameter in each root's reach, asked as that root, in order's order, which
//     for the root's own parameters is the very question the rule asks for a finding there. Nothing asks before the
//     probe, the rule's own pass included, which would have judged the package in its functions' order first, so
//     the first question of each function not yet final enters a walk there, and a shuffled order enters the
//     package's walks where it puts them;
//   - each root computed whole, as the run computes a package for the store, once every question is answered.
//
// No answer may come of replaying a package's walk, since no mark depends on an order. The push runs beside them as
// the record of staticcheck's walk. It marks nothing the definition does not, which fails the test too, and each
// parameter the definition marks beyond it is a departure, returned and never a failure.
func holdToDefinition(test *testing.T, directory string, patterns []string, order askOrder) held {
	test.Helper()
	push := odd_slice_argument.NewPushAnalyzer()
	probe := &analysis.Analyzer{
		Name:       "definition_probe",
		Doc:        "holds the marks of the package, however the rule computes them, to the definition's",
		Requires:   []*analysis.Analyzer{odd_slice_argument.MarksAnalyzer, push, odd_slice_argument.DefinitionAnalyzer},
		ResultType: reflect.TypeFor[*heldPackage](),
		Run: func(pass *analysis.Pass) (any, error) {
			return holdPackage(pass, push, order)
		},
	}
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("cohere-go's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, loadFixtures(test, directory, patterns),
		options)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	all := held{defined: map[string]bool{}}
	for _, action := range graph.Roots {
		found, isFound := action.Result.(*heldPackage)
		if action.Err != nil || !isFound {
			test.Fatalf("holding %s to the definition: %v", action.Package.ID, action.Err)
		}
		home := resolver.Package(action.Package.Types)
		if home == nil {
			test.Fatalf("the resolver never learned %s", action.Package.ID)
		}
		// Whole last, with no pass running, as the run computes a package for the store once its analysis has ended.
		whole := odd_slice_argument.Whole(resolver, home)
		for _, parameter := range parametersOfPackage(action.Package.Types, whole, found.marked) {
			found.compared++
			if whole[parameter] != found.marked[parameter] {
				found.differences = append(found.differences, fmt.Sprintf("in %s, %s: computed whole it is marked "+
					"%t, the definition %t", action.Package.ID, found.name(action.Package.Fset, parameter),
					whole[parameter], found.marked[parameter]))
			}
		}
		maps.Copy(all.defined, found.defined)
		all.differences = append(all.differences, found.differences...)
		all.departures = append(all.departures, found.departures...)
		all.compared += found.compared
	}
	for _, difference := range all.differences {
		test.Error(difference)
	}
	if all.compared == 0 {
		test.Fatalf("no parameter of %v was held to the definition, so the comparison proves nothing", patterns)
	}
	if fallbacks := resolver.Counts(odd_slice_argument.Kind).Fallbacks.Load(); fallbacks != 0 {
		test.Errorf("the marks fell back %d times over %v, want none", fallbacks, patterns)
	}
	slices.Sort(all.departures)
	test.Logf("%d answers over %d packages held to the definition, which the push's order departs from on %d",
		all.compared, len(graph.Roots), len(all.departures))
	return all
}

// holdPackage holds the marks of pass's package to the definition's: the resolver's answer about every parameter in
// the package's reach, asked as the package in order's order.
func holdPackage(pass *analysis.Pass, push *analysis.Analyzer, order askOrder) (*heldPackage, error) {
	resolver, resolverError := fact_resolver.Of(pass)
	if resolverError != nil {
		return nil, resolverError
	}
	reader := resolver.Package(pass.Pkg)
	if reader == nil {
		return nil, fmt.Errorf("the resolver never learned %s", pass.Pkg.Path())
	}
	defined, isDefined := odd_slice_argument.Definition(pass)
	if !isDefined {
		return nil, fmt.Errorf("the definition's result is a %T", pass.ResultOf[odd_slice_argument.DefinitionAnalyzer])
	}
	found := &heldPackage{
		held:   held{defined: map[string]bool{}},
		marked: map[types.Object]bool{},
		names:  map[types.Object]string{},
	}
	for parameter := range defined {
		if parameter.Pkg() == pass.Pkg {
			found.marked[parameter] = true
		}
	}
	// Every parameter of every function in reach, then the root's others, a function literal's among them.
	var parameters []*types.Var
	for _, function := range functionsInReach(pass.Pkg) {
		for parameter := range function.Signature().Params().Variables() {
			found.names[parameter] = function.FullName() + " " + parameter.Name()
			found.defined[found.names[parameter]] = defined[parameter]
			parameters = append(parameters, parameter)
		}
	}
	for _, parameter := range parametersOf(reader) {
		if _, named := found.names[parameter]; !named {
			parameters = append(parameters, parameter)
		}
	}
	order(pass.Pkg.Path(), parameters)
	for _, parameter := range parameters {
		pushed, isPushed := odd_slice_argument.PushMarked(pass, push, parameter)
		if !isPushed {
			return nil, fmt.Errorf("the push's result is a %T", pass.ResultOf[push])
		}
		name := found.name(pass.Fset, parameter)
		found.compared++
		if answer := odd_slice_argument.Ask(resolver, reader, parameter); answer != defined[parameter] {
			found.differences = append(found.differences, fmt.Sprintf("in %s, %s: the resolver answers marked %t, "+
				"the definition %t", pass.Pkg.Path(), name, answer, defined[parameter]))
		}
		if pushed && !defined[parameter] {
			found.differences = append(found.differences, fmt.Sprintf("in %s, %s: the push marks it and the "+
				"definition does not", pass.Pkg.Path(), name))
		}
		if defined[parameter] && !pushed {
			found.departures = append(found.departures, name)
		}
	}
	return found, nil
}

// name is parameter as a failure names it: its function's full name and its own, or its own and its position for one
// of no function in reach, as a function literal's.
func (found *heldPackage) name(fileSet *token.FileSet, parameter types.Object) string {
	if name, named := found.names[parameter]; named {
		return name
	}
	return fmt.Sprintf("%s at %s", parameter.Name(), fileSet.Position(parameter.Pos()))
}

// parametersOfPackage is every parameter of packageTypes that either set marks, each once, in the order of their
// positions, so two sets of marks are compared both ways and their failures read in the same order every run.
func parametersOfPackage(packageTypes *types.Package, sets ...map[types.Object]bool) []types.Object {
	var parameters []types.Object
	listed := map[types.Object]bool{}
	for _, marks := range sets {
		for parameter, marked := range marks {
			if marked && !listed[parameter] && parameter.Pkg() == packageTypes {
				listed[parameter] = true
				parameters = append(parameters, parameter)
			}
		}
	}
	slices.SortFunc(parameters, func(left, right types.Object) int { return cmp.Compare(left.Pos(), right.Pos()) })
	return parameters
}

// loadFixtures loads the packages patterns name from directory, a tree laid out by import path, and returns its roots.
// A package that does not load clean fails the test, since the marks of a package with errors prove nothing.
func loadFixtures(test *testing.T, directory string, patterns []string) []*packages.Package {
	test.Helper()
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	for _, diagnostic := range loaded.Diagnostics {
		test.Errorf("loading %v: %s:%d:%d: %s", patterns, diagnostic.File, diagnostic.Line, diagnostic.Column,
			diagnostic.Message)
	}
	if len(loaded.Diagnostics) != 0 {
		test.FailNow()
	}
	return loaded.Roots
}

// requireDefined fails on each parameter of want the definition marks otherwise than want, or that no root reached.
func requireDefined(test *testing.T, found held, want map[string]bool) {
	test.Helper()
	for _, name := range slices.Sorted(maps.Keys(want)) {
		marked, reached := found.defined[name]
		if !reached {
			test.Errorf("%s was never reached", name)
			continue
		}
		if marked != want[name] {
			test.Errorf("%s: the definition marks it %t, want %t", name, marked, want[name])
		}
	}
}

// requireDepartures fails unless the parameters the definition marks that the push's order leaves unmarked are want,
// exactly.
func requireDepartures(test *testing.T, found held, want []string) {
	test.Helper()
	if !slices.Equal(found.departures, slices.Sorted(slices.Values(want))) {
		test.Errorf("the push departs on %q, want %q", found.departures, want)
	}
}

// parametersOf is every named parameter of every function and function literal in home's syntax, receivers left out.
func parametersOf(home *fact_resolver.Package) []*types.Var {
	var parameters []*types.Var
	for _, defined := range home.Info().Defs {
		parameter, isParameter := defined.(*types.Var)
		if isParameter && !parameter.IsField() && parameter.Kind() == types.ParamVar {
			parameters = append(parameters, parameter)
		}
	}
	slices.SortFunc(parameters, func(left, right *types.Var) int { return cmp.Compare(left.Pos(), right.Pos()) })
	return parameters
}

// functionsInReach is every function and method of root and of every package below it, in the order of their full
// names.
func functionsInReach(root *types.Package) []*types.Func {
	var functions []*types.Func
	seen := map[*types.Package]bool{}
	var visit func(reached *types.Package)
	visit = func(reached *types.Package) {
		if seen[reached] {
			return
		}
		seen[reached] = true
		for _, imported := range reached.Imports() {
			visit(imported)
		}
		scope := reached.Scope()
		for _, name := range scope.Names() {
			switch object := scope.Lookup(name).(type) {
			case *types.Func:
				functions = append(functions, object)
			case *types.TypeName:
				if named, isNamed := object.Type().(*types.Named); isNamed {
					functions = append(functions, slices.Collect(named.Methods())...)
				}
			}
		}
	}
	visit(root)
	slices.SortFunc(functions, func(left, right *types.Func) int {
		return cmp.Compare(left.FullName(), right.FullName())
	})
	return functions
}
