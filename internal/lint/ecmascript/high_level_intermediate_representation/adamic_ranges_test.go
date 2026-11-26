package high_level_intermediate_representation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/system-inc/cohere/modules/mutation_aliasing"
	"github.com/system-inc/cohere/modules/static_single_assignment"
)

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

func (graph recordingRangeGraph) Closure(
	function *Function,
	block *BasicBlock,
	index int,
	into static_single_assignment.IdentifierId,
	kinds map[static_single_assignment.IdentifierId]mutation_aliasing.EffectValueKind,
) ([]Place, bool) {
	captures, frozen := graph.rangeGraph.Closure(function, block, index, into, kinds)
	if captures != nil {
		key := closureKey{block: block.Id, index: index}
		graph.answers[key] = append(graph.answers[key], closureAnswer{into: into, captures: captures, frozen: frozen})
	}
	return captures, frozen
}

// writeAdamicRangesCase writes one function as Adamic's mutation_aliasing cases records, returning how
// many closure answers it holds. TestInlineMemoCallbacksRepeatIdentically compares its bytes across runs.
//
// The cases format is Adamic's (stage1/cohere/mutation_aliasing/main.ts says what each record is). The
// function is written as the pass reads it, through rangeGraph: its blocks in order with their
// terminals' orders and returned values, its phis, its instructions' orders, places, effects and stores
// into captured bindings, its parameters, context and returns place, and whether its parameters arrive
// Frozen. Identifiers keep their ids.
//
// Two of rangeGraph's answers can't be written as facts of the function, and are written as what the
// pass was told. Closure's answer depends on the kinds the pass holds when it asks, so the pass runs
// through a recording adapter and the captures and the frozen answer it gave at each closure are
// written. And the function carries the ranges cohere's own pass gave it, as `expect` records.
func writeAdamicRangesCase(out *strings.Builder, name string, function *Function) int {
	graph := recordingRangeGraph{
		rangeGraph: newRangeGraph(function, InferAliasingEffects(function)),
		answers:    map[closureKey][]closureAnswer{},
	}
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
	for _, parameter := range graph.Parameters(function) {
		fmt.Fprintf(out, "param %d\n", value(parameter))
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
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	for _, id := range ids {
		if mutableRange := ranges.Get(id); mutableRange.IsSet() {
			fmt.Fprintf(out, "expect %d %d %d\n", id, mutableRange.Start, mutableRange.End)
		}
	}
	fmt.Fprintf(out, "expect-length %d\n", ranges.Length())
	out.WriteString("passes ranges\n")
	return closures
}
