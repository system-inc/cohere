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
	"github.com/system-inc/cohere/internal/naming/go_origin_names"
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

// fixtureModule is the fixtures' own module: the fixture package and the rule package below live in it.
const fixtureModule = "example.com/own"

// rulePackageSource is a sibling package of the fixture with a type named exactly Context, like the lint
// rules' rule.Context.
const rulePackageSource = "package rule\n\ntype Context struct{}\n\nfunc (Context) Report() {}\n"

// fixtureVocabulary is a slice of policy/Abbreviations.json in its own shape.
const fixtureVocabulary = `{
	"abbreviations": [
		{"abbreviation": "config", "expansions": ["configuration"], "whole": {"style": "orDescriptive"}, "prefix": {"phase": "late"}, "suffix": {}},
		{"abbreviation": "fn", "advice": "Use a role.", "whole": {"style": "advice"}}
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

// fixtureImporter serves the fixture's sibling packages, and the standard library through the default
// importer.
type fixtureImporter struct {
	packages map[string]*types.Package
	fallback types.Importer
}

func (fixtures fixtureImporter) Import(path string) (*types.Package, error) {
	if packageTypes, found := fixtures.packages[path]; found {
		return packageTypes, nil
	}
	return fixtures.fallback.Import(path)
}

// checkFile parses and type-checks one file as packagePath.
func checkFile(test *testing.T, packagePath string, source string, packageImporter types.Importer) fixture {
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
	configuration := types.Config{Importer: packageImporter}
	packageTypes, checkError := configuration.Check(packagePath, fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("type-checking %s: %v", packagePath, checkError)
	}
	return fixture{fileSet: fileSet, packageTypes: packageTypes, info: info, files: []*ast.File{file}}
}

// optionSchemaPackageSource is a sibling package whose all-lowercase name QualifierSpelling spells in camel
// words, with a type named exactly Context.
const optionSchemaPackageSource = "package optionschema\n\ntype Context struct{}\n"

// typeCheck checks source as the fixture package of the fixture module.
func typeCheck(test *testing.T, source string) fixture {
	test.Helper()
	return typeCheckAs(test, fixtureModule+"/fixture", source)
}

// typeCheckAs checks source as packagePath, which may import the fixture module's rule and optionschema
// packages.
func typeCheckAs(test *testing.T, packagePath string, source string) fixture {
	test.Helper()
	rule := checkFile(test, fixtureModule+"/rule", rulePackageSource, importer.Default())
	optionSchema := checkFile(test, fixtureModule+"/optionschema", optionSchemaPackageSource, importer.Default())
	return checkFile(test, packagePath, source, fixtureImporter{
		packages: map[string]*types.Package{
			fixtureModule + "/rule":         rule.packageTypes,
			fixtureModule + "/optionschema": optionSchema.packageTypes,
		},
		fallback: importer.Default(),
	})
}

func fixtureWords(test *testing.T) go_origin_names.Vocabulary {
	test.Helper()
	words, vocabularyError := parseVocabulary([]byte(fixtureVocabulary))
	if vocabularyError != nil {
		test.Fatal(vocabularyError)
	}
	return words
}

func TestContextNames(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name     string
		body     string
		newName  string
		category string
	}{
		{
			name:    "context.WithTimeout gives timeoutContext",
			body:    "func caller(parent context.Context) { ctx, cancel := context.WithTimeout(parent, time.Second); defer cancel(); _ = ctx }",
			newName: "timeoutContext",
		},
		{
			name:    "context.WithDeadline gives timeoutContext",
			body:    "func caller(parent context.Context) { ctx, cancel := context.WithDeadline(parent, time.Now()); defer cancel(); _ = ctx }",
			newName: "timeoutContext",
		},
		{
			name:    "context.WithCancel gives cancelContext",
			body:    "func caller(parent context.Context) { ctx, cancel := context.WithCancel(parent); defer cancel(); _ = ctx }",
			newName: "cancelContext",
		},
		{
			name:    "context.Background gives backgroundContext",
			body:    "func caller() context.Context { ctx := context.Background(); return ctx }",
			newName: "backgroundContext",
		},
		{
			name:    "signal.NotifyContext gives signalContext",
			body:    "func caller() { ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt); defer stop(); _ = ctx }",
			newName: "signalContext",
		},
		{
			name:    "a testing.T's Context, declared on the unexported common, gives testContext",
			body:    "func caller(test *testing.T) { ctx := test.Context(); _ = ctx }",
			newName: "testContext",
		},
		{
			name:     "a testing.B's Context is not in the table",
			body:     "func caller(benchmark *testing.B) { ctx := benchmark.Context(); _ = ctx }",
			category: "call not in the table",
		},
		{
			name:    "an http.Request's Context gives requestContext",
			body:    "func caller(request *http.Request) { ctx := request.Context(); _ = ctx }",
			newName: "requestContext",
		},
		{
			name:     "context.WithValue is not in the table",
			body:     "func caller(parent context.Context) { ctx := context.WithValue(parent, 1, 2); _ = ctx }",
			category: "call not in the table",
		},
		{
			name:    "a parameter is the caller's",
			body:    "func caller(ctx context.Context) context.Context { return ctx }",
			newName: "callerContext",
		},
		{
			name:    "a function literal's parameter is the caller's",
			body:    "var run = func(ctx context.Context) context.Context { return ctx }",
			newName: "callerContext",
		},
		{
			name:     "a named result is not a parameter",
			body:     "func caller() (ctx context.Context) { return context.Background() }",
			category: "named result",
		},
		{
			name:     "a parameter of another type is not a context",
			body:     "func caller(ctx int) int { return ctx }",
			category: "type is neither context.Context nor an own Context type",
		},
		{
			name:     "reused by a later short declaration",
			body:     "func caller() { ctx, cancel := context.WithCancel(context.Background()); defer cancel(); ctx, stop := context.WithTimeout(ctx, time.Second); defer stop(); _ = ctx }",
			category: "written more than once",
		},
		{
			name:     "address taken",
			body:     "func caller() { ctx := context.Background(); pointer := &ctx; _ = pointer }",
			category: "address taken",
		},
		{
			name:     "type is neither context.Context nor an own Context type",
			body:     "func caller() { ctx := 1; _ = ctx }",
			category: "type is neither context.Context nor an own Context type",
		},
		{
			name:    "a rule.Context parameter takes its package name",
			body:    "func check(ctx rule.Context) { ctx.Report() }",
			newName: "ruleContext",
		},
		{
			name:    "a pointer to an own composeContext keeps the type name, written more than once",
			body:    "type composeContext struct{}\nfunc compose(ctx *composeContext) { ctx = &composeContext{}; _ = ctx }",
			newName: "composeContext",
		},
		{
			name:    "an own escapeContext value variable",
			body:    "type escapeContext struct{}\nfunc escape() { ctx := escapeContext{}; _ = ctx }",
			newName: "escapeContext",
		},
		{
			name:    "an exported own type is lowered and its vocabulary words expanded",
			body:    "type ConfigContext struct{}\nfunc load(ctx ConfigContext) { _ = ctx }",
			newName: "configurationContext",
		},
		{
			name:     "an own type with an advice-only word",
			body:     "type fnContext struct{}\nfunc call(ctx fnContext) { _ = ctx }",
			category: "advice-only word in the type name",
		},
		{
			name:     "an own type's receiver is left for the receiver pass",
			body:     "type composeContext struct{}\nfunc (ctx *composeContext) compose() { _ = ctx }",
			category: "own Context type: receiver, left for the receiver pass",
		},
		{
			name:     "a Context type from outside the module",
			body:     "func load(ctx *build.Context) { _ = ctx }",
			category: "type is neither context.Context nor an own Context type",
		},
	}
	words := fixtureWords(test)
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			source := "package fixture\n\nimport (\n\t\"context\"\n\t\"example.com/own/rule\"\n\t\"go/build\"\n" +
				"\t\"net/http\"\n\t\"os\"\n\t\"os/signal\"\n\t\"testing\"\n\t\"time\"\n)\n\n" +
				"var _, _, _, _, _ = context.Background, http.Get, os.Interrupt, signal.Notify, time.Second\n" +
				"var _ *testing.T\nvar _ rule.Context\nvar _ build.Context\n\n" +
				testCase.body + "\n"
			checked := typeCheck(test, source)
			candidates := findCandidates(checked.packageTypes, checked.info, checked.files, fixtureModule, words)
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

// TestExistingNameCollisionIsRejectedByTheEngine proposes ruleContext for a rule.Context parameter whose
// function already holds a local named ruleContext in a block that also reads ctx, and confirms the engine
// refuses it while a sibling function's ctx is accepted.
func TestExistingNameCollisionIsRejectedByTheEngine(test *testing.T) {
	test.Parallel()
	source := `package fixture

import "example.com/own/rule"

func colliding(ctx rule.Context) {
	if true {
		ruleContext := 1
		_ = ruleContext
		ctx.Report()
	}
}

func clean(ctx rule.Context) {
	ctx.Report()
}
`
	checked := typeCheck(test, source)
	candidates := findCandidates(checked.packageTypes, checked.info, checked.files, fixtureModule, fixtureWords(test))
	var proposals []rename.Proposal
	for _, decided := range candidates {
		if decided.newName != "ruleContext" {
			test.Fatalf(
				"%s: got name %q category %q, want ruleContext",
				decided.variable,
				decided.newName,
				decided.category,
			)
		}
		proposals = append(proposals, rename.Proposal{Object: decided.variable, NewName: decided.newName})
	}
	if len(proposals) != 2 {
		test.Fatalf("got %d proposals, want 2", len(proposals))
	}
	plan := rename.Check(
		rename.Package{FileSet: checked.fileSet, Types: checked.packageTypes, Info: checked.info, Files: checked.files},
		proposals,
	)
	if len(plan.Accepted) != 1 || len(plan.Rejections) != 1 {
		test.Fatalf(
			"got %d accepted and %d rejected, want 1 and 1: %+v",
			len(plan.Accepted),
			len(plan.Rejections),
			plan.Rejections,
		)
	}
	if line := checked.fileSet.Position(plan.Rejections[0].Object.Pos()).Line; line != 5 {
		test.Fatalf("rejected the ctx declared on line %d, want the colliding one on line 5", line)
	}
	if !strings.HasPrefix(plan.Rejections[0].Reason, "conflict:") {
		test.Fatalf("rejection reason %q, want a conflict", plan.Rejections[0].Reason)
	}
}

// TestNameOfItsOwnTypeIsRejectedAsShadowing proposes composeContext for a `ctx *composeContext` parameter
// whose function body names the type, which the parameter would shadow there, and confirms the engine refuses
// it and the report classes it as a conflict with a name already in scope, not as a collision between two
// proposals. (In the signature itself the type stays visible: a parameter's scope starts at the body.)
func TestNameOfItsOwnTypeIsRejectedAsShadowing(test *testing.T) {
	test.Parallel()
	source := `package fixture

type composeContext struct{}

func compose(ctx *composeContext) *composeContext {
	if ctx == nil {
		return &composeContext{}
	}
	return ctx
}
`
	checked := typeCheck(test, source)
	candidates := findCandidates(checked.packageTypes, checked.info, checked.files, fixtureModule, fixtureWords(test))
	if len(candidates) != 1 || candidates[0].newName != "composeContext" {
		test.Fatalf("got %+v, want one composeContext proposal", candidates)
	}
	proposals := []rename.Proposal{{Object: candidates[0].variable, NewName: candidates[0].newName}}
	plan := rename.Check(
		rename.Package{FileSet: checked.fileSet, Types: checked.packageTypes, Info: checked.info, Files: checked.files},
		proposals,
	)
	if len(plan.Rejections) != 1 {
		test.Fatalf("got %d rejections, want 1", len(plan.Rejections))
	}
	proposedAt := map[string]string{checked.fileSet.Position(candidates[0].variable.Pos()).String(): "composeContext"}
	class := reasonClass(
		plan.Rejections[0].Reason,
		plan.Rejections[0].NewName,
		proposedAt,
		map[string]bool{"ctx": true},
	)
	if class != "conflict with a name already in scope" {
		test.Fatalf("classed %q as %q", plan.Rejections[0].Reason, class)
	}
}

// TestContextOverrides names the ctx of each overridden type by the table, and confirms the engine accepts the
// override where the type-derived name was refused for shadowing the type. The same type name in another
// package takes the type-derived name, which the engine refuses where the function reads the type after the
// local's declaration, and an exact Context in a package QualifierSpelling spells takes that spelling.
func TestContextOverrides(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name        string
		packagePath string
		source      string
		newName     string
		refused     bool
	}{
		{
			name:        "composeContext in the yaml compose package",
			packagePath: fixtureModule + "/internal/format/yaml/compose",
			source:      "package compose\n\ntype composeContext struct{}\n\nfunc compose(ctx *composeContext) *composeContext {\n\treturn ctx\n}\n",
			newName:     "composition",
		},
		{
			name:        "escapeContext in the ecmascript regexp package",
			packagePath: fixtureModule + "/internal/lint/ecmascript/regexp",
			source:      "package regexp\n\ntype escapeContext struct{}\n\nfunc escape(ctx escapeContext) escapeContext {\n\treturn ctx\n}\n",
			newName:     "escaping",
		},
		{
			name:        "a composeContext elsewhere is not overridden",
			packagePath: fixtureModule + "/fixture",
			source:      "package fixture\n\ntype composeContext struct{}\n\nfunc compose() {\n\tctx := composeContext{}\n\t_ = ctx\n\t_ = composeContext{}\n}\n",
			newName:     "composeContext",
			refused:     true,
		},
		{
			name:        "an exact Context in optionschema takes its camel spelling",
			packagePath: fixtureModule + "/fixture",
			source:      "package fixture\n\nimport \"example.com/own/optionschema\"\n\nfunc check(ctx optionschema.Context) {\n\t_ = ctx\n}\n",
			newName:     "optionSchemaContext",
		},
	}
	words := fixtureWords(test)
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			checked := typeCheckAs(test, testCase.packagePath, testCase.source)
			candidates := findCandidates(checked.packageTypes, checked.info, checked.files, fixtureModule, words)
			if len(candidates) != 1 || candidates[0].newName != testCase.newName {
				test.Fatalf("got %+v, want one %s proposal", candidates, testCase.newName)
			}
			proposals := []rename.Proposal{{Object: candidates[0].variable, NewName: candidates[0].newName}}
			plan := rename.Check(rename.Package{
				FileSet: checked.fileSet,
				Types:   checked.packageTypes,
				Info:    checked.info,
				Files:   checked.files,
			}, proposals)
			if refused := len(plan.Accepted) == 0; refused != testCase.refused {
				test.Fatalf(
					"engine refused %s: %v, want %v: %+v",
					testCase.newName,
					refused,
					testCase.refused,
					plan.Rejections,
				)
			}
		})
	}
}

// formatShimSource is the format shim's surface the table reads: WithFormatCodeSettings puts settings on a
// context, and WithoutSettings stands for any other call of the same package.
const formatShimSource = "package format\n\nimport \"context\"\n\n" +
	"func WithFormatCodeSettings(parent context.Context, settings int, newLine string) " +
	"context.Context { return parent }\n\n" +
	"func WithoutSettings(parent context.Context) context.Context { return parent }\n"

// TestFormatShimSettingsContext names the context the format shim puts settings on settingsContext, and leaves
// one from any other call of the shim for the waves.
func TestFormatShimSettingsContext(test *testing.T) {
	test.Parallel()
	const shimPath = "github.com/microsoft/TypeScript/tsc/shim/format"
	// One standard library importer for both packages, so both read the same context.Context.
	standardLibrary := importer.Default()
	shim := checkFile(test, shimPath, formatShimSource, standardLibrary)
	words := fixtureWords(test)
	for call, want := range map[string]candidate{
		"WithFormatCodeSettings(context.Background(), 1, \"\\n\")": {newName: "settingsContext"},
		"WithoutSettings(context.Background())":                    {category: "call not in the table"},
	} {
		checked := checkFile(
			test,
			fixtureModule+"/fixture",
			"package fixture\n\nimport (\n\t\"context\"\n\n\t\""+shimPath+"\"\n)\n\n"+
				"func formatted() context.Context { ctx := format."+call+"; return ctx }\n",
			fixtureImporter{
				packages: map[string]*types.Package{shimPath: shim.packageTypes},
				fallback: standardLibrary,
			},
		)
		candidates := findCandidates(checked.packageTypes, checked.info, checked.files, fixtureModule, words)
		if len(candidates) != 1 || candidates[0].newName != want.newName || candidates[0].category != want.category {
			test.Errorf("%s: got %+v, want name %q category %q", call, candidates, want.newName, want.category)
		}
	}
}
