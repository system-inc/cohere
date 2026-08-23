package fix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/parser"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rules/nexus"
)

// proposeFromRules runs real rules over the current text and collects their fixes.
//
// Every synthetic test in this package hands the engine proposals a test author wrote, which proves
// the engine's arithmetic and proves nothing about whether a real rule's ranges line up with it.
// This closes that: the offsets come from the parser, through a shipped rule, into the engine, and
// the assertion is on the resulting file.
//
// Driving real rules this way is what surfaced the trivia defect these tests now pin. Reasoning
// about ranges would not have found it, because every range involved was individually defensible.
func proposeFromRules(rules ...rule.Rule) Propose {
	return func(fileName string, text string) ([]Proposal, error) {
		rooted := fileName
		if !tspath.IsRootedDiskPath(rooted) {
			rooted = "/" + strings.TrimPrefix(rooted, "/")
		}
		rooted = tspath.NormalizePath(rooted)

		scriptKind := core.ScriptKindTS
		if strings.HasSuffix(rooted, ".tsx") {
			scriptKind = core.ScriptKindTSX
		}

		sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
			FileName: rooted, Path: tspath.Path(rooted),
		}, text, scriptKind)
		if sourceFile == nil {
			return nil, nil
		}

		var diagnostics []rule.Diagnostic
		for _, subject := range rules {
			currentRule := subject
			context := rule.Context{
				SourceFile: sourceFile,
				Report: func(diagnostic rule.Diagnostic) {
					diagnostic.RuleName = currentRule.Name
					diagnostics = append(diagnostics, diagnostic)
				},
			}
			listeners := currentRule.Run(context, nil)
			walkForRules(sourceFile.AsNode(), listeners)
		}

		return ProposalsFrom(diagnostics), nil
	}
}

func walkForRules(node *ast.Node, listeners rule.Listeners) {
	if node == nil {
		return
	}
	if listener, isListening := listeners[node.Kind]; isListening {
		listener(node)
	}
	node.ForEachChild(func(child *ast.Node) bool {
		walkForRules(child, listeners)
		return false
	})
}

// A real rule's fix must land at the right bytes and produce a file that still parses. This is the
// end-to-end proof: the rule computes ranges against a real parse, and the engine applies them.
func TestARealRuleFixLandsCorrectly(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "imports.ts")
	source := "import * as NodeFileSystem from 'fs';\n" +
		"import * as NodePath from 'path';\n" +
		"import * as NodeOperatingSystem from 'os';\n" +
		"\n" +
		"export const roots = [NodeFileSystem, NodePath, NodeOperatingSystem];\n"

	if err := os.WriteFile(fileName, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FixFile(fileName, proposeFromRules(nexus.ImportRequireNodeNamespace), DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Changed {
		t.Fatalf("the rule proposes fixes for three bare specifiers and none landed: %+v", result)
	}

	written, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	got := string(written)

	want := "import * as NodeFileSystem from 'node:fs';\n" +
		"import * as NodePath from 'node:path';\n" +
		"import * as NodeOperatingSystem from 'node:os';\n" +
		"\n" +
		"export const roots = [NodeFileSystem, NodePath, NodeOperatingSystem];\n"

	if got != want {
		t.Fatalf("the rewrite is wrong:\n  want %q\n  got  %q", want, got)
	}

	// Three fixes to one file, and every one measured against text the earlier ones moved. This is
	// the offset case in its real form rather than its synthetic one.
	if len(result.Applied) != 3 {
		t.Fatalf("expected 3 fixes applied, got %d", len(result.Applied))
	}

	if parses, reason := Parses(fileName, got); !parses {
		t.Fatalf("the rewritten file does not parse: %s", reason)
	}
}

// The same rule must reach a fixpoint: after its fixes land, re-running proposes nothing.
//
// A rule whose fix does not actually silence its own finding would loop to the pass ceiling, and
// that is worth catching here rather than discovering it on a whole tree.
func TestARealRuleConvergesInTwoPasses(t *testing.T) {
	source := "import * as NodeFileSystem from 'fs';\n"

	result, err := FixText("imports.ts", source, proposeFromRules(nexus.ImportRequireNodeNamespace), DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Converged {
		t.Fatalf("a real rule did not converge: %+v", result)
	}
	// One pass to apply, one to confirm nothing is left.
	if result.Passes != 2 {
		t.Fatalf("expected 2 passes, got %d", result.Passes)
	}
	if result.Text != "import * as NodeFileSystem from 'node:fs';\n" {
		t.Fatalf("unexpected result: %q", result.Text)
	}
}

// A rule's fix must replace a node's own text and leave the trivia before it alone.
//
// This fixture found a live defect rather than confirming a known-good behavior, and it is kept
// because the defect is easy to reintroduce. `node.Loc.Pos()` in typescript-go is the position
// before leading trivia, so the range for the specifier in `import * as X from 'fs'` spans
// `" 'fs'"` — the space included. The original `rule.ReplaceNode` used `node.Loc` directly, so a
// rule replacing that node also deleted the whitespace, producing `from'node:fs'`.
//
// The reason it matters more than its cosmetics, and the reason it belongs in this package's tests
// rather than only in the rule package's: the corrupted output still parses. The refusal guard here
// catches a rewrite that breaks syntax, and this one does not break syntax — it silently ate a
// space, a newline, or an entire comment, and every downstream check stayed green. A helper handing
// the engine a range wider than the rule meant is damage no validation downstream can detect. The
// parse guard is necessary and not sufficient, and this fixture is the standing proof of that gap.
//
// Fixed in internal/rule/rule.go, which now trims through `TokenRange`. The three shapes below are
// the ones measured as broken before the fix.
func TestReplaceNodeMustNotEatLeadingTrivia(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "space before the specifier",
			source: "import * as NodeFileSystem from 'fs';\n",
			want:   "import * as NodeFileSystem from 'node:fs';\n",
		},
		{
			name:   "newline before the specifier",
			source: "import * as NodeFileSystem from\n\t'fs';\n",
			want:   "import * as NodeFileSystem from\n\t'node:fs';\n",
		},
		{
			name:   "comment before the specifier",
			source: "import * as NodeFileSystem from /* pinned */ 'fs';\n",
			want:   "import * as NodeFileSystem from /* pinned */ 'node:fs';\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := FixText("imports.ts", testCase.source, proposeFromRules(nexus.ImportRequireNodeNamespace), DefaultMaxPasses)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Text != testCase.want {
				t.Fatalf("trivia was consumed by the fix:\n  want %q\n  got  %q", testCase.want, result.Text)
			}
		})
	}
}

// A rule that reports without a fix must leave the file alone. Most rules are in this shape
// deliberately — the namespace case in this very rule declines to fix because rewriting the import
// without re-rooting its references through scope would leave the file uncompilable.
func TestAFindingWithNoFixChangesNothing(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "named.ts")
	// A named import of a builtin: reported, and deliberately not fixed.
	source := "import { readFileSync } from 'node:fs';\n\nexport const read = readFileSync;\n"

	if err := os.WriteFile(fileName, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FixFile(fileName, proposeFromRules(nexus.ImportRequireNodeNamespace), DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Changed {
		t.Fatalf("a finding with no fix caused a rewrite")
	}

	onDisk, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != source {
		t.Fatalf("the file changed:\n  want %q\n  got  %q", source, onDisk)
	}
}

// A rule whose proposed fix breaks syntax must be refused, with the file left untouched.
//
// The synthetic tests in this package prove the guard works on ranges a test author wrote. This
// proves it fires on a proposal that arrived through the real rule interface, which is the path a
// genuinely wrong rule would take. A rule cannot damage a file it has no way to write to, and this
// is what that sentence actually means in practice.
var breakingRule = rule.Rule{
	Name: "probe-break-syntax",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindFunctionDeclaration: func(node *ast.Node) {
				body := node.AsFunctionDeclaration().Body
				if body == nil {
					return
				}
				ctx.Report(rule.Diagnostic{
					Range:      node.Loc,
					Message:    rule.Message{Id: "break"},
					SourceFile: ctx.SourceFile,
					Fixes:      []rule.Fix{ctx.ReplaceNode(body, "{ return (1; }")},
				})
			},
		}
	},
}

func TestARuleProposingBrokenSyntaxIsRefused(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "probe.ts")
	source := "export function alpha() { return 1; }\n"

	if err := os.WriteFile(fileName, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FixFile(fileName, proposeFromRules(breakingRule), DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Changed {
		t.Fatalf("a syntax-breaking fix from a real rule was written")
	}

	onDisk, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != source {
		t.Fatalf("the file was modified:\n  want %q\n  got  %q", source, onDisk)
	}

	refused := false
	for _, rejection := range result.Rejected {
		if strings.HasPrefix(rejection.Reason, ReasonParseFailure) {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("no parse-failure refusal was recorded: %+v", result.Rejected)
	}
}
