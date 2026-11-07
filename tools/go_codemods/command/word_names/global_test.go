package main

import (
	"bufio"
	"go/ast"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/tools/go_codemods/global_rename"
	"github.com/system-inc/cohere/tools/go_codemods/rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"
)

// globalFixture declares one of each shape the advice rules read, by name.
const globalFixture = `package fixture

type Node struct{ Name string }

type Function struct{ Body []Node }

type WalkFunc func(path string) error

func Lower(node *Node) *Function { return &Function{} }

func cleanDocFn(document string) string { return document }

func FindAll(nodes []Node, fn func(Node) bool) []Node {
	var found []Node
	for _, node := range nodes {
		if fn(node) {
			found = append(found, node)
		}
	}
	return found
}

func MapAll(nodes []Node, fn func(Node) Node) []Node {
	for index := range nodes {
		nodes[index] = fn(nodes[index])
	}
	return nodes
}

func Each(nodes []Node, fn func(Node, int)) {
	for index, node := range nodes {
		fn(node, index)
	}
}

func Parse(parseFn func() *Node) *Node { return parseFn() }

func Name(fn *Node, fnName string) string { return fn.Name + fnName }

func Run(args []string, callArgs []Node, argType string) int {
	lowered := Lower(&callArgs[0])
	fn := lowered
	return len(args) + len(fn.Body) + len(argType)
}

func Print(args []Node) int { return len(args) }

type Settings struct {
	MessageArgs []string
	Config      string
}

func readConfig(path string) string { return path }
`

// TestAdviceWordsTakeTheirRuledNames runs each advice word's name through globalName with its declared object.
func TestAdviceWordsTakeTheirRuledNames(test *testing.T) {
	test.Parallel()
	loadedVocabulary := loadTestVocabulary(test)
	fixture := typeCheck(test, globalFixture)
	objects := map[string][]types.Object{}
	ast.Inspect(fixture.Files[0], func(node ast.Node) bool {
		if identifier, isIdentifier := node.(*ast.Ident); isIdentifier {
			if object := fixture.Info.Defs[identifier]; object != nil {
				objects[identifier.Name] = append(objects[identifier.Name], object)
			}
		}
		return true
	})
	// Each case names the declaration by its name and, for an fn of which the fixture has several, by the
	// function whose parameter or local it is.
	cases := []struct {
		name    string
		owner   string
		want    string
		wantWhy string
	}{
		{name: "fn", owner: "FindAll", want: "predicate"},
		{name: "fn", owner: "MapAll", want: "transform"},
		{name: "fn", owner: "Each", want: "callback"},
		{name: "fn", owner: "Name", want: "function"},
		{name: "fn", owner: "Run", want: "function"},
		{name: "parseFn", want: "parse"},
		{name: "cleanDocFn", want: "cleanDoc"},
		{name: "fnName", want: "functionName"},
		{name: "args", owner: "Run", want: "commandArguments"},
		{name: "args", owner: "Print", want: "arguments"},
		{name: "callArgs", want: "callArguments"},
		{name: "argType", want: "argumentType"},
		{name: "MessageArgs", want: "MessageArguments"},
		{name: "Config", want: "Configuration"},
		{name: "readConfig", want: "readConfiguration"},
	}
	for _, testCase := range cases {
		var object types.Object
		for _, candidate := range objects[testCase.name] {
			if testCase.owner == "" || enclosingFunction(fixture, candidate) == testCase.owner {
				object = candidate
			}
		}
		if object == nil {
			test.Fatalf("no %s in %s in the fixture", testCase.name, testCase.owner)
		}
		local := rename.IsLocal(object, fixture.Types)
		got, why := globalName(loadedVocabulary, testCase.name, object, local)
		if got != testCase.want || why != testCase.wantWhy {
			test.Errorf("%s in %s: got %q (%s), want %q", testCase.name, testCase.owner, got, why, testCase.want)
		}
	}
}

// A local whose only word is not an advice word is the local pass's, and an err word is the error pass's.
func TestGlobalNameLeavesWhatOtherPassesName(test *testing.T) {
	test.Parallel()
	loadedVocabulary := loadTestVocabulary(test)
	fixture := typeCheck(test, `package fixture

func Read(path string) (string, error) {
	config := path
	var readErr error
	return config, readErr
}
`)
	for name, want := range map[string]string{"config": reasonLocalPass, "readErr": reasonOtherPass} {
		for identifier, object := range fixture.Info.Defs {
			if identifier.Name != name || object == nil {
				continue
			}
			if got, why := globalName(loadedVocabulary, name, object, true); got != "" || why != want {
				test.Errorf("%s: got %q (%s), want the reason %q", name, got, why, want)
			}
		}
	}
}

// A name spelling a ported rule's name, whole or without its leading no, with the flagged word inside, mirrors
// the rule; a flagged word outside any rule's spelling does not.
func TestUpstreamRuleSpellings(test *testing.T) {
	test.Parallel()
	loadedVocabulary := loadTestVocabulary(test)
	rules := upstreamRules{spans: map[string]string{}}
	for _, ruleName := range []string{
		"no-unused-vars",
		"one-var",
		"no-delete-var",
		"react/jsx-no-duplicate-props",
		"no-var",
		"vars-on-top",
		"max-lines",
	} {
		rules.add(ruleName)
	}
	for name, want := range map[string]string{
		"NoUnusedVars":               "no-unused-vars",
		"TestNoUnusedVarsFires":      "no-unused-vars",
		"oneVarKindLet":              "one-var",
		"DecodeOneVarOptions":        "one-var",
		"messageUnexpectedDeleteVar": "no-delete-var",
		"messageJsxNoDuplicateProps": "react/jsx-no-duplicate-props",
		"TestVarsOnTopFires":         "vars-on-top",
		"messageVarsOnTop":           "vars-on-top",
		"maxLinesFile":               "max-lines",
		"TestOneVarious":             "",
		"isLoneVar":                  "",
		"readConfig":                 "",
		"someVarName":                "",
		"phoneVarious":               "",
	} {
		if got := rules.spelledIn(loadedVocabulary, name); got != want {
			test.Errorf("%s: spells %q, want %q", name, got, want)
		}
	}
}

// enclosingFunction is the name of the function declaration whose body or signature declares object.
func enclosingFunction(fixture rename.Package, object types.Object) string {
	for _, declaration := range fixture.Files[0].Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if isFunction && function.Pos() <= object.Pos() && object.Pos() < function.End() {
			return function.Name.Name
		}
	}
	return ""
}

// -rename-library-exports pins each named library directory by its package path, its external test package
// aside; a directory under internal/, or one no package lives in, is refused.
func TestLibraryExportsArePinnedByDirectory(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	for name, contents := range map[string]string{
		"go.work":                   "go 1.27\n\nuse .\n",
		"go.mod":                    "module fixture\n\ngo 1.27\n",
		"graph/graph.go":            "package graph\n\nfunc Params() int { return 0 }\n",
		"graph/graph_test.go":       "package graph_test\n\nimport \"testing\"\n\nfunc TestParams(test *testing.T) {}\n",
		"aliasing/aliasing.go":      "package aliasing\n\nfunc Len() int { return 0 }\n",
		"internal/hidden/hidden.go": "package hidden\n\nfunc Len() int { return 0 }\n",
	} {
		path := filepath.Join(root, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	loaded, loadError := workspace.Load(root, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	pinned, pinnedError := libraryPackagePaths(loaded, []string{"graph", " aliasing/"})
	if pinnedError != nil {
		test.Fatal(pinnedError)
	}
	if len(pinned) != 2 || pinned["fixture/graph"] == "" || pinned["fixture/aliasing"] == "" {
		test.Fatalf("pinned %v, want fixture/graph and fixture/aliasing", pinned)
	}
	for _, refused := range []string{"internal/hidden", "missing"} {
		if _, refusedError := libraryPackagePaths(loaded, []string{refused}); refusedError == nil {
			test.Errorf("%s was pinned, want it refused", refused)
		}
	}
}

// A comment citing a renamed field by its old spelling is respelled, unless a string literal spells that name too:
// there the old spelling is also a name outside Go, React's propTypes, and the comment may cite that.
func TestCitationOfANameALiteralSpellsIsKept(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	for name, contents := range map[string]string{
		"go.work": "go 1.27\n\nuse .\n",
		"go.mod":  "module fixture\n\ngo 1.27\n",
		"internal/react/react.go": `package react

// declarations holds what a component declares.
type declarations struct {
	propTypes map[string]bool
	srcPaths  []string
}

// read folds a component's propTypes object into declarations, and a srcPaths list beside it.
func read(member string, found *declarations) bool {
	return member == "propTypes" && found.propTypes != nil && len(found.srcPaths) == 0
}
`,
	} {
		path := filepath.Join(root, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	loaded, loadError := workspace.Load(root, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	newNames := map[string]string{"propTypes": "propertyTypes", "srcPaths": "sourcePaths"}
	// The fixture holds one package, react, so the first analyzed is the one whose fields are proposed.
	if len(loaded.Analyzed) == 0 {
		test.Fatal("the load analyzed no package")
	}
	var proposals []global_rename.Proposal
	for identifier, object := range loaded.Analyzed[0].TypesInfo.Defs {
		if variable, isVariable := object.(*types.Var); isVariable &&
			variable.IsField() &&
			newNames[identifier.Name] != "" {
			proposals = append(proposals, global_rename.Proposal{
				Package: loaded.Analyzed[0],
				Object:  object,
				NewName: newNames[identifier.Name],
			})
		}
	}
	result := global_rename.Plan(loaded, proposals, global_rename.Options{})
	if len(result.Accepted) != 2 {
		test.Fatalf("accepted %+v, refused %+v", result.Accepted, result.Rejections)
	}
	citationEdits(loaded, result, nil)
	if mismatches, applyError := global_rename.Apply(
		loaded,
		root,
		nil,
		result,
		bufio.NewWriter(io.Discard),
	); applyError != nil || mismatches != 0 {
		test.Fatalf("apply: %d mismatches, %v", mismatches, applyError)
	}
	got, readError := os.ReadFile(filepath.Join(root, "internal/react/react.go"))
	if readError != nil {
		test.Fatal(readError)
	}
	want := `package react

// declarations holds what a component declares.
type declarations struct {
	propertyTypes map[string]bool
	sourcePaths   []string
}

// read folds a component's propTypes object into declarations, and a sourcePaths list beside it.
func read(member string, found *declarations) bool {
	return member == "propTypes" && found.propertyTypes != nil && len(found.sourcePaths) == 0
}
`
	if string(got) != want {
		test.Errorf("react.go:\n%s\nwant:\n%s", got, want)
	}
}
