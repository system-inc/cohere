// Single static assignment construction over a real codebase.
//
// # Why this is separate from lower_corpus_test.go
//
// That test lowers with no checker, which is correct for what it asserts: the graph is well formed
// whatever the identifiers resolve to. This one needs resolution, so it pays for a real program per
// file and caps its sample to keep the runtime bounded.
//
// # How coverage is established, and how it was got wrong first
//
// Terminals are counted out of the lowered IR rather than grepped out of the source text. The first
// version of this grepped for "while (" and reported zero across 1,272 files, which is not credible
// and was not true: this codebase writes "while(", with no space. That is the same wrong-by-a-
// pattern answer lower_corpus_test.go documents finding for "switch", arrived at independently a
// second time. A terminal count cannot make that mistake, because it is reading what was built.
package high_level_intermediate_representation

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
	"github.com/system-inc/cohere/static_single_assignment"
)

// Run SSA over a real tree with a real checker and cohere every function.
func TestSsaOverRealCodebase(test *testing.T) {
	test.Parallel()

	root := corpusRoot(test)
	var files []string
	walkError := filepath.Walk(root, func(path string, info os.FileInfo, pathError error) error {
		if pathError != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx") {
			files = append(files, path)
		}
		return nil
	})
	if walkError != nil {
		test.Fatalf("walking the corpus: %v", walkError)
	}
	sort.Strings(files)
	limit := 400
	if len(files) > limit {
		files = files[:limit]
	}

	totalFns, totalPhis, totalNamed, totalUses, withPhis, violations := 0, 0, 0, 0, 0, 0
	kinds := map[string]int{}
	terminals := map[string]int{}

	for _, path := range files {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			continue
		}
		probe := rule.Rule[rule.NoOptions]{
			Name:             "ssa-corpus-harness",
			NeedsTypeChecker: true,
			Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
				return rule.Listeners{
					ast.KindSourceFile: func(node *ast.Node) {
						forEachFunctionLike(node, func(functionNode *ast.Node) {
							function := Lower(functionNode, ruleContext.TypeChecker)
							if function == nil {
								return
							}
							Construct(function)
							var each func(*Function)
							each = func(lowered *Function) {
								totalFns++
								st := CollectSsaStats(lowered)
								totalPhis += st.Phis
								totalNamed += st.NamedValues
								totalUses += st.Uses
								if st.Phis > 0 {
									withPhis++
								}
								for _, violation := range VerifySsa(lowered) {
									violations++
									if violations <= 8 {
										test.Errorf("%s: %s", filepath.Base(path), violation.Detail)
									}
									switch violation.Kind {
									case static_single_assignment.StaticSingleAssignmentViolationMultipleDefinitions:
										kinds["multiple-definitions"]++
									default:
										kinds["use-not-dominated"]++
									}
								}
								// Coverage measured from the IR, not from grepping source text.
								// A text grep for "while (" reports zero on this corpus because it
								// is written "while(", which is the same wrong-by-a-pattern answer
								// the `HIR` corpus test documents. Terminals cannot lie about this.
								for _, blk := range lowered.Blocks {
									switch blk.Terminal.(type) {
									case *While:
										terminals["While"]++
									case *DoWhile:
										terminals["DoWhile"]++
									case *For:
										terminals["For"]++
									case *ForOf:
										terminals["ForOf"]++
									case *ForIn:
										terminals["ForIn"]++
									case *Switch:
										terminals["Switch"]++
									case *Try:
										terminals["Try"]++
									case *If:
										terminals["If"]++
									case *Branch:
										terminals["Branch"]++
									case *Logical:
										terminals["Logical"]++
									case *Ternary:
										terminals["Ternary"]++
									case *Label:
										terminals["Label"]++
									case *MaybeThrow:
										terminals["MaybeThrow"]++
									}
								}
								for _, nested := range lowered.Functions {
									each(nested)
								}
							}
							each(function)
						})
					},
				}
			},
		}
		rule_testing.RunTyped(test, probe, filepath.Base(path), string(contents))
	}

	test.Logf("files=%d functions=%d", len(files), totalFns)
	test.Logf("phis=%d functionsWithPhis=%d namedValues=%d uses=%d", totalPhis, withPhis, totalNamed, totalUses)
	test.Logf("violations=%d %v", violations, kinds)
	keys := make([]string, 0, len(terminals))
	for key := range terminals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		test.Logf("  terminal %-8s %d", key, terminals[key])
	}
	// No lowering builds MaybeThrow, and terminal.go's MaybeThrow would end a run of throwable instructions,
	// which this IR's answer to ThrowsAtLastInstruction, no, only approximates (#js7tdfg). Building one here
	// forces the choice terminal.go names before it ships silently.
	if terminals["MaybeThrow"] != 0 {
		test.Errorf("lowering built %d MaybeThrow terminals over the corpus: decide at terminal.go's MaybeThrow "+
			"how a run of throwable instructions meets ThrowsAtLastInstruction (one throwing instruction per "+
			"block, answering yes) before this ships (#js7tdfg)", terminals["MaybeThrow"])
	}
	if terminals["Try"] == 0 {
		test.Error("the corpus lowered no Try, so its MaybeThrow count says nothing about try bodies")
	}
}
