package odd_slice_argument_test

import (
	"fmt"
	"hash/crc32"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
)

// The generated graphs: how many, how many functions each holds, and how many declaration orders each is written in.
// The seed is fixed, so every run generates the same graphs in the same orders and a failure names the same package.
const (
	generatedGraphs    = 12
	generatedFunctions = 6
	generatedOrders    = 4
	generatedSeed      = 8
)

// generatedParameters names the two slices every generated function takes.
var generatedParameters = [2]string{"left", "right"}

// oddSlice is the source of a slice of one, whose length the rule knows to be odd.
const oddSlice = `[]string{"one"}`

// leafMarks is each function of the generated leaf package, by name, with whether its one slice is marked there.
var leafMarks = map[string]bool{"Pairs": true, "Forward": true, "Loose": false}

// leafSource is the package every generated one imports, so a call can pass a slice across a package.
const leafSource = `// Package leaf is what the generated graphs call in another package.
package leaf

// Pairs panics unless its values come in pairs.
func Pairs(values []string) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// Forward passes its values on to Pairs.
func Forward(values []string) {
	Pairs(values)
}

// Loose takes values of any length.
func Loose(values []string) {
	_ = len(values)
}
`

// Over generated call graphs dense with cycles, each written as a package in several declaration orders, the marks
// are the definition's wherever a judgment enters (#8d9r2rh). Every function takes two slices and passes each, the
// other or neither on to others, to itself, and to another package's functions, on every path or only sometimes, as a
// declared function, a method, a method reached through a method expression's thunk, or a generic function reached
// through its instantiation wrapper, the last two being functions of no package.
//
// Three things are held together. The definition's oracle marks each package exactly as the graph it was generated
// from says, computed on the graph alone, with no source and no representation, so the oracle is itself held to
// something outside it. The resolver's answers, asked in a shuffled order, and the package computed whole are the
// definition's (holdToDefinition). And a reader package, importing each one and passing every function two slices of
// one, is reported exactly where the graph marks a parameter: there the package is a dependency the resolver judges in
// its own program, as a run judges one, with nothing but the analyzer running.
func TestTheMarksAreTheDefinitionsOnGeneratedGraphsInAnyOrder(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	variants := writeGeneratedVariants(test, directory, rand.New(rand.NewPCG(generatedSeed, 0)))
	var patterns []string
	var readerPatterns []string
	for _, variant := range variants {
		patterns = append(patterns, variant.path)
		readerPatterns = append(readerPatterns, variant.path+"/reader")
	}
	found := holdToDefinition(test, directory, patterns, func(root string, parameters []*types.Var) {
		// A package's order is its own, from its path, since the probes of two packages run at once.
		shuffle := rand.New(rand.NewPCG(generatedSeed, uint64(crc32.ChecksumIEEE([]byte(root)))))
		shuffle.Shuffle(len(parameters), func(left, right int) {
			parameters[left], parameters[right] = parameters[right], parameters[left]
		})
	})
	// The seed is fixed, so a failure names the same package every run; its source is what the name stands for.
	shown := 0
	for _, variant := range variants {
		departs := slices.ContainsFunc(found.differences, func(difference string) bool {
			return strings.HasPrefix(difference, "in "+variant.path+",")
		})
		if departs && shown < 2 {
			shown++
			test.Logf("%s is:\n%s", variant.path, variant.source)
		}
	}
	for _, variant := range variants {
		marks := variant.graph.marks()
		for index := range variant.graph.functions {
			for parameter, name := range generatedParameters {
				key := variant.graph.fullName(variant.path, index) + " " + name
				defined, reached := found.defined[key]
				if !reached {
					test.Errorf("%s was never reached", key)
					continue
				}
				if defined != marks[index][parameter] {
					test.Errorf("%s: the definition marks it %t, the graph it was generated from %t\n%s", key, defined,
						marks[index][parameter], variant.source)
				}
			}
		}
	}
	requireReadersReported(test, directory, readerPatterns, variants)
}

// requireReadersReported runs the analyzer alone over each variant's reader, so the variant is a dependency whose
// marks the resolver judges on demand in its own program, and requires a finding on exactly each slice of one passed
// to a parameter the variant's graph marks.
func requireReadersReported(test *testing.T, directory string, patterns []string, variants []generatedVariant) {
	test.Helper()
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{odd_slice_argument.Analyzer},
		loadFixtures(test, directory, patterns), options)
	if analyzeError != nil {
		test.Fatalf("analyzing the readers: %v", analyzeError)
	}
	// A reader declares no parameter, so every function the resolver judged is one of a package a reader imports.
	if judged := resolver.Counts(odd_slice_argument.Kind).Computed.Load(); judged == 0 {
		test.Fatalf("the resolver judged no function for the readers, so their findings prove nothing of it")
	}
	readers := map[string]generatedVariant{}
	for _, variant := range variants {
		readers[variant.path+"/reader"] = variant
	}
	reported := 0
	for _, action := range graph.Roots {
		variant, isReader := readers[action.Package.PkgPath]
		if action.Err != nil || !isReader {
			test.Fatalf("running on %s, a reader %t: %v", action.Package.ID, isReader, action.Err)
		}
		delete(readers, action.Package.PkgPath)
		found := map[string]bool{}
		for _, diagnostic := range action.Diagnostics {
			found[fmt.Sprintf("%d: %s", action.Package.Fset.Position(diagnostic.Pos).Line, diagnostic.Message)] = true
		}
		reported += len(found)
		marks := variant.graph.marks()
		for index := range variant.graph.functions {
			for parameter, name := range generatedParameters {
				finding := fmt.Sprintf("%d: argument %q is expected to have even number of elements, but has 1 "+
					"elements", variant.readerLines[index], name)
				if found[finding] != marks[index][parameter] {
					test.Errorf("%s %s: its reader's slice of one is reported %t, and the graph marks the parameter %t",
						variant.graph.fullName(variant.path, index), name, found[finding], marks[index][parameter])
				}
				delete(found, finding)
			}
		}
		for _, finding := range slices.Sorted(maps.Keys(found)) {
			test.Errorf("%s/reader/reader.go:%s, which no parameter of the graph accounts for", variant.path, finding)
		}
	}
	for path := range readers {
		test.Errorf("%s was never analyzed", path)
	}
	if reported == 0 {
		test.Fatalf("no reader was reported at all, so the comparison proves nothing")
	}
}

// functionKind is how a generated function is declared, which is how a call reaches it.
type functionKind int

const (
	// declaredFunction is a function called by its name.
	declaredFunction functionKind = iota

	// methodFunction is a method of the package's Holder, called on a value or, through a method expression, by a
	// thunk, a function of no package that calls the method.
	methodFunction

	// genericFunction is a generic function, called through an instantiation wrapper, a function of no package that
	// calls the generic one.
	genericFunction
)

// generatedCall is one call in a generated function's body.
type generatedCall struct {
	// callee is the function called, by its index in the graph, or -1 for leaf's function named leaf.
	callee int
	leaf   string

	// arguments is, for each parameter of the callee, which of the caller's own two slices is passed to it as it
	// is, 0 or 1, or -1 for nil. Only the first counts for a function of leaf, which takes one.
	arguments [2]int

	// sometimes puts the call in a branch, a block that dominates no return, so it passes nothing on.
	sometimes bool

	// throughExpression calls a method through a method expression, and so through its thunk.
	throughExpression bool
}

// generatedFunction is one function of a generated graph: how it is declared, which of its two slices it checks
// itself, and its calls in the order its body makes them.
type generatedFunction struct {
	kind   functionKind
	checks [2]bool
	calls  []generatedCall
}

// generatedGraph is a call graph the test generates, apart from any order its functions are declared in.
type generatedGraph struct {
	functions []generatedFunction
}

// generatedVariant is one graph written as a package in one declaration order, with its reader: the package's import
// path, its source for a failure to show, and the line of the reader's call to each function.
type generatedVariant struct {
	graph       generatedGraph
	path        string
	source      string
	readerLines []int
}

// newGeneratedGraph is a random graph: each function of a random kind, checking each slice itself one time in six,
// with one to three calls, each to a random function, itself included, or one time in eight to leaf, in a branch one
// time in five. At least one slice is checked, so some mark exists to spread.
func newGeneratedGraph(random *rand.Rand) generatedGraph {
	graph := generatedGraph{functions: make([]generatedFunction, generatedFunctions)}
	leafNames := slices.Sorted(maps.Keys(leafMarks))
	kinds := []functionKind{declaredFunction, methodFunction, genericFunction}
	for index := range graph.functions {
		function := &graph.functions[index]
		function.kind = kinds[random.IntN(len(kinds))]
		for parameter := range function.checks {
			function.checks[parameter] = random.IntN(6) == 0
		}
		for range 1 + random.IntN(3) {
			call := generatedCall{
				callee:            random.IntN(generatedFunctions),
				arguments:         [2]int{random.IntN(3) - 1, random.IntN(3) - 1},
				sometimes:         random.IntN(5) == 0,
				throughExpression: random.IntN(2) == 0,
			}
			if random.IntN(8) == 0 {
				call.callee, call.leaf = -1, leafNames[random.IntN(len(leafNames))]
			}
			function.calls = append(function.calls, call)
		}
	}
	graph.functions[random.IntN(generatedFunctions)].checks[random.IntN(2)] = true
	return graph
}

// marks is whether each function's each slice is marked by the definition, computed on the graph itself: the least set
// holding every slice a function checks and every slice a call on every path passes to a slice in the set.
func (graph generatedGraph) marks() [][2]bool {
	marked := make([][2]bool, len(graph.functions))
	for index, function := range graph.functions {
		marked[index] = function.checks
	}
	for added := true; added; {
		added = false
		for index, function := range graph.functions {
			for _, call := range function.calls {
				if call.sometimes {
					continue
				}
				for calleeParameter, passed := range call.arguments {
					if passed < 0 || marked[index][passed] {
						continue
					}
					if call.callee < 0 && (calleeParameter > 0 || !leafMarks[call.leaf]) {
						continue
					}
					if call.callee >= 0 && !marked[call.callee][calleeParameter] {
						continue
					}
					marked[index][passed] = true
					added = true
				}
			}
		}
	}
	return marked
}

// fullName is the full name of the graph's function at index, declared in the package at path, as go/types prints it.
func (graph generatedGraph) fullName(path string, index int) string {
	if graph.functions[index].kind == methodFunction {
		return fmt.Sprintf("(%s.Holder).Function%d", path, index)
	}
	return fmt.Sprintf("%s.Function%d", path, index)
}

// call is the source of a call to the graph's function at index with the two arguments given, from its own package,
// or from another when qualifier names the package, as `subject.`.
//
// A generic function takes, after its slices, an array of its package's own Holder as long as its index. An
// instantiation's signature is made canonical by type identity across the program it is built in, so two generic
// functions whose instantiations had identical signatures would share one's parameters. The rule never reads a mark
// there (declaredParameter, and testdata/src/instance holds it to that), but the push does, as staticcheck does, and
// would mark one function's parameter by another's check, which is no departure of order. The array keeps every
// instantiation's signature its own, in every package, so the push stays the record of staticcheck's order alone.
func (graph generatedGraph) call(qualifier string, index int, throughExpression bool, arguments [2]string) string {
	switch graph.functions[index].kind {
	case methodFunction:
		if throughExpression {
			return fmt.Sprintf("%sHolder.Function%d(%sHolder{}, %s, %s)", qualifier, index, qualifier, arguments[0],
				arguments[1])
		}
		return fmt.Sprintf("%sHolder{}.Function%d(%s, %s)", qualifier, index, arguments[0], arguments[1])
	case genericFunction:
		return fmt.Sprintf("%sFunction%d[int](%s, %s, [%d]%sHolder{})", qualifier, index, arguments[0], arguments[1],
			index+1, qualifier)
	default:
		return fmt.Sprintf("%sFunction%d(%s, %s)", qualifier, index, arguments[0], arguments[1])
	}
}

// source is the graph as a package named name, its functions declared in order, and after them a probe for each, a
// function that passes it two slices of one, so the package holds a finding of its own wherever the graph marks a
// parameter.
func (graph generatedGraph) source(name string, order []int) string {
	var source strings.Builder
	fmt.Fprintf(&source, "package %s\n\nimport \"graphs/leaf\"\n\n", name)
	source.WriteString("// Sometimes is what a call made only sometimes branches on.\nvar Sometimes bool\n\n")
	source.WriteString("// Holder holds the methods.\ntype Holder struct{}\n\n")
	source.WriteString("// The import is used whether or not a function calls leaf.\nvar _ = leaf.Pairs\n\n")
	for _, index := range order {
		function := graph.functions[index]
		switch function.kind {
		case methodFunction:
			fmt.Fprintf(&source, "func (Holder) Function%d(left, right []string) {\n", index)
		case genericFunction:
			fmt.Fprintf(&source, "func Function%d[Unused any](left, right []string, _ [%d]Holder) {\n", index, index+1)
		default:
			fmt.Fprintf(&source, "func Function%d(left, right []string) {\n", index)
		}
		for parameter, checked := range function.checks {
			if checked {
				fmt.Fprintf(&source, "\tif len(%s)%%2 != 0 {\n\t\tpanic(\"odd\")\n\t}\n",
					generatedParameters[parameter])
			}
		}
		for _, call := range function.calls {
			arguments := [2]string{"nil", "nil"}
			for calleeParameter, passed := range call.arguments {
				if passed >= 0 {
					arguments[calleeParameter] = generatedParameters[passed]
				}
			}
			text := fmt.Sprintf("leaf.%s(%s)", call.leaf, arguments[0])
			if call.callee >= 0 {
				text = graph.call("", call.callee, call.throughExpression, arguments)
			}
			if call.sometimes {
				text = "if Sometimes {\n\t\t" + text + "\n\t}"
			}
			fmt.Fprintf(&source, "\t%s\n", text)
		}
		source.WriteString("}\n\n")
	}
	// Declared last, so no walk in declaration order enters the graph through one.
	for index := range graph.functions {
		fmt.Fprintf(&source, "func Probe%d() {\n\t%s\n}\n\n", index, graph.call("", index, index%2 == 0,
			[2]string{oddSlice, oddSlice}))
	}
	return source.String()
}

// readerSource is a package that imports the graph's package at path and passes every function two slices of one, each
// call on a line of its own in order, every other one reaching a method through a method expression, and the line of
// the call to each function, by its index.
func (graph generatedGraph) readerSource(path string, order []int) (string, []int) {
	var source strings.Builder
	fmt.Fprintf(&source, "package reader\n\nimport subject %q\n\nfunc read() {\n", path)
	lines := make([]int, len(graph.functions))
	for position, index := range order {
		// The calls start on the sixth line, after the package clause, the import and the function's opening.
		lines[index] = 6 + position
		fmt.Fprintf(&source, "\t%s\n", graph.call("subject.", index, position%2 == 0, [2]string{oddSlice, oddSlice}))
	}
	source.WriteString("}\n")
	return source.String(), lines
}

// writeGeneratedVariants generates the graphs and writes each, under directory laid out by import path, as a package
// in each of several declaration orders, the first as generated and the rest shuffled, each with a reader calling its
// functions in another shuffled order, beside the leaf package they all import.
func writeGeneratedVariants(test *testing.T, directory string, random *rand.Rand) []generatedVariant {
	test.Helper()
	write := func(path string, name string, source string) {
		folder := filepath.Join(directory, "src", filepath.FromSlash(path))
		if makeError := os.MkdirAll(folder, 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(filepath.Join(folder, name), []byte(source), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	write("graphs/leaf", "leaf.go", leafSource)
	var variants []generatedVariant
	for graphIndex := range generatedGraphs {
		graph := newGeneratedGraph(random)
		order := make([]int, generatedFunctions)
		for index := range order {
			order[index] = index
		}
		for orderIndex := range generatedOrders {
			if orderIndex > 0 {
				random.Shuffle(len(order), func(left, right int) {
					order[left], order[right] = order[right], order[left]
				})
			}
			name := fmt.Sprintf("order%d", orderIndex)
			variant := generatedVariant{graph: graph, path: fmt.Sprintf("graphs/graph%d/%s", graphIndex, name)}
			variant.source = graph.source(name, order)
			write(variant.path, name+".go", variant.source)
			var readerSource string
			readerSource, variant.readerLines = graph.readerSource(variant.path, random.Perm(generatedFunctions))
			write(variant.path+"/reader", "reader.go", readerSource)
			variants = append(variants, variant)
		}
	}
	return variants
}
