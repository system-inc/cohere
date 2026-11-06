package high_level_intermediate_representation

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/mutation_aliasing"
)

func TestManualMemoizationPreservesMixedCaptureConditionalCall(test *testing.T) {
	test.Parallel()
	const source = `import {useCallback} from 'react'; function Component(props) {
const item=props.item??{};
const value=item.value;
const callback=useCallback(()=>[value],[value]);
return <div onClick={callback}>{props.flag ? (()=>{consume(item);return null;})() : null}</div>;
}`
	for _, testCase := range []struct {
		name, source string
		findings     int
	}{
		{"mixed", source, 0},
		{"frozen", strings.Replace(source, "props.item??{}", "props.item", 1), 0},
		{"direct call", strings.Replace(source, "(()=>{consume(item);return null;})()", "consume(item)", 1), 0},
		{"unconditional call", strings.Replace(
			source,
			"props.flag ? (()=>{consume(item);return null;})() : null",
			"(()=>{consume(item);return null;})()",
			1,
		), 0},
		{"mutable", strings.Replace(source, "props.item??{}", "getItem(props.item)??{}", 1), 2},
		{"missing dependency", strings.Replace(source, "[value]);", "[]);", 1), 1},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			findings, lowered := findingsForSource(test, testCase.source)
			if !lowered || len(findings) != testCase.findings {
				test.Fatalf("lowered=%t findings=%v, want %d findings", lowered, findings, testCase.findings)
			}
		})
	}
}

func TestMixedCaptureClosureRefinementRetainsEffectGuards(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name, body string
		frozen     bool
	}{
		{"conditional mutation", "consume(item); return null;", true},
		{"direct mutation", "item.value=1; return null;", false},
		{"global assignment", "external=item; return null;", false},
		{"unknown method", "external.consume(item); return null;", false},
		{"impure method", "consume(item); return Math.random();", false},
		{"impure function", "consume(item); return Date();", false},
		{"impure alias", "const clock=Date; consume(item); return clock();", false},
		{"local mutation", "const result=[]; result.push(item); return result;", false},
		{"constructor", "return new Factory(item);", false},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			function, effects := effectsFor(
				test,
				"function Component(props) { const item=props.item??{}; return ()=>{"+testCase.body+"}; }",
			)
			aliasingGraph := mutation_aliasing.BuildAliasingGraph(
				newRangeGraph(function, effects),
				function,
				mutation_aliasing.Options{},
			)
			closures := 0
			for _, instruction := range function.Instructions {
				if instruction == nil {
					continue
				}
				if _, ok := instruction.Value.(*FunctionExpression); !ok {
					continue
				}
				closures++
				if got := aliasingGraph.Kind(
					instruction.LValue.Identifier,
				) == mutation_aliasing.EffectValueFrozen; got != testCase.frozen {
					test.Errorf("closure frozen=%t, want %t", got, testCase.frozen)
				}
			}
			if closures != 1 {
				test.Fatalf("closures=%d, want one", closures)
			}
		})
	}
}
