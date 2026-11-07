package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFixture writes a module with its own go.work, loaded as a cohere worktree is, and returns its root.
func writeFixture(test *testing.T, files map[string]string) string {
	test.Helper()
	root := test.TempDir()
	all := map[string]string{
		"go.work": "go 1.27\n\nuse .\n",
		"go.mod":  "module fixture\n\ngo 1.27\n",
		// The fixture's rule package, with a type named exactly Context as the lint rules' rule.Context is.
		"internal/rule/rule.go": "package rule\n\ntype Context struct{ Program int }\n",
	}
	for name, contents := range files {
		all[name] = contents
	}
	for name, contents := range all {
		path := filepath.Join(root, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return root
}

// requireFile fails unless the file under root reads want exactly.
func requireFile(test *testing.T, root string, name string, want string) {
	test.Helper()
	contents, readError := os.ReadFile(filepath.Join(root, name))
	if readError != nil {
		test.Fatal(readError)
	}
	if string(contents) != want {
		test.Errorf("%s reads:\n%s\nwant:\n%s", name, contents, want)
	}
}

// A ctx field of rule.Context takes ruleContext at its declaration, its doc comment, its composite literal key
// and every selector, in every file of its package and in another package; a composite literal key beside a
// ruleContext parameter is no collision, since a key names only the field.
func TestARuleContextFieldIsRenamedEverywhere(test *testing.T) {
	test.Parallel()
	root := writeFixture(test, map[string]string{
		"internal/rules/walker.go": `package rules

import "fixture/internal/rule"

// Walker holds what one file's walk needs.
type Walker struct {
	// ctx is the context the rule was handed.
	ctx   rule.Context
	depth int
}

func newWalker(ruleContext rule.Context) *Walker { return &Walker{ctx: ruleContext} }
`,
		"internal/rules/read.go": `package rules

func (walker *Walker) program() int { return walker.ctx.Program + walker.depth }
`,
		"internal/rules/read_test.go": `package rules

import "testing"

func TestProgram(test *testing.T) {
	if newWalker(newWalker(walkerContext()).ctx).program() != 0 {
		test.Fail()
	}
}
`,
		"internal/rules/context.go": `package rules

import "fixture/internal/rule"

func walkerContext() rule.Context { return rule.Context{} }
`,
	})
	if code := run(root, nil, false, os.Stdout, os.Stderr); code != 0 {
		test.Fatalf("run returned %d", code)
	}
	requireFile(test, root, "internal/rules/walker.go", `package rules

import "fixture/internal/rule"

// Walker holds what one file's walk needs.
type Walker struct {
	// ruleContext is the context the rule was handed.
	ruleContext rule.Context
	depth       int
}

func newWalker(ruleContext rule.Context) *Walker { return &Walker{ruleContext: ruleContext} }
`)
	requireFile(test, root, "internal/rules/read.go", `package rules

func (walker *Walker) program() int { return walker.ruleContext.Program + walker.depth }
`)
	requireFile(test, root, "internal/rules/read_test.go", `package rules

import "testing"

func TestProgram(test *testing.T) {
	if newWalker(newWalker(walkerContext()).ruleContext).program() != 0 {
		test.Fail()
	}
}
`)
}

// A selector of the field where ruleContext already means something, a method's own parameter, refuses the
// rename whole: the field keeps ctx everywhere, and no number is added to make the name fit.
func TestAFieldReadBesideARuleContextIsRefused(test *testing.T) {
	test.Parallel()
	walker := `package rules

import "fixture/internal/rule"

type walker struct{ ctx rule.Context }

func (walker walker) both(ruleContext rule.Context) int { return walker.ctx.Program + ruleContext.Program }

func (walker walker) alone() int { return walker.ctx.Program }
`
	root := writeFixture(test, map[string]string{"internal/rules/walker.go": walker})
	if code := run(root, nil, false, os.Stdout, os.Stderr); code != 0 {
		test.Fatalf("run returned %d", code)
	}
	requireFile(test, root, "internal/rules/walker.go", walker)
}

// A ctx field of any other type is left: context.Context is named Context but declared outside the workspace,
// so not ours to name the field for, and a type of ours named anything but Context has no package name to lend.
// A ctx local and a rule.Context field already named are not this command's.
func TestCtxFieldsOfOtherTypesAreLeft(test *testing.T) {
	test.Parallel()
	sources := map[string]string{
		"internal/rules/others.go": `package rules

import (
	"context"

	"fixture/internal/rule"
)

type composeContext struct{}

type others struct {
	ctx      context.Context
	compose  struct{ ctx composeContext }
	context  rule.Context
	elements []int
}

func read(given others) int {
	ctx := given.context
	return ctx.Program + len(given.elements)
}

func cancel(given others) error { return given.ctx.Err() }

func composed(given others) composeContext { return given.compose.ctx }
`,
	}
	root := writeFixture(test, sources)
	if code := run(root, nil, false, os.Stdout, os.Stderr); code != 0 {
		test.Fatalf("run returned %d", code)
	}
	requireFile(test, root, "internal/rules/others.go", sources["internal/rules/others.go"])
}

// A pointer to rule.Context is still the context, seen through one pointer, as contextnames sees a local.
func TestAPointerToRuleContextIsRenamed(test *testing.T) {
	test.Parallel()
	root := writeFixture(test, map[string]string{
		"internal/rules/walker.go": `package rules

import "fixture/internal/rule"

type walker struct{ ctx *rule.Context }

func (walker walker) program() int { return walker.ctx.Program }
`,
	})
	if code := run(root, nil, false, os.Stdout, os.Stderr); code != 0 {
		test.Fatalf("run returned %d", code)
	}
	requireFile(test, root, "internal/rules/walker.go", `package rules

import "fixture/internal/rule"

type walker struct{ ruleContext *rule.Context }

func (walker walker) program() int { return walker.ruleContext.Program }
`)
}
