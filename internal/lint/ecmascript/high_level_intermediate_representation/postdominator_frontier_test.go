package high_level_intermediate_representation

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// TestPostDominatorFrontiersMatchTheChainWalk holds postDominatorFrontiers to the walk it replaced.
//
// The old walk found the blocks a target post-dominates by following every block's chain of immediate
// post-dominators up to the root, into a map per target. It is kept here verbatim as the reference, and
// both run over every function the sources below lower to, nested functions included: the control-flow
// shapes written for this, and the pinned Structure corpus when it is on this machine. The answers must
// be equal, frontier order included, since the reactivity pass reads that order.
func TestPostDominatorFrontiersMatchTheChainWalk(t *testing.T) {
	t.Parallel()

	sources := map[string]string{
		"Shapes.tsx": strings.Join([]string{
			"function straight(a) { const b = a + 1; return b; }",
			"function branch(a) { if (a) { return 1; } else if (a > 2) { foo(); } return 2; }",
			"function loops(xs) { let n = 0; for (const x of xs) { if (x) continue; n++; } while (n > 0) { n--; if (n === 3) break; } do { n++; } while (n < 2); return n; }",
			"function cases(k) { switch (k) { case 1: foo(); case 2: bar(); break; default: return 0; } return 1; }",
			"function guarded(a) { try { if (a) throw new Error(); foo(); } catch (e) { bar(e); } finally { baz(); } return a; }",
			"function labeled(xs) { outer: for (const a of xs) { for (const b of a) { if (b) continue outer; if (!b) break outer; } } return xs; }",
			"function logic(a, b) { const c = a && b || (a ?? b); return c ? a : b; }",
			"function Component({ items }) { const [state, setState] = useState(0); const total = useMemo(() => items.reduce((s, x) => s + x, 0), [items]); if (!items.length) return null; return <div onClick={() => setState(state + 1)}>{total}</div>; }",
			"function early(a) { if (!a) { return; } for (let i = 0; i < a; i++) { if (i % 2) { continue; } foo(i); } }",
		}, "\n"),
	}
	if paths, contents := pinnedCorpusFilesIfPresent(t, 400); paths != nil {
		for _, path := range paths {
			sources[path] = contents[path]
		}
	}

	compared := 0
	for name, code := range sources {
		kind := core.ScriptKindTS
		if strings.HasSuffix(name, ".tsx") {
			kind = core.ScriptKindTSX
		}
		source := parser.ParseSourceFile(ast.SourceFileParseOptions{
			FileName: "/" + name,
			Path:     tspath.Path("/" + name),
		}, code, kind)
		forEachFunctionLike(source.AsNode(), func(node *ast.Node) {
			var compare func(*Function)
			compare = func(function *Function) {
				if function == nil {
					return
				}
				compared++
				r := &reactivity{function: function}
				got := r.postDominatorFrontiers()
				want := chainWalkFrontiers(function)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s, %q: the frontiers are\n  %v\nand the chain walk says\n  %v", name, function.Name,
						fmt.Sprint(got), fmt.Sprint(want))
				}
				for _, nested := range function.Functions {
					compare(nested)
				}
			}
			compare(Lower(node, nil))
		})
	}
	if compared < 20 {
		t.Fatalf("compared only %d functions, so this proved little", compared)
	}
	t.Logf("compared %d functions", compared)
}

// pinnedCorpusFilesIfPresent is pinnedCorpusFiles, answering nothing when the repository is absent.
func pinnedCorpusFilesIfPresent(t *testing.T, count int) ([]string, map[string]string) {
	t.Helper()
	var paths []string
	var contents map[string]string
	// Not parallel: the subtest is a scope for pinnedCorpusFiles to skip in, and it writes the two
	// results this function returns, which a parallel subtest would not have written yet.
	t.Run("pinned corpus", func(t *testing.T) {
		paths, contents = pinnedCorpusFiles(t, count)
	})
	return paths, contents
}

// chainWalkFrontiers is postDominatorFrontiers as it was before #hekjpw3, kept as the reference.
func chainWalkFrontiers(function *Function) map[BlockId][]BlockId {
	tree := computePostDominance(function)
	frontiers := make(map[BlockId][]BlockId, len(function.Blocks))
	for _, block := range function.Blocks {
		postDominated := make(map[BlockId]bool, len(function.Blocks))
		for _, other := range function.Blocks {
			current := other.Id
			for step := 0; step <= len(function.Blocks); step++ {
				if current == block.Id {
					if other.Id != block.Id {
						postDominated[other.Id] = true
					}
					break
				}
				next, ok := tree.immediate[current]
				if !ok || next == current {
					break
				}
				current = next
			}
		}

		seen := map[BlockId]bool{}
		var frontier []BlockId
		for _, candidate := range function.Blocks {
			if !postDominated[candidate.Id] && candidate.Id != block.Id {
				continue
			}
			for _, predecessorId := range candidate.Predecessors {
				if postDominated[predecessorId] || seen[predecessorId] {
					continue
				}
				seen[predecessorId] = true
				frontier = append(frontier, predecessorId)
			}
		}
		if len(frontier) > 0 {
			frontiers[block.Id] = frontier
		}
	}
	return frontiers
}
