package high_level_intermediate_representation

import (
	"strings"
	"testing"
)

func TestNestedDependencyHoistingRequiresAssumedInvocation(test *testing.T) {
	test.Parallel()
	const callback = `import React from 'react'; function Component(properties) {
const callback = React.useCallback(() => properties.inputReference.current.value, [properties.inputReference]);
`
	for _, testCase := range []struct {
		name, source string
		fires        bool
	}{
		{"unknown callback consumer", callback + `consume(callback); return null;}`, false},
		{"namespace effect", callback + `React.useEffect(() => callback(), [callback]); return null;}`, false},
		{"jsx assumes invocation", callback + `return <div onClick={callback}/>;}`, true},
		{"ordinary property", strings.ReplaceAll(callback, ".current", ".value") + `return <div onClick={callback}/>;}`, false},
		{"missing reactive dependency", `import {useMemo} from 'react'; function Component(props) { const result = useMemo(() => [props.value], []); return <div>{result}</div>; }`, true},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			findings, lowered := findingsForSource(test, testCase.source)
			if !lowered || (len(findings) != 0) != testCase.fires {
				test.Fatalf("lowered=%t findings=%v; want fires=%t", lowered, findings, testCase.fires)
			}
		})
	}
}
