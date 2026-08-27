package high_level_intermediate_representation

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rule_testing"
)

// forEachCorpusFunction lowers every outermost function-like in the corpus and hands it to visit,
// already constructed with its ranges, disjoint set and scopes.
//
// Typed, and the checker is asserted present, for the reason `lower_corpus_test.go` records: a
// checker-less lowering never emits `StoreContext`, so every count taken through one is a fact about
// the harness rather than about the code.
//
// Note that `forEachFunctionLike` visits only OUTERMOST function-likes, so nested functions are not
// walked. That is the same denominator every measurement in this package uses, and it is stated here
// because a number taken from a different walk is not comparable to the ones in these comments.
func forEachCorpusFunction(t *testing.T, limit int,
	visit func(function *Function, ranges *MutableRanges, scopes *ReactiveScopes)) {
	t.Helper()

	var files []string
	err := filepath.Walk(corpusRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the corpus: %v", err)
	}
	sort.Strings(files)
	if len(files) > limit {
		files = files[:limit]
	}
	if len(files) < 10 {
		t.Fatalf("the corpus holds only %d files; the path is probably wrong", len(files))
	}

	for _, path := range files {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		fileName := "/corpus/" + filepath.Base(path)
		probe := rule.Rule{
			Name:             "merge-corpus",
			NeedsTypeChecker: true,
			Run: func(ctx rule.Context, options any) rule.Listeners {
				return rule.Listeners{
					ast.KindSourceFile: func(node *ast.Node) {
						if ctx.TypeChecker == nil {
							t.Fatal("the typed harness handed this probe a nil checker, so every " +
								"count below would be a fact about the harness rather than the code")
						}
						forEachFunctionLike(node, func(functionNode *ast.Node) {
							function := Lower(functionNode, ctx.TypeChecker)
							if function == nil {
								return
							}
							Construct(function)
							ranges := InferMutableRanges(function)
							set := FindDisjointMutableValuesWithRanges(function, ranges)
							visit(function, ranges, AssignReactiveScopesWithSets(function, ranges, set))
						})
					},
				}
			},
		}
		rule_testing.RunTypedFiles(t, probe, map[string]string{fileName: string(contents)}, fileName)
	}
}

// nestingItem is one entry in upstream's `assertValidBlockNesting` item list.
type nestingItem struct {
	start EvaluationOrder
	end   EvaluationOrder
}

// scopeNestingItems turns a scope range table into nesting items.
func scopeNestingItems(ranges map[ScopeId]MutableRange) []nestingItem {
	ids := make([]ScopeId, 0, len(ranges))
	for id := range ranges {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	items := make([]nestingItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, nestingItem{ranges[id].Start, ranges[id].End})
	}
	return items
}

// programBlockSubtrees is upstream's `ProgramBlockSubtree` contribution to `assertValidBlockNesting`
// (bundle lines 20956-20967): for every block with a fallthrough, the span from that block's
// terminal to the first instruction of the fallthrough.
func programBlockSubtrees(function *Function) []nestingItem {
	byId := map[BlockId]*BasicBlock{}
	for _, block := range function.Blocks {
		if block != nil {
			byId[block.Id] = block
		}
	}

	var items []nestingItem
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		fallthroughId, ok := Fallthrough(block.Terminal)
		if !ok {
			continue
		}
		fallthroughBlock := byId[fallthroughId]
		if fallthroughBlock == nil {
			continue
		}
		var end EvaluationOrder
		if len(fallthroughBlock.Instructions) > 0 {
			if first := function.Instructions[fallthroughBlock.Instructions[0]]; first != nil {
				end = first.Order
			}
		}
		if end == 0 {
			end = TerminalOrder(fallthroughBlock.Terminal)
		}
		items = append(items, nestingItem{TerminalOrder(block.Terminal), end})
	}
	return items
}

// blockNestingViolations is upstream's `recursivelyTraverseItems` invariant (bundle lines
// 20917-20950), returning the offenders instead of raising.
//
// Upstream asserts `disjoint || nested` and stops on the first failure. Counting instead is the same
// choice `ValidateScopes` made and for the same reason: a linter that stops on a graph it dislikes
// is worse than one that declines a function, and a count is what a measurement needs.
func blockNestingViolations(scopes, blocks []nestingItem) int {
	items := make([]nestingItem, 0, len(scopes)+len(blocks))
	items = append(items, scopes...)
	items = append(items, blocks...)

	// Upstream's `rangePreOrderComparator`: earlier start first, then the LONGER item first, so an
	// enclosing item is entered before the items it encloses.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].start != items[j].start {
			return items[i].start < items[j].start
		}
		return items[i].end > items[j].end
	})

	violations := 0
	var active []nestingItem
	for _, current := range items {
		for i := len(active) - 1; i >= 0; i-- {
			parent := active[i]
			disjoint := current.start >= parent.end
			nested := current.end <= parent.end
			if !disjoint && !nested {
				violations++
			}
			if disjoint {
				active = active[:i]
			} else {
				break
			}
		}
		active = append(active, current)
	}
	return violations
}
