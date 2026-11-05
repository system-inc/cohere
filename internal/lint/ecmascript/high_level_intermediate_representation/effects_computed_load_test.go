package high_level_intermediate_representation

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/mutation_aliasing"
)

func TestManualMemoizationPreservesComputedFrozenRead(test *testing.T) {
	test.Parallel()
	const source = `import {useCallback} from 'react'; function Component(props) {
const item=props.items[0];
const value=item.value;
const callback=useCallback(()=>[value],[value]);
consume(item);
return <div onClick={callback}/>;
}`
	for _, testCase := range []struct {
		name, source string
		findings     int
	}{
		{"computed", source, 0},
		{"dynamic key", strings.Replace(source, "props.items[0]", "props.items[props.index]", 1), 0},
		{"named", strings.Replace(source, "props.items[0]", "props.items.first", 1), 0},
		{"mutable receiver", strings.Replace(source, "props.items[0]", "makeItems()[0]", 1), 2},
		{"missing dependency", strings.Replace(source, "[value]);", "[]);", 1), 1},
		{"no later call", strings.Replace(source, "consume(item);", "", 1), 0},
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

func TestComputedLoadCopiesReceiverKind(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name string
		want mutation_aliasing.EffectValueKind
	}{
		{"Component", mutation_aliasing.EffectValueFrozen},
		{"ordinary", mutation_aliasing.EffectValueMutable},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			function, effects := effectsFor(test, "function "+testCase.name+"(items, key) { return items[key]; }")
			aliasingGraph := mutation_aliasing.BuildAliasingGraph(newRangeGraph(function, effects), function, mutation_aliasing.Options{})
			matched := 0
			for _, instruction := range function.Instructions {
				if instruction == nil {
					continue
				}
				load, ok := instruction.Value.(*ComputedLoad)
				if !ok {
					continue
				}
				matched++
				list := effects.Get(instruction.Id)
				if len(list) == 0 || list[0].Kind != mutation_aliasing.AliasingEffectCreateFrom || list[0].From.Identifier != load.Object.Identifier {
					test.Errorf("computed load effects=%v, want CreateFrom receiver", list)
				}
				if got := aliasingGraph.Kind(instruction.LValue.Identifier); got != testCase.want {
					test.Errorf("computed kind=%s, want %s", got, testCase.want)
				}
			}
			if matched != 1 {
				test.Fatalf("computed reads=%d, want one", matched)
			}
		})
	}
}
