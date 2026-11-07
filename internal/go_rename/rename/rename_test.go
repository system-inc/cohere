package rename

import (
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestMain points go/build at the toolchain's `GOROOT`: under `GOFLAGS=-trimpath` the test binary has none
// baked in, and the importer finds the standard library through go/build.
func TestMain(main *testing.M) {
	output, goEnvironmentError := exec.Command("go", "env", "GOROOT").Output()
	if goEnvironmentError != nil {
		panic(goEnvironmentError)
	}
	build.Default.GOROOT = strings.TrimSpace(string(output))
	os.Exit(main.Run())
}

// checked is a fixture parsed and type-checked from a file on disk.
type checked struct {
	loadedPackage Package
	filename      string
}

func typeCheck(test *testing.T, filename string) checked {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, filename, nil, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing: %v", parseError)
	}
	info := &types.Info{
		Defs:      map[*ast.Ident]types.Object{},
		Uses:      map[*ast.Ident]types.Object{},
		Implicits: map[ast.Node]types.Object{},
		Scopes:    map[ast.Node]*types.Scope{},
		Types:     map[ast.Expr]types.TypeAndValue{},
	}
	configuration := types.Config{Importer: importer.Default()}
	packageTypes, checkError := configuration.Check("fixture", fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("type-checking: %v", checkError)
	}
	return checked{
		loadedPackage: Package{FileSet: fileSet, Types: packageTypes, Info: info, Files: []*ast.File{file}},
		filename:      filename,
	}
}

// proposeTestingT proposes every variable named t of type *testing.T, fields and all, as test.
func proposeTestingT(loadedPackage Package) []Proposal {
	var proposals []Proposal
	for identifier, object := range loadedPackage.Info.Defs {
		variable, isVariable := object.(*types.Var)
		if isVariable && identifier.Name == "t" && variable.Type().String() == "*testing.T" {
			proposals = append(proposals, Proposal{Object: variable, NewName: "test"})
		}
	}
	return proposals
}

// proposeByName proposes every local variable whose name is a key of newNames.
func proposeByName(newNames map[string]string) func(Package) []Proposal {
	return func(loadedPackage Package) []Proposal {
		var proposals []Proposal
		for identifier, object := range loadedPackage.Info.Defs {
			newName, wanted := newNames[identifier.Name]
			if variable, isVariable := object.(*types.Var); wanted &&
				isVariable &&
				IsLocal(variable, loadedPackage.Types) {
				proposals = append(proposals, Proposal{Object: variable, NewName: newName})
			}
		}
		return proposals
	}
}

// renameFixture renames the fixture's *testing.T handles named t to test.
func renameFixture(test *testing.T, source string) (Plan, string) {
	test.Helper()
	return renameFixtureProposing(test, source, proposeTestingT)
}

// renameFixtureProposing writes source, plans the renames propose asks for, applies them, re-checks the
// result, and confirms every identifier resolves to the same declaration as before. It returns the plan and
// the rewritten source.
func renameFixtureProposing(test *testing.T, source string, propose func(Package) []Proposal) (Plan, string) {
	test.Helper()
	filename := filepath.Join(test.TempDir(), "fixture.go")
	if writeError := os.WriteFile(filename, []byte(source), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	before := typeCheck(test, filename)
	plan := Check(before.loadedPackage, propose(before.loadedPackage))
	edits := Edits(before.loadedPackage, plan.Accepted)

	expected := Fingerprint(
		before.loadedPackage.FileSet,
		before.loadedPackage.Types,
		before.loadedPackage.Info,
		before.loadedPackage.Files,
	)[filename]
	offsets := IdentifierOffsets(before.loadedPackage.FileSet, before.loadedPackage.Files)[filename]
	for _, edit := range edits {
		for index, offset := range offsets {
			if offset == edit.Offset {
				expected[index].Name = edit.NewText
			}
		}
	}

	_, rewritten, rewriteError := RewriteFile(filename, edits)
	if rewriteError != nil {
		test.Fatal(rewriteError)
	}
	if writeError := os.WriteFile(filename, rewritten, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	after := typeCheck(test, filename)
	actual := Fingerprint(
		after.loadedPackage.FileSet,
		after.loadedPackage.Types,
		after.loadedPackage.Info,
		after.loadedPackage.Files,
	)[filename]
	if len(actual) != len(expected) {
		test.Fatalf("%d identifiers before, %d after", len(expected), len(actual))
	}
	for index := range expected {
		if expected[index] != actual[index] {
			test.Errorf("identifier %d: expected %+v, got %+v\n%s", index, expected[index], actual[index], rewritten)
		}
	}
	return plan, string(rewritten)
}

func requireRejected(test *testing.T, plan Plan, wantReason string) {
	test.Helper()
	if len(plan.Accepted) != 0 {
		test.Errorf("accepted %d renames, want none", len(plan.Accepted))
	}
	found := false
	for _, rejection := range plan.Rejections {
		if strings.Contains(rejection.Reason, wantReason) {
			found = true
		}
	}
	if !found {
		test.Errorf("no rejection mentioning %q in %+v", wantReason, plan.Rejections)
	}
}

func TestLocalNamedLikeTheNewNameRejects(test *testing.T) {
	test.Parallel()
	plan, rewritten := renameFixture(test, `package fixture

import "testing"

func TestOne(t *testing.T) {
	test := 1
	t.Log(test)
}
`)
	requireRejected(test, plan, "conflict")
	if !strings.Contains(rewritten, "func TestOne(t *testing.T)") {
		test.Errorf("source changed:\n%s", rewritten)
	}
}

func TestPackageFuncCalledInBodyRejects(test *testing.T) {
	test.Parallel()
	plan, _ := renameFixture(test, `package fixture

import "testing"

func test() {}

func TestTwo(t *testing.T) {
	test()
	t.Log()
}
`)
	requireRejected(test, plan, `would resolve to variable "test"`)
}

func TestFieldAndSelectorUntouched(test *testing.T) {
	test.Parallel()
	plan, rewritten := renameFixture(test, `package fixture

import "testing"

type holder struct{ t *testing.T }

func TestThree(t *testing.T) {
	h := holder{t: t}
	h.t.Log()
	t.Log()
}
`)
	if len(plan.Accepted) != 1 {
		test.Errorf("accepted %d, want the parameter only", len(plan.Accepted))
	}
	for _, want := range []string{
		"type holder struct{ t *testing.T }",
		"holder{t: test}",
		"h.t.Log()",
		"func TestThree(test *testing.T)",
		"\ttest.Log()",
	} {
		if !strings.Contains(rewritten, want) {
			test.Errorf("missing %q in:\n%s", want, rewritten)
		}
	}
	requireReason(test, plan, "not a local variable")
}

func requireReason(test *testing.T, plan Plan, wantReason string) {
	test.Helper()
	for _, rejection := range plan.Rejections {
		if strings.Contains(rejection.Reason, wantReason) {
			return
		}
	}
	test.Errorf("no rejection mentioning %q in %+v", wantReason, plan.Rejections)
}

func TestNestedLiteralsEachRenamedAndShadowing(test *testing.T) {
	test.Parallel()
	plan, rewritten := renameFixture(test, `package fixture

import "testing"

func TestFour(t *testing.T) {
	t.Run("a", func(t *testing.T) {
		t.Run("b", func(t *testing.T) {
			t.Log("b")
		})
		t.Log("a")
	})
	t.Log("outer")
}
`)
	if len(plan.Accepted) != 3 {
		test.Errorf("accepted %d, want 3; rejections %+v", len(plan.Accepted), plan.Rejections)
	}
	if regexp.MustCompile(`\bt\b`).MatchString(rewritten) {
		test.Errorf("a t survived:\n%s", rewritten)
	}
}

func TestOuterUsedInClosureRenamed(test *testing.T) {
	test.Parallel()
	plan, rewritten := renameFixture(test, `package fixture

import "testing"

func TestFive(t *testing.T) {
	run := func() {
		t.Log("inside")
	}
	run()
}
`)
	if len(plan.Accepted) != 1 {
		test.Errorf("accepted %d, want 1", len(plan.Accepted))
	}
	if !strings.Contains(rewritten, `test.Log("inside")`) {
		test.Errorf("closure use not renamed:\n%s", rewritten)
	}
}

func TestImportAliasNamedLikeTheNewNameRejects(test *testing.T) {
	test.Parallel()
	plan, _ := renameFixture(test, `package fixture

import (
	test "strings"
	"testing"
)

func TestSix(t *testing.T) {
	t.Log(test.ToUpper("x"))
}
`)
	requireRejected(test, plan, `would resolve to variable "test"`)
}

func TestUnusedParameterNamedLikeTheNewNameRejects(test *testing.T) {
	test.Parallel()
	plan, _ := renameFixture(test, `package fixture

import "testing"

func helper(t *testing.T, test int) {}
`)
	requireRejected(test, plan, "conflict")
}

func TestTableLoopRenamedInTheSameBatchUnblocksTheHandle(test *testing.T) {
	test.Parallel()
	filename := filepath.Join(test.TempDir(), "fixture.go")
	source := `package fixture

import "testing"

func TestTable(t *testing.T) {
	for _, test := range []string{"a"} {
		t.Run(test, func(t *testing.T) {
			t.Log(test)
		})
	}
}
`
	if writeError := os.WriteFile(filename, []byte(source), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	fixture := typeCheck(test, filename)
	handlesOnly := Check(fixture.loadedPackage, proposeTestingT(fixture.loadedPackage))
	if len(handlesOnly.Accepted) != 0 {
		test.Fatalf("handles alone: accepted %d, want both rejected", len(handlesOnly.Accepted))
	}
	proposals := proposeTestingT(fixture.loadedPackage)
	for identifier, object := range fixture.loadedPackage.Info.Defs {
		if identifier.Name == "test" {
			proposals = append(proposals, Proposal{Object: object, NewName: "testCase"})
		}
	}
	plan := Check(fixture.loadedPackage, proposals)
	if len(plan.Accepted) != 3 {
		test.Fatalf("accepted %d, want 3; rejections %+v", len(plan.Accepted), plan.Rejections)
	}
	_, rewritten, rewriteError := RewriteFile(filename, Edits(fixture.loadedPackage, plan.Accepted))
	if rewriteError != nil {
		test.Fatal(rewriteError)
	}
	if !strings.Contains(string(rewritten), "test.Run(testCase, func(test *testing.T) {\n\t\t\ttest.Log(testCase)") {
		test.Errorf("unexpected rewrite:\n%s", rewritten)
	}
}

func TestReceiverAndResultNamedLikeTheirTypeAccepted(test *testing.T) {
	test.Parallel()
	plan, rewritten := renameFixtureProposing(test, `package fixture

type nodeType struct{ next *nodeType }

func (n nodeType) Next() nodeType {
	return *n.next
}

func first() (n nodeType) {
	return n
}
`, proposeByName(map[string]string{"n": "nodeType"}))
	if len(plan.Accepted) != 2 {
		test.Errorf(
			"accepted %d, want the receiver and the result; rejections %+v",
			len(plan.Accepted),
			plan.Rejections,
		)
	}
	for _, want := range []string{
		"func (nodeType nodeType) Next() nodeType {",
		"return *nodeType.next",
		"func first() (nodeType nodeType) {",
	} {
		if !strings.Contains(rewritten, want) {
			test.Errorf("missing %q in:\n%s", want, rewritten)
		}
	}
}

func TestReceiverNamedLikeItsTypeRefusedWhenTheBodyBuildsOne(test *testing.T) {
	test.Parallel()
	plan, _ := renameFixtureProposing(test, `package fixture

type nodeType struct{ next *nodeType }

func (n nodeType) Clone() *nodeType {
	return &nodeType{next: n.next}
}
`, proposeByName(map[string]string{"n": "nodeType"}))
	requireRejected(test, plan, `would resolve to variable "nodeType"`)
}

func TestReceiverNamedLikeItsTypeRefusedWhenTheBodyConverts(test *testing.T) {
	test.Parallel()
	plan, _ := renameFixtureProposing(test, `package fixture

type count int

func (c count) Plus(value int) count {
	return c + count(value)
}
`, proposeByName(map[string]string{"c": "count"}))
	requireRejected(test, plan, `would resolve to variable "count"`)
}

// A local named for its type, `p := &parser{}`, takes the type's name: the type read in its own declaration
// comes before the local's scope opens, so it still names the type. The same rename is refused where the body
// reads the type again after the declaration, and where the declaration reads a variable, not a type, of the
// new name (`n := total(...)` would become `total := total(...)`).
func TestLocalNamedLikeTheTypeItsDeclarationBuildsAccepted(test *testing.T) {
	test.Parallel()
	plan, rewritten := renameFixtureProposing(test, `package fixture

type parser struct{ this *parser }

type comparison struct{ offsets []int }

func newParser() *parser {
	p := &parser{}
	p.this = p
	return p
}

func compare(offsets []int) int {
	var c = &comparison{offsets: offsets}
	return len(c.offsets)
}
`, proposeByName(map[string]string{"p": "parser", "c": "comparison"}))
	if len(plan.Accepted) != 2 {
		test.Errorf("accepted %d, want both locals; rejections %+v", len(plan.Accepted), plan.Rejections)
	}
	want := `func newParser() *parser {
	parser := &parser{}
	parser.this = parser
	return parser
}

func compare(offsets []int) int {
	var comparison = &comparison{offsets: offsets}
	return len(comparison.offsets)
}
`
	if !strings.HasSuffix(rewritten, want) {
		test.Errorf("rewritten:\n%s\nwant it to end with:\n%s", rewritten, want)
	}
}

func TestLocalNamedLikeItsTypeRefusedWhenTheBodyReadsTheTypeAfter(test *testing.T) {
	test.Parallel()
	plan, rewritten := renameFixtureProposing(test, `package fixture

type parser struct{ this *parser }

func newParser() *parser {
	p := &parser{}
	var other *parser = p
	return other
}
`, proposeByName(map[string]string{"p": "parser"}))
	requireRejected(test, plan, `would resolve to variable "parser"`)
	if !strings.Contains(rewritten, "p := &parser{}") {
		test.Errorf("source changed:\n%s", rewritten)
	}
}

func TestLocalNamedLikeAVariableItsDeclarationReadsRefused(test *testing.T) {
	test.Parallel()
	plan, _ := renameFixtureProposing(test, `package fixture

func runningSums(values []int, total int) []int {
	var sums []int
	for _, value := range values {
		n := total + value
		sums = append(sums, n)
	}
	return append(sums, total)
}
`, proposeByName(map[string]string{"n": "total"}))
	requireRejected(test, plan, `would resolve to variable "total"`)
}

// A parameter named in its function's `Mutates:` doc line is renamed there too, and only there: a prose
// mention of the old name elsewhere in the comment, and a Mutates line naming another parameter, stay.
func TestMutatesLineFollowsTheRenamedParameter(test *testing.T) {
	test.Parallel()
	plan, rewritten := renameFixtureProposing(test, `package fixture

// fill writes into cfg, the old name.
//
// Mutates: cfg the caller hands it in to be filled
// Mutates: other the caller hands it in too
func fill(cfg map[string]int, other map[string]int) {
	cfg["a"] = 1
	other["b"] = 2
}
`, proposeByName(map[string]string{"cfg": "configuration"}))
	if len(plan.Accepted) != 1 {
		test.Fatalf("accepted %d renames, want 1: %+v", len(plan.Accepted), plan.Rejections)
	}
	for _, want := range []string{
		"// fill writes into cfg, the old name.",
		"// Mutates: configuration the caller hands it in to be filled",
		"// Mutates: other the caller hands it in too",
		"func fill(configuration map[string]int, other map[string]int) {",
		"\tconfiguration[\"a\"] = 1",
	} {
		if !strings.Contains(rewritten, want) {
			test.Errorf("rewritten source lacks %q:\n%s", want, rewritten)
		}
	}
}

// A parameter its function's doc comment writes alone in backticks is renamed there too. A code span holding
// more than the name, a backtick pair that only looks like a span because one span closed before it, a span
// naming another parameter, a bare mention, and a span in another function's comment all stay.
func TestCodeSpanFollowsTheRenamedParameter(test *testing.T) {
	test.Parallel()
	// The fixture's comments hold backticks, which a raw string cannot, so it is written a line at a time.
	fixtureSource := "package fixture\n\n" +
		"// split cuts `str` at each bar; `str` may be empty, `str[0]` is the first byte, and str alone is prose.\n" +
		"// It reads `other` too, and `a`str`b` pairs a and b around a bare str.\n" +
		"func split(str string, other string) string { return str + other }\n\n" +
		"// unrelated mentions `str`, which is not its parameter.\n" +
		"func unrelated(value string) string { return value }\n"
	plan, rewritten := renameFixtureProposing(test, fixtureSource, proposeByName(map[string]string{"str": "text"}))
	if len(plan.Accepted) != 1 {
		test.Fatalf("accepted %d renames, want 1: %+v", len(plan.Accepted), plan.Rejections)
	}
	for _, want := range []string{
		"// split cuts `text` at each bar; `text` may be empty, `str[0]` is the first byte, and str alone is prose.",
		"// It reads `other` too, and `a`str`b` pairs a and b around a bare str.",
		"func split(text string, other string) string { return text + other }",
		"// unrelated mentions `str`, which is not its parameter.",
	} {
		if !strings.Contains(rewritten, want) {
			test.Errorf("rewritten source lacks %q:\n%s", want, rewritten)
		}
	}
}
