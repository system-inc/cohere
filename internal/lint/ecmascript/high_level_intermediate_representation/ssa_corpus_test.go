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
						forEachFunctionLike(node, func(fnNode *ast.Node) {
							fn := Lower(fnNode, ruleContext.TypeChecker)
							if fn == nil {
								return
							}
							Construct(fn)
							var each func(*Function)
							each = func(f *Function) {
								totalFns++
								st := CollectSsaStats(f)
								totalPhis += st.Phis
								totalNamed += st.NamedValues
								totalUses += st.Uses
								if st.Phis > 0 {
									withPhis++
								}
								for _, violation := range VerifySsa(f) {
									violations++
									if violations <= 8 {
										test.Errorf("%s: %s", filepath.Base(path), violation.Detail)
									}
									switch violation.Kind {
									case static_single_assignment.SSAViolationMultipleDefinitions:
										kinds["multiple-definitions"]++
									default:
										kinds["use-not-dominated"]++
									}
								}
								// Coverage measured from the IR, not from grepping source text.
								// A text grep for "while (" reports zero on this corpus because it
								// is written "while(", which is the same wrong-by-a-pattern answer
								// the `HIR` corpus test documents. Terminals cannot lie about this.
								for _, blk := range f.Blocks {
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
									}
								}
								for _, n := range f.Functions {
									each(n)
								}
							}
							each(fn)
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
}
