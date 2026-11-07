package main

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
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_rename/rename"
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

// fixtureVocabulary is a slice of policy/Abbreviations.json in its own shape: plain words, a segment word, an
// advice-only word with and without an expansion, and the matcher-judged ms.
const fixtureVocabulary = `{
	"abbreviations": [
		{"abbreviation": "config", "expansions": ["configuration"], "whole": {"style": "orDescriptive"}, "prefix": {"phase": "late"}, "suffix": {}},
		{"abbreviation": "args", "expansions": ["arguments"], "advice": "Use arguments.", "whole": {"style": "advice"}, "prefix": {"phase": "late", "style": "advice"}, "suffix": {}},
		{"abbreviation": "fn", "advice": "Use a role.", "whole": {"style": "advice"}},
		{"abbreviation": "ms", "expansions": ["milliseconds"], "suffix": {"matcher": "millisecondWord", "replacement": "InMilliseconds"}},
		{"abbreviation": "err", "expansions": ["error"], "whole": {"style": "orDescriptive"}, "prefix": {"phase": "late"}, "suffix": {}},
		{"abbreviation": "cwd", "expansions": ["workingDirectory"], "segment": {}},
		{"abbreviation": "dir", "expansions": ["directory"], "segment": {}}
	],
	"allowedNames": [],
	"allowedSegments": []
}`

// fixture is one type-checked source file.
type fixture struct {
	fileSet      *token.FileSet
	packageTypes *types.Package
	info         *types.Info
	files        []*ast.File
}

func typeCheck(test *testing.T, source string) fixture {
	test.Helper()
	filename := filepath.Join(test.TempDir(), "fixture.go")
	if writeError := os.WriteFile(filename, []byte(source), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, filename, nil, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing: %v", parseError)
	}
	info := &types.Info{
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Scopes:     map[ast.Node]*types.Scope{},
		Types:      map[ast.Expr]types.TypeAndValue{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := types.Config{Importer: importer.Default()}
	packageTypes, checkError := configuration.Check("fixture", fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("type-checking: %v", checkError)
	}
	return fixture{fileSet: fileSet, packageTypes: packageTypes, info: info, files: []*ast.File{file}}
}

func fixtureWords(test *testing.T) vocabulary {
	test.Helper()
	words, vocabularyError := parseVocabulary([]byte(fixtureVocabulary))
	if vocabularyError != nil {
		test.Fatal(vocabularyError)
	}
	return words
}

func TestErrorNames(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name     string
		body     string
		fixup    bool
		newName  string
		category string
	}{
		{
			name:    "eligible single assignment",
			body:    "func validate() error { return nil }\nfunc caller() error { err := validate(); return err }",
			newName: "validateError",
		},
		{
			name:    "if statement initializer",
			body:    "func validate() error { return nil }\nfunc caller() { if err := validate(); err != nil { panic(err) } }",
			newName: "validateError",
		},
		{
			name:     "a reused error whose first write is never read",
			body:     "func one() (int, error) { return 0, nil }\nfunc two() (int, error) { return 0, nil }\nfunc caller() int { a, err := one(); b, err := two(); _ = err; return a + b }",
			category: "a write at line 16 is never read",
		},
		{
			name:    "var then assigned takes the assigned call's name",
			body:    "func validate() error { return nil }\nfunc caller() error { var err error; err = validate(); return err }",
			newName: "validateError",
		},
		{
			name:     "assigned inside a closure",
			body:     "func validate() error { return nil }\nfunc caller() error { err := validate(); func() { err = nil }(); return err }",
			category: "read or written inside a function literal",
		},
		{
			name:    "named result takes its function's name",
			body:    "func caller() (err error) { return nil }",
			newName: "callerError",
		},
		{
			name:     "parameter",
			body:     "func caller(err error) error { return err }",
			category: "parameter",
		},
		{
			name:     "address taken",
			body:     "func validate() error { return nil }\nfunc caller() error { err := validate(); pointer := &err; return *pointer }",
			category: "address taken",
		},
		{
			name:     "declared without a value",
			body:     "func caller() error { var err error; return err }",
			category: "declared without a value",
		},
		{
			name:     "right side is not a call",
			body:     "func caller(channel chan error) error { err := <-channel; return err }",
			category: "right side is not a call",
		},
		{
			name:     "constructed error",
			body:     "func caller() error { err := fmt.Errorf(\"failed\"); return err }",
			category: "constructed error",
		},
		{
			name:    "function value takes the value's name",
			body:    "func caller(load func() error) error { err := load(); return err }",
			newName: "loadError",
		},
		{
			name:     "type is not error",
			body:     "func caller() *os.PathError { err := &os.PathError{}; return err }",
			category: "type is not error",
		},
		{
			name:    "method call on exec.Command",
			body:    "func caller() []byte { output, err := exec.Command(\"true\").Output(); _ = err; return output }",
			newName: "outputError",
		},
		{
			name:    "method promoted from an embedded exec.Cmd",
			body:    "type wrapped struct{ *exec.Cmd }\nfunc caller(command wrapped) error { err := command.Run(); return err }",
			newName: "runError",
		},
		{
			name:    "table override",
			body:    "func caller() []byte { data, err := os.ReadFile(\"x\"); _ = err; return data }",
			newName: "readError",
		},
		{
			name:    "table override for a formatter",
			body:    "func caller(source []byte) []byte { formatted, err := format.Source(source); _ = err; return formatted }",
			newName: "formatError",
		},
		{
			name:    "table override by prefix",
			body:    "func caller() int64 { value, err := strconv.ParseInt(\"1\", 10, 64); _ = err; return value }",
			newName: "parseError",
		},
		{
			name:    "a local function named like a table entry is not the table entry",
			body:    "func ReadFile(name string) ([]byte, error) { return nil, nil }\nfunc caller() []byte { data, err := ReadFile(\"x\"); _ = err; return data }",
			newName: "readFileError",
		},
		{
			name:    "abbreviation expanded in the default",
			body:    "func loadConfig() error { return nil }\nfunc caller() error { err := loadConfig(); return err }",
			newName: "loadConfigurationError",
		},
		{
			name:    "abbreviation expanded as the first word",
			body:    "func cwdLookup() error { return nil }\nfunc caller() error { err := cwdLookup(); return err }",
			newName: "workingDirectoryLookupError",
		},
		{
			name:    "initialism first word lowered",
			body:    "func HTTPGet() error { return nil }\nfunc caller() error { err := HTTPGet(); return err }",
			newName: "httpGetError",
		},
		{
			name:    "name already ending in Error",
			body:    "func wrapError() error { return nil }\nfunc caller() error { err := wrapError(); return err }",
			newName: "wrapError",
		},
		{
			name:    "abbreviated Err ending is expanded, not doubled",
			body:    "func wrapErr() error { return nil }\nfunc caller() error { err := wrapErr(); return err }",
			newName: "wrapError",
		},
		{
			name:     "a bare Err method would be the predeclared error",
			body:     "type scanner struct{}\nfunc (scanner) Err() error { return nil }\nfunc caller(input scanner) error { err := input.Err(); return err }",
			category: "name would be a keyword or predeclared identifier",
		},
		{
			name:     "advice-only word",
			body:     "func parseArgs() error { return nil }\nfunc caller() error { err := parseArgs(); return err }",
			category: "advice-only word in the function name",
		},
		{
			name:    "New folds in the package name",
			body:    "func New() (int, error) { return 0, nil }\nfunc caller() int { value, err := New(); _ = err; return value }",
			newName: "newFixtureError",
		},
		{
			name:    "For folds in the package name before the call name",
			body:    "func For() error { return nil }\nfunc caller() error { err := For(); return err }",
			newName: "fixtureForError",
		},
		{
			name:    "Of folds in the receiver type name",
			body:    "type Graph struct{}\nfunc (*Graph) Of() error { return nil }\nfunc caller(graph *Graph) error { err := graph.Of(); return err }",
			newName: "graphOfError",
		},
		{
			name:    "the receiver's last camel word is the qualifier, expanded",
			body:    "type rawConfig struct{}\nfunc (rawConfig) New() error { return nil }\nfunc caller(raw rawConfig) error { err := raw.New(); return err }",
			newName: "newConfigurationError",
		},
		{
			name:     "errors.New stays a constructed error",
			body:     "func caller() error { err := errors.New(\"failed\"); return err }",
			category: "constructed error",
		},
		{
			name:    "fixup renames newError from a New call",
			body:    "func New() error { return nil }\nfunc caller() error { newError := New(); err := New(); _ = err; return newError }",
			fixup:   true,
			newName: "newFixtureError",
		},
		{
			name:    "fixup renames ofError from a method Of",
			body:    "type Graph struct{}\nfunc (*Graph) Of() error { return nil }\nfunc caller(graph *Graph) error { ofError := graph.Of(); return ofError }",
			fixup:   true,
			newName: "graphOfError",
		},
		{
			name:     "fixup leaves a newError from any other call",
			body:     "func validate() error { return nil }\nfunc caller() error { newError := validate(); return newError }",
			fixup:    true,
			category: "fixup: call is not New, For or Of",
		},
		{
			name:     "fixup leaves a forError written twice",
			body:     "func For() error { return nil }\nfunc caller() error { forError := For(); forError = For(); return forError }",
			fixup:    true,
			category: "written more than once",
		},
		{
			name:     "advice-only word without an expansion",
			body:     "func callFn() error { return nil }\nfunc caller() error { err := callFn(); return err }",
			category: "advice-only word in the function name",
		},
	}
	words := fixtureWords(test)
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			source := "package fixture\n\nimport (\n\t\"errors\"\n\t\"fmt\"\n\t\"go/format\"\n\t\"os\"\n" +
				"\t\"os/exec\"\n\t\"strconv\"\n)\n\n" +
				"var _, _, _, _, _, _ = " +
				"errors.New, fmt.Errorf, format.Source, os.Stdout, exec.Command, strconv.Atoi\n\n" +
				testCase.body +
				"\n"
			checked := typeCheck(test, source)
			candidates := findCandidates(
				checked.packageTypes,
				checked.info,
				checked.fileSet,
				checked.files,
				words,
				testCase.fixup,
				nil,
			)
			if len(candidates) != 1 {
				test.Fatalf("got %d candidates, want 1", len(candidates))
			}
			if candidates[0].newName != testCase.newName || candidates[0].category != testCase.category {
				test.Fatalf("got name %q category %q, want name %q category %q",
					candidates[0].newName, candidates[0].category, testCase.newName, testCase.category)
			}
		})
	}
}

// TestCollidingNamesGoToTheEngine proposes two nested errors that both become readError where the inner
// one's definition reads the outer one, and confirms the engine, not the generator, refuses the collision,
// while a sibling with the same name in its own scope is accepted.
func TestCollidingNamesGoToTheEngine(test *testing.T) {
	test.Parallel()
	source := `package fixture

import "os"

func check(cause error) error { return cause }

func caller() error {
	_, err := os.ReadFile("a")
	if _, err := os.ReadFile(string(rune(len(check(err).Error())))); err != nil {
		return err
	}
	return err
}

func sibling() {
	if _, err := os.ReadFile("b"); err != nil {
		panic(err)
	}
	if _, err := os.ReadFile("c"); err != nil {
		panic(err)
	}
}
`
	checked := typeCheck(test, source)
	candidates := findCandidates(
		checked.packageTypes,
		checked.info,
		checked.fileSet,
		checked.files,
		fixtureWords(test),
		false,
		nil,
	)
	var proposals []rename.Proposal
	for _, decided := range candidates {
		if decided.newName != "readError" {
			test.Fatalf(
				"%s: got name %q category %q, want readError",
				decided.variable,
				decided.newName,
				decided.category,
			)
		}
		proposals = append(proposals, rename.Proposal{Object: decided.variable, NewName: decided.newName})
	}
	if len(proposals) != 4 {
		test.Fatalf("got %d proposals, want 4", len(proposals))
	}
	plan := rename.Check(
		rename.Package{FileSet: checked.fileSet, Types: checked.packageTypes, Info: checked.info, Files: checked.files},
		proposals,
	)
	acceptedLines := map[int]bool{}
	for object := range plan.Accepted {
		acceptedLines[checked.fileSet.Position(object.Pos()).Line] = true
	}
	if len(acceptedLines) != 2 || !acceptedLines[16] || !acceptedLines[19] {
		test.Fatalf("accepted lines %v, want the two siblings at 16 and 19", acceptedLines)
	}
	proposedAt := map[string]string{}
	for _, proposal := range proposals {
		proposedAt[checked.fileSet.Position(proposal.Object.Pos()).String()] = proposal.NewName
	}
	collisions := 0
	for _, rejection := range plan.Rejections {
		if reasonClass(
			rejection.Reason,
			rejection.NewName,
			proposedAt,
			map[string]bool{"err": true},
		) == "conflict with another proposed rename of the same name" {
			collisions++
		}
	}
	if collisions == 0 {
		test.Fatalf("no rejection classed as a collision: %+v", plan.Rejections)
	}
}

// TestQualifierWords names the errors of package functions whose all-lowercase package name is spelled in its
// camel words by qualifierWords, for each of For, Of and New, and a package outside the table as it is.
func TestQualifierWords(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		packageName  string
		functionName string
		newName      string
	}{
		{packageName: "optionschema", functionName: "For", newName: "optionSchemaForError"},
		{packageName: "tooldirectory", functionName: "Of", newName: "toolDirectoryOfError"},
		{packageName: "optionschema", functionName: "New", newName: "newOptionSchemaError"},
		{packageName: "gitignore", functionName: "New", newName: "newGitignoreError"},
	}
	words := fixtureWords(test)
	for _, testCase := range testCases {
		packageTypes := types.NewPackage("example.com/own/"+testCase.packageName, testCase.packageName)
		signature := types.NewSignatureType(
			nil,
			nil,
			nil,
			nil,
			types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Universe.Lookup("error").Type())),
			false,
		)
		function := types.NewFunc(token.NoPos, packageTypes, testCase.functionName, signature)
		newName, category := qualifiedErrorName(function, words)
		if newName != testCase.newName || category != "" {
			test.Errorf(
				"%s.%s: got name %q category %q, want %q",
				testCase.packageName,
				testCase.functionName,
				newName,
				category,
				testCase.newName,
			)
		}
	}
}
