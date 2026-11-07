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
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/tools/go_codemods/rename"
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

// fixtureVocabulary is a small stand-in for policy/Abbreviations.json.
var fixtureVocabulary = map[string]bool{"cfg": true, "cmd": true, "config": true, "ctx": true, "env": true, "err": true}

// typeCheckFixture parses and type-checks the files in directory as one package.
func typeCheckFixture(
	test *testing.T,
	directory string,
	filenames []string,
) (*token.FileSet, *types.Package, *types.Info, []*ast.File) {
	test.Helper()
	fileSet := token.NewFileSet()
	var files []*ast.File
	for _, filename := range filenames {
		file, parseError := parser.ParseFile(fileSet, filepath.Join(directory, filename), nil, parser.ParseComments)
		if parseError != nil {
			test.Fatalf("parsing: %v", parseError)
		}
		files = append(files, file)
	}
	info := &types.Info{
		Defs:      map[*ast.Ident]types.Object{},
		Uses:      map[*ast.Ident]types.Object{},
		Implicits: map[ast.Node]types.Object{},
		Scopes:    map[ast.Node]*types.Scope{},
		Types:     map[ast.Expr]types.TypeAndValue{},
	}
	configuration := types.Config{Importer: importer.Default()}
	packageTypes, checkError := configuration.Check("fixture", fileSet, files, info)
	if checkError != nil {
		test.Fatalf("type-checking: %v", checkError)
	}
	return fileSet, packageTypes, info, files
}

// planFixture writes sources (filename to source) as one package, plans its receivers with every file but
// skipped editable, applies the edits, type-checks the result, and returns the plan and the rewritten sources.
func planFixture(test *testing.T, sources map[string]string, skipped ...string) (receiverPlan, map[string]string) {
	test.Helper()
	directory := test.TempDir()
	var filenames []string
	for filename, source := range sources {
		if writeError := os.WriteFile(filepath.Join(directory, filename), []byte(source), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	fileSet, packageTypes, info, files := typeCheckFixture(test, directory, filenames)
	var editableFiles []*ast.File
	for index, file := range files {
		isSkipped := false
		for _, skip := range skipped {
			if filenames[index] == skip {
				isSkipped = true
			}
		}
		if !isSkipped {
			editableFiles = append(editableFiles, file)
		}
	}
	renamePackage := rename.Package{FileSet: fileSet, Types: packageTypes, Info: info, Files: editableFiles}
	plan := planReceivers(renamePackage, files, fixtureVocabulary)
	for filename, edits := range rename.GroupByFile(rename.Edits(renamePackage, plan.accepted)) {
		_, rewritten, rewriteError := rename.RewriteFile(filename, edits)
		if rewriteError != nil {
			test.Fatal(rewriteError)
		}
		if writeError := os.WriteFile(filename, rewritten, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	typeCheckFixture(test, directory, filenames)
	rewritten := map[string]string{}
	for _, filename := range filenames {
		contents, readError := os.ReadFile(filepath.Join(directory, filename))
		if readError != nil {
			test.Fatal(readError)
		}
		rewritten[filename] = string(contents)
	}
	return plan, rewritten
}

func TestCamelWords(test *testing.T) {
	test.Parallel()
	for name, want := range map[string][]string{
		"HTTPServer":   {"HTTP", "Server"},
		"parseJSON":    {"parse", "JSON"},
		"swiftRun":     {"swift", "Run"},
		"Engine":       {"Engine"},
		"IDs":          {"IDs"},
		"URLsFor":      {"URLs", "For"},
		"sha256Hasher": {"sha256", "Hasher"},
		"nodeType":     {"node", "Type"},
		"has_under":    nil,
	} {
		if got := camelWords(name); !reflect.DeepEqual(got, want) {
			test.Errorf("camelWords(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestReceiverNames(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name string
		// sources is one package; a.go is checked against want and unwanted, a_test.go against wantInTest.
		sources    map[string]string
		skipped    []string
		want       []string
		wantInTest []string
		unwanted   []string
		dropped    string
	}{
		{
			name: "last word of the type",
			sources: map[string]string{"a.go": `package fixture

type swiftRun struct{ name string }

func (r *swiftRun) Name() string { return r.name }
`},
			want: []string{"func (run *swiftRun) Name() string { return run.name }"},
		},
		{
			name: "one-word type",
			sources: map[string]string{"a.go": `package fixture

type Engine struct{ count int }

func (e *Engine) Count() int { return e.count }
func (e Engine) Twice() int  { return e.count * 2 }
`},
			want: []string{
				"func (engine *Engine) Count() int { return engine.count }",
				"func (engine Engine) Twice() int",
			},
		},
		{
			name: "keyword last word takes two words",
			sources: map[string]string{"a.go": `package fixture

type astNodeType int

func (n astNodeType) Next() astNodeType { return n + 1 }
`},
			want: []string{"func (nodeType astNodeType) Next() astNodeType { return nodeType + 1 }"},
		},
		{
			name: "predeclared last word takes two words",
			sources: map[string]string{"a.go": `package fixture

type parseFooError struct{ message string }

func (e *parseFooError) Error() string { return e.message }
`},
			want: []string{"func (fooError *parseFooError) Error() string { return fooError.message }"},
		},
		{
			name: "a local named like the target moves the whole type to two words",
			sources: map[string]string{"a.go": `package fixture

type cachedSwiftRun struct{ name string }

func (r *cachedSwiftRun) Name() string { return r.name }

func (r *cachedSwiftRun) Twice() string {
	run := r.name + r.name
	return run
}
`},
			want: []string{
				"func (swiftRun *cachedSwiftRun) Name() string { return swiftRun.name }",
				"func (swiftRun *cachedSwiftRun) Twice() string",
			},
		},
		{
			name: "an import path base named like the target takes two words",
			sources: map[string]string{"a.go": `package fixture

import "go/token"

type jsonStreamToken struct{ position token.Pos }

func (s jsonStreamToken) Valid() bool { return s.position.IsValid() }
`},
			want: []string{"func (streamToken jsonStreamToken) Valid() bool { return streamToken.position.IsValid() }"},
		},
		{
			name: "an import alias named like the target takes two words, even unused in the method",
			sources: map[string]string{"a.go": `package fixture

import parser "strings"

type fastJSONParser struct{ input string }

func (p fastJSONParser) Input() string { return p.input }

var _ = parser.ToUpper
`},
			want: []string{"func (jsonParser fastJSONParser) Input() string { return jsonParser.input }"},
		},
		{
			name: "an initialism last word is lowercased whole",
			sources: map[string]string{"a.go": `package fixture

type parseHTML struct{ text string }

func (p parseHTML) Text() string { return p.text }
`},
			want: []string{"func (html parseHTML) Text() string { return html.text }"},
		},
		{
			name: "a standard library package name takes two words, imported or not",
			sources: map[string]string{"a.go": `package fixture

type parseJSON struct{ text string }

func (p parseJSON) Text() string { return p.text }

type requestStateContext struct{ depth int }

func (ctx *requestStateContext) Depth() int { return ctx.depth }
`},
			want: []string{
				"func (parseJson parseJSON) Text() string { return parseJson.text }",
				"func (stateContext *requestStateContext) Depth() int { return stateContext.depth }",
			},
		},
		{
			name: "a negation before the fallback words joins them",
			sources: map[string]string{"a.go": `package fixture

type ruleNotAllowedError struct{ rule string }

func (err ruleNotAllowedError) Error() string { return err.rule }
`},
			want: []string{"func (notAllowedError ruleNotAllowedError) Error() string { return notAllowedError.rule }"},
		},
		{
			name: "a negation before the last word joins it",
			sources: map[string]string{"a.go": `package fixture

type cacheNotFound struct{ key string }

func (c cacheNotFound) Key() string { return c.key }

type nonEmptyNoMatch struct{ pattern string }

func (m nonEmptyNoMatch) Pattern() string { return m.pattern }
`},
			want: []string{
				"func (notFound cacheNotFound) Key() string { return notFound.key }",
				"func (noMatch nonEmptyNoMatch) Pattern() string { return noMatch.pattern }",
			},
		},
		{
			name: "an override is applied before the computed name",
			sources: map[string]string{"a.go": `package fixture

type composeContext struct{ depth int }

func (ctx *composeContext) Depth() int { return ctx.depth }

type importerFromMap func(path string) int

func (i importerFromMap) Import(path string) int { return i(path) }
`},
			want: []string{
				"func (composition *composeContext) Depth() int { return composition.depth }",
				"func (importer importerFromMap) Import(path string) int { return importer(path) }",
			},
		},
		{
			name: "an override that is an import in the file leaves the type",
			sources: map[string]string{"a.go": `package fixture

import "go/importer"

type importerFromMap func(path string) int

func (i importerFromMap) Import(path string) int { return i(path) }

var _ = importer.Default
`},
			want:    []string{"func (i importerFromMap) Import(path string) int { return i(path) }"},
			dropped: "override unusable",
		},
		{
			name: "an override the engine rejects leaves the type without falling back",
			sources: map[string]string{"a.go": `package fixture

type composeContext struct{ depth int }

func (ctx *composeContext) Depth() int {
	composition := ctx.depth
	return composition
}
`},
			want:    []string{"func (ctx *composeContext) Depth() int {"},
			dropped: "engine rejected the override",
		},
		{
			name: "an imported initialism takes two words",
			sources: map[string]string{"a.go": `package fixture

import "encoding/json"

type fastParseJSON struct{ text string }

func (p fastParseJSON) Valid() bool { return json.Valid([]byte(p.text)) }
`},
			want: []string{"func (parseJson fastParseJSON) Valid() bool { return json.Valid([]byte(parseJson.text)) }"},
		},
		{
			name: "a file system initialism is spelled out, and a receiver already in capitals is renamed again",
			sources: map[string]string{"a.go": `package fixture

type memoryFS struct{ files map[string]string }

func (m *memoryFS) Read(name string) string { return m.files[name] }

type listingFS struct{ names []string }

func (listingFS *listingFS) Count() int { return len(listingFS.names) }
`},
			want: []string{
				"func (fileSystem *memoryFS) Read(name string) string { return fileSystem.files[name] }",
				"func (fileSystem *listingFS) Count() int { return len(fileSystem.names) }",
			},
		},
		{
			name: "generic receiver",
			sources: map[string]string{"a.go": `package fixture

type Set[T comparable] struct{ members map[T]bool }

func (s *Set[T]) Add(value T) { s.members[value] = true }
`},
			want: []string{"func (set *Set[T]) Add(value T) { set.members[value] = true }"},
		},
		{
			name: "already spelled receivers are untouched, and short ones join them",
			sources: map[string]string{"a.go": `package fixture

type Engine struct{ count int }

func (engine *Engine) Count() int { return engine.count }
func (e *Engine) Twice() int      { return e.count * 2 }

type Runner struct{}

func (runner Runner) Run() {}
`},
			want: []string{
				"func (engine *Engine) Count() int",
				"func (engine *Engine) Twice() int",
				"func (runner Runner) Run() {}",
			},
		},
		{
			name: "a vocabulary receiver is renamed",
			sources: map[string]string{"a.go": `package fixture

type httpServer struct{ port int }

func (cfg httpServer) Port() int { return cfg.port }
`},
			want: []string{"func (server httpServer) Port() int { return server.port }"},
		},
		{
			// The spec takes two words when the last is an abbreviation, but `serverConfig` would still hold one,
			// so the type is left for the waves rather than renamed to a name the abbreviation rule flags.
			name: "a vocabulary last word leaves the type when the two words still hold it",
			sources: map[string]string{"a.go": `package fixture

type httpServerConfig struct{ port int }

func (cfg httpServerConfig) Port() int { return cfg.port }
`},
			want:    []string{"func (cfg httpServerConfig) Port() int { return cfg.port }"},
			dropped: "no usable name",
		},
		{
			name: "receivers move together across files, test files included",
			sources: map[string]string{
				"a.go": `package fixture

type swiftRun struct{ name string }

func (r *swiftRun) Name() string { return r.name }
`,
				"a_test.go": `package fixture

func (r *swiftRun) Debug() string { return "run " + r.name }
`,
			},
			want:       []string{"func (run *swiftRun) Name() string { return run.name }"},
			wantInTest: []string{`func (run *swiftRun) Debug() string { return "run " + run.name }`},
		},
		{
			name: "both names rejected leaves every method of the type",
			sources: map[string]string{"a.go": `package fixture

type cachedSwiftRun struct{ name string }

func (r *cachedSwiftRun) Name() string { return r.name }

func (r *cachedSwiftRun) Both() string {
	run, swiftRun := r.name, r.name
	return run + swiftRun
}
`},
			want: []string{
				"func (r *cachedSwiftRun) Name() string { return r.name }",
				"func (r *cachedSwiftRun) Both() string",
			},
			dropped: "engine rejected every candidate",
		},
		{
			name: "a one-word type whose name is rejected is left",
			sources: map[string]string{"a.go": `package fixture

type Engine struct{ count int }

func (e *Engine) Count() int { return e.count }

func (e *Engine) Reset() {
	engine := Engine{}
	*e = engine
}
`},
			want:    []string{"func (e *Engine) Count() int { return e.count }", "func (e *Engine) Reset() {"},
			dropped: "engine rejected the one-word name, no two-word name",
		},
		{
			name: "a receiver to rename in a skipped file leaves the type",
			sources: map[string]string{
				"a.go": `package fixture

type swiftRun struct{ name string }

func (r *swiftRun) Name() string { return r.name }
`,
				"b.go": `package fixture

func (r *swiftRun) Other() string { return r.name }
`,
			},
			skipped: []string{"b.go"},
			want:    []string{"func (r *swiftRun) Name() string { return r.name }"},
			dropped: "a receiver to rename is in a skipped or generated file",
		},
		{
			name: "unnamed and blank receivers are left alone",
			sources: map[string]string{"a.go": `package fixture

type swiftRun struct{}

func (*swiftRun) One() {}
func (_ *swiftRun) Two() {}
`},
			want: []string{"func (*swiftRun) One() {}", "func (_ *swiftRun) Two() {}"},
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			plan, rewritten := planFixture(test, testCase.sources, testCase.skipped...)
			for _, want := range testCase.wantInTest {
				if !strings.Contains(rewritten["a_test.go"], want) {
					test.Errorf("missing %q in:\n%s", want, rewritten["a_test.go"])
				}
			}
			for _, want := range testCase.want {
				if !strings.Contains(rewritten["a.go"], want) {
					test.Errorf("missing %q in:\n%s", want, rewritten["a.go"])
				}
			}
			for _, unwanted := range testCase.unwanted {
				if strings.Contains(rewritten["a.go"], unwanted) {
					test.Errorf("unexpected %q in:\n%s", unwanted, rewritten["a.go"])
				}
			}
			var droppedClasses []string
			for _, dropped := range plan.dropped {
				droppedClasses = append(droppedClasses, dropped.class)
			}
			switch {
			case testCase.dropped == "" && len(plan.dropped) > 0:
				test.Errorf("dropped %+v, want none", plan.dropped)
			case testCase.dropped != "" && !reflect.DeepEqual(droppedClasses, []string{testCase.dropped}):
				test.Errorf("dropped classes %q, want [%q]", droppedClasses, testCase.dropped)
			}
			if len(plan.mixed) > 0 {
				test.Errorf("mixed types after the pass: %+v", plan.mixed)
			}
		})
	}
}

// TestReceiverNamedLikeItsType covers a receiver whose new name is its own type's name: a two-word type's
// fallback (`nodeType`) and a lowercase one-word type (`parser`). Go scopes a receiver to the body, so the
// signature still names the type; a use of the type inside the body would be a real conflict.
func TestReceiverNamedLikeItsType(test *testing.T) {
	test.Parallel()
	plan, rewritten := planFixture(test, map[string]string{"a.go": `package fixture

type nodeType int

func (n nodeType) Next() nodeType { return n + 1 }

type parser struct{ position int }

func (p *parser) Advance() *parser {
	p.position++
	return p
}

type lexer struct{ position int }

func (l *lexer) Fresh() *lexer { return &lexer{position: l.position} }
`})
	for _, want := range []string{
		"func (nodeType nodeType) Next() nodeType { return nodeType + 1 }",
		"func (parser *parser) Advance() *parser {\n\tparser.position++\n\treturn parser",
		"func (l *lexer) Fresh() *lexer { return &lexer{position: l.position} }",
	} {
		if !strings.Contains(rewritten["a.go"], want) {
			test.Errorf("missing %q in:\n%s", want, rewritten["a.go"])
		}
	}
	if len(plan.dropped) != 1 || plan.dropped[0].typeName != "lexer" {
		test.Errorf("dropped = %+v, want lexer only, whose body names its type", plan.dropped)
	}
}

func TestMixedNamesAreReported(test *testing.T) {
	test.Parallel()
	plan, rewritten := planFixture(test, map[string]string{"a.go": `package fixture

type Engine struct{ count int }

func (machine *Engine) Count() int { return machine.count }
func (e *Engine) Twice() int       { return e.count * 2 }
`})
	if !strings.Contains(rewritten["a.go"], "func (engine *Engine) Twice() int") {
		test.Errorf("short receiver not renamed:\n%s", rewritten["a.go"])
	}
	if len(plan.mixed) != 1 || plan.mixed[0].names != "engine x1, machine x1" {
		test.Errorf("mixed = %+v, want Engine with engine and machine", plan.mixed)
	}
}
