package high_level_intermediate_representation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// Run identical prepass graphs, rather than assert the loop's shape: map iteration
// can change freshly allocated identifiers even when ranges are equivalent.
func TestInlineMemoCallbacksRepeatIdentically(test *testing.T) {
	test.Parallel()
	const repetitions = 128
	fixtures, functions, splices := 0, 0, 0
	// These five differed on origin/main. Keep their presence load-bearing even
	// if the vendored corpus changes; the wider census also guards new cases.
	required := map[string]bool{
		"preserve-memo-validation-error.useMemo-infer-less-specific-conditional-access.ts#0":      false,
		"preserve-memo-validation-error.useMemo-infer-less-specific-conditional-value-block.ts#0": false,
		"preserve-memo-validation-prune-nonescaping-useMemo-mult-returns-primitive.ts#0":          false,
		"preserve-memo-validation-prune-nonescaping-useMemo-mult-returns.ts#0":                    false,
		"preserve-memo-validation-useMemo-conditional-access-own-scope.ts#0":                      false,
	}
	for _, fixture := range reactCompilerFixtures(test) {
		if !strings.Contains(fixture.source, "useMemo") && !strings.Contains(fixture.source, "useCallback") {
			continue
		}
		prepasses := lowerReactCompilerFixture(test, fixture.source, func(ruleContext rule.Context, node *ast.Node) *Function {
			f := Lower(node, ruleContext.TypeChecker)
			if f != nil {
				Construct(f)
				DropManualMemoization(f)
			}
			return f
		})
		for index, prepass := range prepasses {
			name := fmt.Sprintf("%s#%d", fixture.name, index)
			if _, needed := required[name]; needed {
				required[name] = true
			}
			// Not parallel: each case adds to splices, which this test checks once every case has run.
			test.Run(name, func(test *testing.T) {
				var expected string
				for run := 0; run < repetitions; run++ {
					f := CloneFunction(prepass)
					count := InlineImmediatelyInvokedFunctionExpressionsIncludingMemoCallbacks(f)
					if run == 0 {
						if _, needed := required[name]; needed && count == 0 {
							test.Fatal("required regression case did not inline")
						}
						splices += count
					}
					var out strings.Builder
					// Preserve raw ids as well as the export bytes; instruction ids can be
					// renumbered by later construction, which would conceal this allocation bug.
					for _, block := range f.Blocks {
						fmt.Fprintf(&out, "block %d\n", block.Id)
						for _, id := range block.Instructions {
							instruction := f.Instructions[id]
							fmt.Fprintf(&out, "%d=%d\n", id, instruction.LValue.Identifier)
						}
					}
					writeAdamicRangesCase(&out, "repeat", f)
					if out.Len() == 0 || len(f.Blocks) == 0 {
						test.Fatal("empty output: repetition would be vacuous")
					}
					if run == 0 {
						expected = out.String()
					} else if out.String() != expected {
						test.Fatalf("run %d differs from run 0 (%d bytes, %d splices)", run, out.Len(), count)
					}
				}
			})
			functions++
		}
		fixtures++
	}
	for name, seen := range required {
		if !seen {
			test.Errorf("missing regression case %s", name)
		}
	}
	test.Logf("%d fixtures, %d functions, %d splices, %d repetitions per function", fixtures, functions, splices, repetitions)
	if fixtures == 0 || functions == 0 || splices == 0 {
		test.Fatal("no memo callback inlined: repetition would be vacuous")
	}
}
