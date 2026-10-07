package high_level_intermediate_representation

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
	"github.com/system-inc/cohere/static_single_assignment"
)

// exportGraphs names the directory TestExportGraphsForAdamic writes to. Only a test flag sets it, so
// nothing in a cohere run can reach the export.
var exportGraphs = flag.String("export-graphs", "", "write the functions React Compiler's vendored fixtures lower to into this directory, in the cases format of Adamic's static_single_assignment slice")

// TestExportGraphsForAdamic writes every function React Compiler's own fixtures lower to, as this IR
// hands it to static_single_assignment.Construct, for Adamic's port of that module (#dnv6f2c), which
// runs the module's passes on them in Go and in the port and holds the two to each other byte for byte.
//
// It skips unless -export-graphs names a directory:
//
//	go test -count=1 -run '^TestExportGraphsForAdamic$' ./internal/lint/ecmascript/high_level_intermediate_representation -args -export-graphs <dir>
//
// Each fixture becomes one file of cases, named for the fixture, with its outermost functions and every
// function nested in them, each read through ssaGraph and the module's Graph, as construction reads it.
// Flow fixtures are left out, as the conformance corpus leaves them out: the parser can't read them.
//
// The cases format is Adamic's (stage1/cohere/static_single_assignment/main.ts says what each record
// is). Identifiers and declarations are renumbered densely from 1 in the order the function first names
// them, since the format numbers identifiers by position and the port mints new ones after the last; a
// named value is named for its declaration, and every place carries one tag. Nothing the passes read is
// lost: which values share a binding, which bindings a closure captures, every edge and its kind, and
// every place with its role.
func TestExportGraphsForAdamic(t *testing.T) {
	t.Parallel()
	if *exportGraphs == "" {
		t.Skip("writes Adamic's corpus only when -export-graphs names a directory")
	}
	if err := os.MkdirAll(*exportGraphs, 0o755); err != nil {
		t.Fatal(err)
	}

	fixtures, functions := 0, 0
	for _, fixture := range reactCompilerFixtures(t) {
		lowered := lowerReactCompilerFixture(t, fixture.source, func(ctx rule.Context, node *ast.Node) *Function {
			return Lower(node, ctx.TypeChecker)
		})
		name := fixture.name

		var out strings.Builder
		index := 0
		var write func(function *Function)
		write = func(function *Function) {
			writeAdamicCase(&out, fmt.Sprintf("%s#%d", name, index), ssaGraph{}, function)
			index++
			functions++
			for _, nested := range function.Functions {
				write(nested)
			}
		}
		for _, function := range lowered {
			write(function)
		}
		if index == 0 {
			continue
		}
		if err := os.WriteFile(filepath.Join(*exportGraphs, name+".txt"), []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		fixtures++
	}
	t.Logf("%d fixtures, %d functions, written to %s", fixtures, functions, *exportGraphs)
	if fixtures == 0 {
		t.Fatal("no fixture lowered to anything, so the export holds nothing")
	}
}

// reactCompilerFixture is one of React Compiler's vendored fixtures: its path made a file name, and its
// source.
type reactCompilerFixture struct {
	name, source string
}

// reactCompilerFixtures are React Compiler's vendored fixtures in path order, Flow left out, as the
// conformance corpus leaves them out: the parser can't read them.
func reactCompilerFixtures(t *testing.T) []reactCompilerFixture {
	t.Helper()
	root := filepath.Join("..", "..", "rules", "react", "conformance", "testdata", "fixtures")
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && !strings.HasSuffix(path, ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	var fixtures []reactCompilerFixture
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source := string(contents)
		if strings.Contains(source, "@flow") {
			continue
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, reactCompilerFixture{name: strings.NewReplacer("/", "-", " ", "-").Replace(name), source: source})
	}
	return fixtures
}

// lowerReactCompilerFixture lowers every outermost function in a fixture's source through lower, with a
// real type checker.
func lowerReactCompilerFixture(t *testing.T, source string, lower func(rule.Context, *ast.Node) *Function) []*Function {
	t.Helper()
	var lowered []*Function
	probe := rule.Rule{
		Name:             "adamic-graphs-export",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ctx.TypeChecker == nil {
						t.Fatal("the harness produced no type checker, so every reference would lower as a global")
					}
					forEachFunctionLike(node, func(function *ast.Node) {
						if fn := lower(ctx, function); fn != nil {
							lowered = append(lowered, fn)
						}
					})
				},
			}
		},
	}
	rule_testing.RunTyped(t, probe, "fixture.tsx", source)
	return lowered
}

// writeAdamicCase writes one function as Adamic's cases records, read through any IR's Graph.
func writeAdamicCase[G static_single_assignment.Graph[F, B, P], F any, B comparable, P any](out *strings.Builder, name string, graph G, function F) {
	identifiers := map[static_single_assignment.IdentifierId]int{}
	var order []static_single_assignment.IdentifierId
	identifier := func(place P) int {
		id := graph.IdentifierOf(place)
		if number, ok := identifiers[id]; ok {
			return number
		}
		order = append(order, id)
		identifiers[id] = len(order)
		return len(order)
	}

	// Every place, in the order the records name them, so the numbering is fixed before any is written.
	type placeRecord struct {
		record string
		number int
	}
	type instructionRecord struct {
		result  int
		places  []placeRecord
		context bool
	}
	type blockRecord struct {
		id           static_single_assignment.BlockId
		returns      bool
		edges        []string
		instructions []instructionRecord
		terminal     []placeRecord
	}
	var params []int
	for _, param := range graph.Params(function) {
		params = append(params, identifier(param))
	}
	returns := -1
	if place := graph.Returns(function); place != nil {
		returns = identifier(*place)
	}
	edgeNames := map[static_single_assignment.Edge]string{static_single_assignment.Real: "Real", static_single_assignment.Fallthrough: "Fallthrough", static_single_assignment.Exceptional: "Exceptional"}
	var blocks []blockRecord
	for _, block := range graph.Blocks(function) {
		record := blockRecord{id: graph.Id(block), returns: graph.EndsInReturn(block)}
		graph.EachEdge(block, func(successor static_single_assignment.BlockId, edge static_single_assignment.Edge) {
			record.edges = append(record.edges, fmt.Sprintf("edge %d %s", successor, edgeNames[edge]))
		})
		count := graph.InstructionCount(function, block)
		for index := 0; index < count; index++ {
			instruction := instructionRecord{result: -1, context: graph.IsContextStore(function, block, index)}
			graph.EachInstructionPlace(function, block, index, func(place *P, role static_single_assignment.Role) {
				number := identifier(*place)
				if role == static_single_assignment.Define {
					instruction.places = append(instruction.places, placeRecord{"define", number})
					if instruction.context && instruction.result == -1 && graph.ContextStoreDefines(function, block, index, *place) {
						instruction.result = number
					}
					return
				}
				instruction.places = append(instruction.places, placeRecord{"use", number})
			})
			record.instructions = append(record.instructions, instruction)
		}
		graph.EachTerminalPlace(block, func(place *P, role static_single_assignment.Role) {
			if role == static_single_assignment.Define {
				record.terminal = append(record.terminal, placeRecord{"terminal-define", identifier(*place)})
				return
			}
			record.terminal = append(record.terminal, placeRecord{"terminal-use", identifier(*place)})
		})
		blocks = append(blocks, record)
	}

	fmt.Fprintf(out, "function %s\nentry %d\nbound %d\nidentifier 0 -\n", name, graph.Entry(function), graph.BlockBound(function))
	declarations := map[static_single_assignment.DeclarationId]int{}
	var declarationOrder []static_single_assignment.DeclarationId
	for _, id := range order {
		declaration := graph.Declaration(function, id)
		number, ok := declarations[declaration]
		if !ok {
			declarationOrder = append(declarationOrder, declaration)
			number = len(declarationOrder)
			declarations[declaration] = number
		}
		valueName := "-"
		if graph.Named(function, id) {
			valueName = fmt.Sprintf("n%d", number)
		}
		fmt.Fprintf(out, "identifier %d %s\n", number, valueName)
	}
	for _, declaration := range declarationOrder {
		if graph.Contextual(function, declaration) {
			fmt.Fprintf(out, "contextual %d\n", declarations[declaration])
		}
	}
	for _, param := range params {
		fmt.Fprintf(out, "param %d v\n", param)
	}
	if returns >= 0 {
		fmt.Fprintf(out, "returns %d v\n", returns)
	}
	for _, block := range blocks {
		fmt.Fprintf(out, "block %d\n", block.id)
		if block.returns {
			out.WriteString("return\n")
		}
		for _, edge := range block.edges {
			out.WriteString(edge + "\n")
		}
		for _, instruction := range block.instructions {
			fmt.Fprintf(out, "instruction %d\n", instruction.result)
			if instruction.context {
				out.WriteString("context\n")
			}
			for _, place := range instruction.places {
				fmt.Fprintf(out, "%s %d v\n", place.record, place.number)
			}
		}
		for _, place := range block.terminal {
			fmt.Fprintf(out, "%s %d v\n", place.record, place.number)
		}
	}
	out.WriteString("passes construct\n")
}
