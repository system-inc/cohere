package high_level_intermediate_representation

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/mutation_aliasing"
	"github.com/system-inc/cohere/static_single_assignment"
)

// exportRanges names the directory TestExportRangesForAdamic writes to. Only a test flag sets it, so
// nothing in a cohere run can reach the export.
var exportRanges = flag.String("export-ranges", "", "write the functions React Compiler's vendored fixtures lower to, with their effects, into this directory, in the cases format of Adamic's mutation_aliasing slice")

// TestExportRangesForAdamic writes every function React Compiler's own fixtures lower to, as this IR
// hands it to mutation_aliasing.InferMutableRanges, for Adamic's port of that module (#dnv6f2c), which
// runs the pass on them in Go and in the port and holds the two to each other byte for byte.
//
// It skips unless -export-ranges names a directory:
//
//	go test -count=1 -run '^TestExportRangesForAdamic$' ./internal/lint/ecmascript/high_level_intermediate_representation -args -export-ranges <dir>
//
// Each fixture becomes one file of cases, named for the fixture, holding its outermost functions and
// every function nested in them, as ForFunction gives them. A fixture whose source names useMemo or
// useCallback is written a second time, as ForFunctionWithoutManualMemoization gives it (the erasure,
// then the inlining), under `~memo` in its functions' names.
//
// The cases format is Adamic's (stage1/cohere/mutation_aliasing/main.ts says what each record is).
// Every function is written as the pass reads it, through rangeGraph: its blocks in order with their
// terminals' orders and returned values, its phis, its instructions' orders, places, effects and
// stores into captured bindings, its parameters, context and returns place, and whether its parameters
// arrive Frozen. Identifiers keep their ids.
//
// Two of rangeGraph's answers can't be written as facts of the function, and are written as what the
// pass was told. Closure's answer depends on the kinds the pass holds when it asks, so the export runs
// the pass through a recording adapter and writes the captures and the frozen answer it gave at each
// closure, which the cases replay. And each function carries the ranges cohere's own pass gave it, as
// `expect` records, so the side of Adamic's test that runs the module in Go can show the cases
// reproduce what cohere computes before it holds the port to them.
func TestExportRangesForAdamic(t *testing.T) {
	t.Parallel()
	if *exportRanges == "" {
		t.Skip("writes Adamic's corpus only when -export-ranges names a directory")
	}
	if err := os.MkdirAll(*exportRanges, 0o755); err != nil {
		t.Fatal(err)
	}

	pipelines := []struct {
		suffix  string
		lower   func(rule.Context, *ast.Node) *Function
		applies func(source string) bool
	}{
		{"", ForFunction, func(string) bool { return true }},
		{"~memo", ForFunctionWithoutManualMemoization, func(source string) bool {
			return strings.Contains(source, "useMemo") || strings.Contains(source, "useCallback")
		}},
	}

	fixtures, functions, closures := 0, 0, 0
	for _, fixture := range reactCompilerFixtures(t) {
		var out strings.Builder
		for _, pipeline := range pipelines {
			if !pipeline.applies(fixture.source) {
				continue
			}
			index := 0
			var write func(function *Function)
			write = func(function *Function) {
				closures += writeAdamicRangesCase(&out, fmt.Sprintf("%s%s#%d", fixture.name, pipeline.suffix, index), function)
				index++
				functions++
				for _, nested := range function.Functions {
					write(nested)
				}
			}
			for _, function := range lowerReactCompilerFixture(t, fixture.source, pipeline.lower) {
				write(function)
			}
		}
		if out.Len() == 0 {
			continue
		}
		if err := os.WriteFile(filepath.Join(*exportRanges, fixture.name+".txt"), []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		fixtures++
	}
	t.Logf("%d fixtures, %d functions, %d closure answers, written to %s", fixtures, functions, closures, *exportRanges)
	if fixtures == 0 {
		t.Fatal("no fixture lowered to anything, so the export holds nothing")
	}
}

// closureAnswer is what rangeGraph's Closure answered at one Create.
type closureAnswer struct {
	into     static_single_assignment.IdentifierId
	captures []Place
	frozen   bool
}

// closureKey is where the pass asked: a block and the instruction's index in it.
type closureKey struct {
	block static_single_assignment.BlockId
	index int
}

// recordingRangeGraph is rangeGraph, keeping every answer its Closure gives that names captures.
type recordingRangeGraph struct {
	rangeGraph
	answers map[closureKey][]closureAnswer
}

func (g recordingRangeGraph) Closure(function *Function, block *BasicBlock, index int, into static_single_assignment.IdentifierId,
	kinds map[static_single_assignment.IdentifierId]mutation_aliasing.EffectValueKind) ([]Place, bool) {
	captures, frozen := g.rangeGraph.Closure(function, block, index, into, kinds)
	if captures != nil {
		key := closureKey{block: block.Id, index: index}
		g.answers[key] = append(g.answers[key], closureAnswer{into: into, captures: captures, frozen: frozen})
	}
	return captures, frozen
}

// writeAdamicRangesCase writes one function as Adamic's mutation_aliasing cases records, returning how
// many closure answers it holds.
func writeAdamicRangesCase(out *strings.Builder, name string, function *Function) int {
	graph := recordingRangeGraph{rangeGraph: newRangeGraph(function, InferAliasingEffects(function)), answers: map[closureKey][]closureAnswer{}}
	ranges := mutation_aliasing.InferMutableRanges(graph, function, mutation_aliasing.Options{})

	values := map[static_single_assignment.IdentifierId]bool{}
	value := func(place Place) static_single_assignment.IdentifierId {
		values[place.Identifier] = true
		return place.Identifier
	}

	fmt.Fprintf(out, "function %s\nentry %d\nbound %d\n", name, graph.Entry(function), graph.BlockBound(function))
	if graph.ParametersFrozen(function) {
		out.WriteString("frozen-parameters\n")
	}
	for _, param := range graph.Params(function) {
		fmt.Fprintf(out, "param %d\n", value(param))
	}
	for _, context := range graph.Context(function) {
		fmt.Fprintf(out, "context %d\n", value(context))
	}
	if returns := graph.Returns(function); returns != nil {
		fmt.Fprintf(out, "returns %d\n", value(*returns))
	}
	closures := 0
	for _, block := range graph.Blocks(function) {
		fmt.Fprintf(out, "block %d %d\n", graph.Id(block), graph.TerminalOrder(block))
		if returned, ok := graph.ReturnValue(block); ok {
			fmt.Fprintf(out, "return %d\n", value(returned))
		}
		for _, phi := range graph.Phis(block) {
			fmt.Fprintf(out, "phi %d", value(phi.Place))
			for _, operand := range phi.Operands {
				fmt.Fprintf(out, " %d=%d", operand.Predecessor, value(operand.Place))
			}
			out.WriteByte('\n')
		}
		count := graph.InstructionCount(function, block)
		for index := 0; index < count; index++ {
			order, ok := graph.InstructionOrder(function, block, index)
			if !ok {
				out.WriteString("instruction -\n")
				continue
			}
			fmt.Fprintf(out, "instruction %d\n", order)
			graph.EachInstructionPlace(function, block, index, func(place *Place, role static_single_assignment.Role) {
				if role == static_single_assignment.Define {
					fmt.Fprintf(out, "define %d\n", value(*place))
					return
				}
				fmt.Fprintf(out, "use %d\n", value(*place))
			})
			for _, effect := range graph.Effects(function, block, index) {
				from := "-"
				if effect.HasFrom {
					from = fmt.Sprint(value(effect.From))
				}
				fmt.Fprintf(out, "effect %s %d %s %s\n", effect.Kind, value(effect.Into), from, effect.Value)
			}
			if stored, ok := graph.StoredContextValue(function, block, index); ok {
				values[stored] = true
				fmt.Fprintf(out, "stored-context %d\n", stored)
			}
			for _, answer := range graph.answers[closureKey{block: graph.Id(block), index: index}] {
				frozen := 0
				if answer.frozen {
					frozen = 1
				}
				fmt.Fprintf(out, "closure %d answer %d\n", answer.into, frozen)
				for _, capture := range answer.captures {
					fmt.Fprintf(out, "capture %d\n", value(capture))
				}
				closures++
			}
		}
	}

	ids := make([]static_single_assignment.IdentifierId, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if r := ranges.Get(id); r.IsSet() {
			fmt.Fprintf(out, "expect %d %d %d\n", id, r.Start, r.End)
		}
	}
	fmt.Fprintf(out, "expect-length %d\n", ranges.Len())
	out.WriteString("passes ranges\n")
	return closures
}
