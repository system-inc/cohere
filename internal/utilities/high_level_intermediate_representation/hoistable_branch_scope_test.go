package high_level_intermediate_representation

import "testing"

func TestBranchLocalHoistableFactsRespectDominatingReactiveScope(t *testing.T) {
	testCases := []struct {
		name         string
		source       string
		wantFindings int
	}{
		{
			name: "top-level branch scope keeps branch facts",
			source: `
				import {useMemo} from 'react';
				function Component({propA, propB}) {
					return useMemo(() => {
						if (propA) return {value: propB.x.y};
					}, [propA, propB.x.y]);
				}
			`,
			wantFindings: 0,
		},
		{
			name: "dominating mutable scope keeps entry facts",
			source: `
				import {useMemo} from 'react';
				import {mutate} from 'shared-runtime';
				function Component({propA, propB}) {
					return useMemo(() => {
						const x = {};
						if (propA?.a) {
							mutate(x);
							return {value: propB.x.y};
						}
					}, [propA?.a, propB.x.y]);
				}
			`,
			wantFindings: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			findings, lowered := findingsForSource(t, testCase.source)
			if !lowered {
				t.Fatal("fixture did not lower")
			}
			if len(findings) != testCase.wantFindings {
				t.Fatalf("got %d preserved-memoization findings, want %d: %+v",
					len(findings), testCase.wantFindings, findings)
			}
		})
	}
}
