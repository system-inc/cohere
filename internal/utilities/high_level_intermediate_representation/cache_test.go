package high_level_intermediate_representation

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/rule_testing"
)

// The component below is the shape the cache exists for: a `useMemo`, a state setter, and a JSX
// tag, so all three rules that lower functions would want it.
const cacheProbeSource = `
import {useState, useMemo, useEffect} from 'react';
export function Widget({items}: {items: number[]}) {
	const [count, setCount] = useState(0);
	let label = 'low';
	if (items.length > 1) { label = 'high'; } else { label = 'one'; }
	const total = useMemo(() => items.reduce((a, b) => a + b, 0), [items]);
	useEffect(() => { setCount(total); }, [total]);
	return <div>{label}{count}</div>;
}
`

// totalPhis counts phis across a function AND every nested function.
//
// `CollectSSAStats` reports the top-level function only, which is why the first version of the test
// below could not see the defect it exists to catch: repeated construction adds phis wherever the
// merges are, and in a real component that is often a nested arrow. A whole-tree count is the only
// one that stays true as the probe source changes.
func totalPhis(function *Function) int {
	if function == nil {
		return 0
	}
	total := 0
	for _, block := range function.Blocks {
		total += len(block.Phis)
	}
	for _, nested := range function.Functions {
		total += totalPhis(nested)
	}
	return total
}

// TestForFunctionLowersOncePerFunction is the assertion that the cache is HIT.
//
// A faster timing number cannot distinguish a cache that hits perfectly from one that never hits,
// so this counts. It asks for the same function three times, the way the three react rules do, and
// requires exactly one lowering.
//
// It runs against the real `ForFunction` and a real `rule.Context` from the typed harness rather
// than against a stand-in cache, because the property under test is that the PRODUCTION path shares
// work. Counting is possible without a hook into `Lower` because a lowering is a distinct pointer
// every time it runs: three asks returning one pointer is one lowering.
func TestForFunctionLowersOncePerFunction(t *testing.T) {
	var results []*Function
	var directLowering *Function

	probe := rule.Rule{
		Name:             "hir-cache-probe",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ctx.TypeChecker == nil {
						t.Fatal("the harness produced no type checker, so this test would prove nothing")
					}
					if ctx.FileCache == nil {
						t.Fatal("the harness produced no file cache, so a hit could not be observed")
					}
					forEachFunctionLike(node, func(function *ast.Node) {
						if len(results) > 0 {
							return
						}
						// Three asks, standing in for the three rules.
						for range 3 {
							results = append(results, ForFunction(ctx, function))
						}
						// An independent lowering of the same node, to confirm that a fresh one is
						// genuinely a different object and the identity check above has teeth.
						directLowering = Lower(function, ctx.TypeChecker)
					})
				},
			}
		},
	}
	rule_testing.RunTyped(t, probe, "probe.tsx", cacheProbeSource)

	if len(results) != 3 {
		t.Fatalf("want three asks recorded, got %d", len(results))
	}
	if results[0] == nil {
		t.Fatal("the probe function did not lower, so this test proves nothing")
	}
	if results[1] != results[0] || results[2] != results[0] {
		t.Errorf("each ask lowered again: %p, %p, %p — the cache is not being hit",
			results[0], results[1], results[2])
	}
	if directLowering == results[0] {
		t.Fatal("a fresh Lower returned the cached pointer, so pointer identity proves nothing here")
	}
}

// TestForFunctionConstructsExactlyOnce is the correctness half, and the reason Construct lives
// inside the cached computation.
//
// `Construct` mutates in place and is not idempotent: a second run over one graph adds phis that
// correspond to no branch. The doubly-constructed graph still passes `VerifySSA`, so nothing would
// have caught this at runtime. Asserting the phi count is stable across repeated asks is what
// pins the design.
func TestForFunctionConstructsExactlyOnce(t *testing.T) {
	var firstPhis, lastPhis int

	probe := rule.Rule{
		Name:             "hir-cache-construct-probe",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					done := false
					forEachFunctionLike(node, func(function *ast.Node) {
						if done {
							return
						}
						done = true
						first := ForFunction(ctx, function)
						if first == nil {
							t.Fatal("the probe function did not lower")
						}
						firstPhis = totalPhis(first)
						for range 3 {
							lastPhis = totalPhis(ForFunction(ctx, function))
						}
					})
				},
			}
		},
	}
	rule_testing.RunTyped(t, probe, "probe.tsx", cacheProbeSource)

	if firstPhis == 0 {
		t.Fatal("the probe lowered to zero phis, so a repeated Construct could not be detected — " +
			"this test would pass against any implementation")
	}
	if firstPhis != lastPhis {
		t.Errorf("repeated asks changed the graph: %d phis then %d — Construct ran more than once",
			firstPhis, lastPhis)
	}
}

// TestForFunctionWithoutCacheStillLowers pins the nil-cache contract.
//
// `rule.Cached` treats a nil cache as "no caching, compute every time", so a harness that builds a
// Context by hand keeps working. That contract is load-bearing here and easy to break by reaching
// for `ctx.FileCache` directly.
func TestForFunctionWithoutCacheStillLowers(t *testing.T) {
	var withoutCache *Function

	probe := rule.Rule{
		Name:             "hir-cache-nil-probe",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					bare := ctx
					bare.FileCache = nil
					done := false
					forEachFunctionLike(node, func(function *ast.Node) {
						if done {
							return
						}
						done = true
						withoutCache = ForFunction(bare, function)
					})
				},
			}
		},
	}
	rule_testing.RunTyped(t, probe, "probe.tsx", cacheProbeSource)

	if withoutCache == nil {
		t.Fatal("a nil cache must still compute, but nothing came back")
	}
	if len(withoutCache.Blocks) == 0 {
		t.Fatal("a nil cache returned an empty graph rather than a real lowering")
	}
}

// TestForFunctionRecordsOneFillPerFunction proves the cache participates in the timing table's
// shared-fill accounting, which is what moves the lowering cost off whichever rule asked first.
//
// This is the second independent check that a hit is real. The pointer test shows one object comes
// back; this shows the cache recorded exactly one FILL for the function, so the work ran once by
// the cache's own accounting rather than only by identity.
func TestForFunctionRecordsOneFillPerFunction(t *testing.T) {
	var fillKeys []string
	var askedKey string

	probe := rule.Rule{
		Name:             "hir-cache-fill-probe",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					done := false
					forEachFunctionLike(node, func(function *ast.Node) {
						if done {
							return
						}
						done = true
						askedKey = cacheKeyFor(function)
						for range 3 {
							ForFunction(ctx, function)
						}
					})
					for key := range ctx.FileCache.FillDurations() {
						fillKeys = append(fillKeys, key)
					}
				},
			}
		},
	}
	rule_testing.RunTyped(t, probe, "probe.tsx", cacheProbeSource)

	matching := 0
	for _, key := range fillKeys {
		if key == askedKey {
			matching++
		}
	}
	if matching != 1 {
		t.Errorf("want exactly one recorded fill for %q after three asks, got %d (keys: %v)",
			askedKey, matching, fillKeys)
	}
}
