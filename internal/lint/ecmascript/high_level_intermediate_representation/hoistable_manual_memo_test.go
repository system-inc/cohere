package high_level_intermediate_representation

import (
	"strings"
	"testing"
)

func TestManualMemoDependenciesProveOnlyNonOptionalPrefixes(test *testing.T) {
	test.Parallel()
	const source = `import {useState, useMemo} from 'react';
function usePreview(options) {
const [selected] = useState(null);
const result = useMemo(() => {
if (selected === null) return null;
return options.rows.find(row => row.id === selected) ?? null;
}, [options.rows, selected]);
return {result};
}`
	for _, testCase := range []struct {
		name   string
		source string
		fires  bool
	}{
		{"non-optional dependency", source, false},
		{"optional dependency", strings.Replace(source, "[options.rows, selected]", "[options?.rows, selected]", 1), true},
		{"unconditional read", strings.Replace(source, "if (selected === null) return null;", "", 1), false},
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
