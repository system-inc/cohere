package rename

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/types/program"
)

// mentionHelpers declares usePreviousValue at line 2, column 17, and its parameter value at column 34.
const mentionHelpers = `// usePreviousValue keeps the last value, unlike usePreviousValueRef.
export function usePreviousValue(value: number): number {
	/* the value is returned as given */
	const label = "call usePreviousValue once";
	return label.length > 0 ? value : 0; // usePreviousValue ends here
}
`

const mentionReadme = "Use `usePreviousValue` to keep a value.\n\n```ts\nusePreviousValue(2);\n```\n\nThen usePreviousValue again.\n"

// mentionFixture is a program of two files and a README, its graph and its root.
func mentionFixture(test *testing.T) (*program.Graph, string) {
	test.Helper()
	root := test.TempDir()
	for name, contents := range map[string]string{
		"tsconfig.json": `{"compilerOptions":{"strict":true,"noEmit":true},"include":["*.ts"]}`,
		"helpers.ts":    mentionHelpers,
		"main.ts":       "import { usePreviousValue } from \"./helpers\";\n\nexport const result = usePreviousValue(1);\n",
		"README.md":     mentionReadme,
	} {
		if writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	graph, buildError := program.Build(program.Options{
		ConfigFileName:   filepath.Join(root, "tsconfig.json"),
		CurrentDirectory: root,
		SingleThreaded:   true,
	})
	if buildError != nil {
		test.Fatal(buildError)
	}
	return graph, root
}

// mentionPlan is the plan for renaming what sits at helpers.ts's line and column, its mentions collected.
func mentionPlan(test *testing.T, graph *program.Graph, root string, line int, column int, newName string) *Plan {
	test.Helper()
	plan, buildError := Build(context.Background(), graph, Position{
		FileName: filepath.Join(root, "helpers.ts"),
		Line:     line,
		Column:   column,
	}, newName)
	if buildError != nil {
		test.Fatal(buildError)
	}
	if collectError := CollectMentions(graph, plan, root); collectError != nil {
		test.Fatal(collectError)
	}
	return plan
}

// readFile is a fixture file's contents, the test failing if it can't be read.
func readFile(test *testing.T, root string, name string) string {
	test.Helper()
	contents, readError := os.ReadFile(filepath.Join(root, name))
	if readError != nil {
		test.Fatal(readError)
	}
	return string(contents)
}

// describeMentions is the plan's mentions as `file:line:column place rewritable`, one a line.
func describeMentions(plan *Plan, root string) string {
	var lines []string
	for _, mention := range plan.Mentions {
		lines = append(lines, strings.TrimPrefix(mention.FileName, root+string(filepath.Separator))+":"+
			fmt.Sprintf("%02d:%02d", mention.Line, mention.Column)+" "+string(mention.Place)+" "+map[bool]string{
			true: "rewritable", false: "listed"}[mention.Rewritable])
	}
	return strings.Join(lines, "\n")
}

// A rename lists every whole-word mention of the old name apart from its edits (#p9q52mg): in the comments and
// strings of the files it touches, and in markdown, a code fence marked as code. A distinctive name's comments
// and prose are rewritable; a string and a fenced block never are. usePreviousValueRef, a longer word, is no
// mention. Written with --comments, the rewritable ones change and the rest stay as they were; without it, only
// the code does.
func TestARenameListsTextMentionsAndRewritesThemOnlyWhenAsked(test *testing.T) {
	test.Parallel()
	graph, root := mentionFixture(test)
	plan := mentionPlan(test, graph, root, 2, 17, "useLastValue")
	want := strings.Join([]string{
		"README.md:01:06 markdown rewritable",
		"README.md:04:01 markdown code listed",
		"README.md:07:06 markdown rewritable",
		"helpers.ts:01:04 comment rewritable",
		"helpers.ts:04:22 string listed",
		"helpers.ts:05:42 comment rewritable",
	}, "\n")
	if got := describeMentions(plan, root); got != want {
		test.Fatalf("the mentions are\n%s\nwant\n%s", got, want)
	}

	var output bytes.Buffer
	if writeError := Write(&output, plan, false); writeError != nil {
		test.Fatal(writeError)
	}
	for _, expected := range []string{"text mentions: 6", "rewritten under --comments", "listed only: a string",
		"listed only: a code fence", "4 of them would be rewritten with --comments"} {
		if !strings.Contains(output.String(), expected) {
			test.Errorf("the dry run lacks %q:\n%s", expected, output.String())
		}
	}

	if _, applyError := Apply(plan); applyError != nil {
		test.Fatal(applyError)
	}
	helpers := readFile(test, root, "helpers.ts")
	if !strings.Contains(helpers, "// usePreviousValue keeps") || !strings.Contains(helpers, "useLastValue(") {
		test.Errorf("without --comments, the code was not renamed or a comment was:\n%s", helpers)
	}

	graph, root = mentionFixture(test)
	plan = mentionPlan(test, graph, root, 2, 17, "useLastValue")
	plan.RewriteMentions = true
	if _, applyError := Apply(plan); applyError != nil {
		test.Fatal(applyError)
	}
	helpers = readFile(test, root, "helpers.ts")
	readme := readFile(test, root, "README.md")
	for _, expected := range []string{"// useLastValue keeps the last value, unlike usePreviousValueRef.",
		"// useLastValue ends here", `"call usePreviousValue once"`} {
		if !strings.Contains(helpers, expected) {
			test.Errorf("helpers.ts after --comments lacks %q:\n%s", expected, helpers)
		}
	}
	want = "Use `useLastValue` to keep a value.\n\n```ts\nusePreviousValue(2);\n```\n\nThen useLastValue again.\n"
	if readme != want {
		test.Errorf("README.md after --comments reads\n%s\nwant\n%s", readme, want)
	}
}

// A plain word's mentions are listed and never rewritten, --comments or not: renaming the parameter value, the
// comment saying "the value is returned" is English, not a reference.
func TestAPlainWordsMentionsAreListedAndNeverRewritten(test *testing.T) {
	test.Parallel()
	graph, root := mentionFixture(test)
	plan := mentionPlan(test, graph, root, 2, 34, "amount")
	found := false
	for _, mention := range plan.Mentions {
		if mention.Rewritable {
			test.Errorf("a plain word's mention is rewritable: %+v", mention)
		}
		if mention.Place == MentionComment && strings.Contains(mention.Why, `"value" is a plain word`) {
			found = true
		}
	}
	if !found {
		test.Fatalf("the comment naming value was not listed as a plain word:\n%s", describeMentions(plan, root))
	}
	plan.RewriteMentions = true
	if _, applyError := Apply(plan); applyError != nil {
		test.Fatal(applyError)
	}
	helpers := readFile(test, root, "helpers.ts")
	if !strings.Contains(helpers, "/* the value is returned as given */") ||
		!strings.Contains(helpers, "(amount: number)") {
		test.Errorf("the code was not renamed, or the plain word's comment was:\n%s", helpers)
	}
}

// A distinctive name has a capital after its first letter, an underscore, a dollar sign or a digit.
func TestADistinctiveNameIsOneATextMatchRarelyHitsByAccident(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]bool{"usePreviousValue": true, "PageProps": true, "max_size": true,
		"value2": true, "$store": true, "data": false, "value": false, "Value": false} {
		if got := IsDistinctive(name); got != want {
			test.Errorf("IsDistinctive(%q) is %t, want %t", name, got, want)
		}
	}
}
