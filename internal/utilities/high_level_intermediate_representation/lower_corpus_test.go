package high_level_intermediate_representation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// corpusRoot is a real TypeScript codebase to lower against.
//
// # Why a path outside the repository, and why the test skips rather than fails
//
// Toy inputs agree with whatever the author imagined. Every construct in the hand-written tests was
// written by the same person who wrote the lowering for it, so both can share a misconception and
// the test still passes. Real code was written by someone with no knowledge of this IR, which is
// the only kind of input that can disagree with it.
//
// It is a path rather than vendored files because vendoring would freeze the corpus, and the value
// here is precisely that the code keeps changing under the lowering. The cost is that the test
// cannot run on a machine without the tree, so it SKIPS there. A skipping test proves nothing on
// CI, and that is accepted deliberately: this test is a development instrument for finding the
// constructs a hand-written suite forgot, and every defect it finds gets a hand-written test of its
// own. Two such defects were found on the first run and both are pinned in lower_test.go.
// The whole library rather than one subdirectory. The first version pointed at `utilities/` and
// lowered 171 functions cleanly, which read as a strong result until the coverage was checked
// against the constructs the design was most likely to get wrong. That subtree happens to contain
// no `switch` written with a space after the keyword, and the grep used to confirm coverage was
// itself wrong, so "no unsupported constructs" was measuring a corpus far narrower than intended.
// Widening it is the cheap fix and it costs about a second.
const corpusRoot = "/Users/kirkouimet/Projects/ahra/libraries/structure/source"

// TestLowerRealCodebase lowers every function in a real TypeScript tree and checks the invariants.
//
// It asserts nothing about what the IR SAYS, only that what it produces is well-formed: terminals
// set, successors existing, predecessors agreeing in both directions, evaluation order assigned and
// increasing, every place naming a real identifier. Those are the properties every pass built on
// this will assume, and they are the ones a construct nobody anticipated will break.
func TestLowerRealCodebase(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("the corpus at %s is not present on this machine", corpusRoot)
	}

	var files []string
	err := filepath.Walk(corpusRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the corpus: %v", err)
	}
	if len(files) < 10 {
		t.Fatalf("the corpus holds only %d files; the path is probably wrong", len(files))
	}

	totalFunctions := 0
	totalBlocks := 0
	totalInstructions := 0
	unsupported := map[string]int{}

	for _, path := range files {
		contents, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		kind := core.ScriptKindTS
		if strings.HasSuffix(path, ".tsx") {
			kind = core.ScriptKindTSX
		}
		source := parser.ParseSourceFile(ast.SourceFileParseOptions{
			FileName: path,
			Path:     tspath.Path("/" + filepath.Base(path)),
		}, string(contents), kind)

		forEachFunctionLike(source.AsNode(), func(node *ast.Node) {
			function := Lower(node, nil)
			if function == nil {
				return
			}
			totalFunctions++

			// Run the invariants against a subtest so a failure names the file and function.
			name := function.Name
			if name == "" {
				name = "<anonymous>"
			}
			t.Run(filepath.Base(path)+"/"+name, func(t *testing.T) {
				checkInvariants(t, function)
			})

			var count func(*Function)
			count = func(f *Function) {
				totalBlocks += len(f.Blocks)
				totalInstructions += len(f.Instructions)
				for _, instruction := range f.Instructions {
					if node, ok := instruction.Value.(*UnsupportedNode); ok {
						unsupported[node.Reason]++
					}
				}
				for _, nested := range f.Functions {
					count(nested)
				}
			}
			count(function)
		})
	}

	if totalFunctions < 1000 {
		t.Errorf("lowered only %d functions from %d files; the walk is probably not finding them",
			totalFunctions, len(files))
	}

	t.Logf("lowered %d functions from %d files: %d blocks, %d instructions",
		totalFunctions, len(files), totalBlocks, totalInstructions)

	// The unsupported tally is reported rather than asserted. It is the measurement that tells the
	// next person what completing this lowering actually costs, and asserting a number would make
	// the test fail when the corpus changes rather than when the lowering does.
	if len(unsupported) > 0 {
		var lines []string
		for reason, count := range unsupported {
			lines = append(lines, reason+"="+itoa(count))
		}
		t.Logf("unsupported constructs encountered: %s", strings.Join(lines, " "))
	}
}

// TestLowerRealCodebaseIsDeterministic pins that lowering the same input twice gives the same
// answer.
//
// Nondeterminism here would be invisible in every other test and fatal to a golden-file suite
// downstream. The usual source is map iteration order leaking into block or identifier numbering,
// which this package avoids by allocating ids from counters rather than from map walks.
func TestLowerRealCodebaseIsDeterministic(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("the corpus at %s is not present on this machine", corpusRoot)
	}

	path := filepath.Join(corpusRoot, "style", "ColorConverter.ts")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("the sample file is not present: %v", err)
	}

	printOnce := func() string {
		source := parser.ParseSourceFile(ast.SourceFileParseOptions{
			FileName: path,
			Path:     tspath.Path("/ColorConverter.ts"),
		}, string(contents), core.ScriptKindTS)

		var out strings.Builder
		forEachFunctionLike(source.AsNode(), func(node *ast.Node) {
			if function := Lower(node, nil); function != nil {
				out.WriteString(Print(function))
			}
		})
		return out.String()
	}

	first := printOnce()
	second := printOnce()
	if first != second {
		t.Error("lowering the same file twice produced different output; something depends on map order")
	}
	if len(first) == 0 {
		t.Error("lowering the sample file produced nothing")
	}
}

// forEachFunctionLike calls visit for every function-like node in the tree, outermost first.
//
// Nested functions are visited by `Lower` through the function arena, so this deliberately does not
// descend into a function it has already handed over: doing so would lower each inner function
// twice, once standalone and once as a child, and the standalone copy would have no context.
func forEachFunctionLike(root *ast.Node, visit func(*ast.Node)) {
	if root == nil {
		return
	}
	root.ForEachChild(func(node *ast.Node) bool {
		if ast.IsFunctionLike(node) {
			visit(node)
			return false
		}
		forEachFunctionLike(node, visit)
		return false
	})
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
