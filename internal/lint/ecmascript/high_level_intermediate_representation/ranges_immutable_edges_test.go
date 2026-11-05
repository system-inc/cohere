package high_level_intermediate_representation

import (
	"fmt"
	"github.com/system-inc/cohere/static_single_assignment"
	"testing"
)

func TestImmutableSourcesDoNotWidenThroughMutableFallbacks(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name    string
		binding string
	}{
		{"property", "const items = props.items ?? [];"},
		{"local alias", "const alias = props; const items = alias ?? [];"},
	} {
		for _, functionName := range []string{"Component", "ordinary"} {
			test.Run(testCase.name+"/"+functionName, func(test *testing.T) {
				test.Parallel()
				function, ranges := rangesFor(test, fmt.Sprintf("function %s(props) {%s mutate(items); return props;}", functionName, testCase.binding))
				var mutationOrder static_single_assignment.EvaluationOrder
				calls := 0
				for _, instruction := range function.Instructions {
					if instruction != nil {
						if _, ok := instruction.Value.(*CallExpression); ok {
							mutationOrder = instruction.Order
							calls++
						}
					}
				}
				if calls != 1 {
					test.Fatalf("found %d calls, want one conditional mutation", calls)
				}
				foundSource, foundFallback, sourceWidened, fallbackWidened := false, false, false, false
				for _, identifier := range function.Identifiers {
					if identifier == nil {
						continue
					}
					switch identifier.Name {
					case "props":
						foundSource = true
						sourceWidened = sourceWidened || ranges.Get(identifier.Id).Contains(mutationOrder)
					case "items":
						foundFallback = true
						fallbackWidened = fallbackWidened || ranges.Get(identifier.Id).Contains(mutationOrder)
					}
				}
				if !foundSource || !foundFallback {
					test.Fatal("source or fallback was not exercised")
				}
				if wantWidened := functionName == "ordinary"; fallbackWidened != wantWidened {
					test.Errorf("fallback widened=%t, want %t", fallbackWidened, wantWidened)
				}
				if wantWidened := functionName == "ordinary"; sourceWidened != wantWidened {
					test.Errorf("source widened=%t, want %t", sourceWidened, wantWidened)
				}
			})
		}
	}
}

func TestManualMemoizationPreservesFrozenPropertyFallback(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name   string
		source string
		fires  bool
	}{
		{"fallback", `import {useMemo} from 'react'; import {useProjects} from './hooks';
function Component() {
const request = useProjects();
const result = useMemo(() => {
const projects = request.data ?? [];
const items = [];
projects.forEach(project => items.push(project));
return items;
}, [request.data]);
return <div>{result}</div>;
}`, false},
		{"no fallback", `import {useMemo} from 'react'; import {useProjects} from './hooks';
function Component() {
const request = useProjects();
const result = useMemo(() => {
const projects = request.data;
const items = [];
projects.forEach(project => items.push(project));
return items;
}, [request.data]);
return <div>{result}</div>;
}`, false},
		{"no iteration", `import {useMemo} from 'react'; import {useProjects} from './hooks';
function Component() {const request = useProjects(); const result = useMemo(() => request.data ?? [], [request.data]); return <div>{result}</div>;}`, false},
		{"unpreserved dependency", `import {useMemo} from 'react'; function Component({propA}) {return useMemo(() => propA.x(), [propA.x]);}`, true},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			findings, lowered := findingsForSource(test, testCase.source)
			if !lowered {
				test.Fatal("fixture did not lower")
			}
			if (len(findings) != 0) != testCase.fires {
				test.Fatalf("findings=%v, want fires=%t", findings, testCase.fires)
			}
		})
	}
}
