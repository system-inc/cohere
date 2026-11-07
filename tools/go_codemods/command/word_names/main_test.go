package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_rename/rename"
)

// testVocabularyPath is the dry-run tree's policy file, the one the command reads there. WORDNAMES_VOCABULARY
// overrides it.
func testVocabularyPath() string {
	if path := os.Getenv("WORDNAMES_VOCABULARY"); path != "" {
		return path
	}
	// cohere-go 1c77e4a7's policy/Abbreviations.json, the first with a list of expansions per word.
	return "testdata/Abbreviations.json"
}

func loadTestVocabulary(test *testing.T) *vocabulary {
	test.Helper()
	data, readError := os.ReadFile(testVocabularyPath())
	if readError != nil {
		test.Fatalf("reading the vocabulary: %v", readError)
	}
	loaded, loadError := loadVocabulary(data)
	if loadError != nil {
		test.Fatalf("loading the vocabulary: %v", loadError)
	}
	return loaded
}

// fixturePackages are the packages the fixtures import, each checked from its source: a package named
// command, the lint rule package whose OptionsAs decodes a rule's options, and a configuration package two
// directories under lint, so a fixture needs no standard library.
var fixturePackages = map[string]string{
	"example.com/command": "package command\n\nconst Name = \"command\"\n",
	"github.com/system-inc/cohere/internal/lint/rule": "package rule\n\n" +
		"func OptionsAs[Options any](options any) (Options, bool) { decoded, ok := options.(Options); return decoded, ok }\n",
	"example.com/internal/lint/configuration": "package configuration\n\ntype Config struct{ Root string }\n",
	"example.com/internal/gotypes":            "package gotypes\n\ntype Package struct{ Path string }\n",
	"example.com/go/ast":                      "package ast\n\ntype Package struct{ Name string }\n",
	"example.com/go_tools/ast":                "package ast\n\ntype Package struct{ Name string }\n",
}

// fixtureImporter type-checks the fixture packages on demand.
type fixtureImporter struct{}

func (fixtureImporter) Import(path string) (*types.Package, error) {
	source, known := fixturePackages[path]
	if !known {
		return nil, fmt.Errorf("no package %q in the fixtures", path)
	}
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, path+".go", source, 0)
	if parseError != nil {
		return nil, parseError
	}
	return (&types.Config{}).Check(path, fileSet, []*ast.File{file}, nil)
}

func typeCheck(test *testing.T, source string) rename.Package {
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
	configuration := types.Config{Importer: fixtureImporter{}}
	packageTypes, checkError := configuration.Check("fixture", fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("type-checking: %v", checkError)
	}
	return rename.Package{FileSet: fileSet, Types: packageTypes, Info: info, Files: []*ast.File{file}}
}

// TestWordnamesRenamesLocalsAsTheFormsSay runs each fixture through planPackage and the engine, and checks
// what became of the local named in the case: its new name, or that it kept its name, and why.
func TestWordnamesRenamesLocalsAsTheFormsSay(test *testing.T) {
	test.Parallel()
	loadedVocabulary := loadTestVocabulary(test)
	cases := []struct {
		name   string
		source string
		local  string
		// want is the new name, or empty when the local keeps its name.
		want string
		// outcome, when set, must appear in the local's outcome; "none" means the vocabulary flags nothing.
		outcome string
	}{
		{"whole cfg", `func f(cfg int) int { return cfg }`, "cfg", "configuration", outcomeProposed},
		{
			"prefix src, then nothing",
			`func f() string { srcPath := "a"; return srcPath }`,
			"srcPath",
			"sourcePath",
			outcomeProposed,
		},
		{
			"prefix tmp, then segment Dir",
			`func f() string { tmpDir := "a"; return tmpDir }`,
			"tmpDir",
			"temporaryDirectory",
			outcomeProposed,
		},
		{
			"late prefix max, then segment Len",
			`func f() int { maxLen := 1; return maxLen }`,
			"maxLen",
			"maximumLength",
			outcomeProposed,
		},
		{
			"millisecond suffix",
			`func f() int { elapsedMs := 1; return elapsedMs }`,
			"elapsedMs",
			"elapsedInMilliseconds",
			outcomeProposed,
		},
		{"segment Cmd", `func f() int { runCmd := 1; return runCmd }`, "runCmd", "runCommand", outcomeProposed},
		{
			"whole cmd with no import named command",
			`func f() int { cmd := 1; return cmd }`,
			"cmd",
			"command",
			outcomeProposed,
		},
		{"whole pkg would be a keyword, and an int names nothing", `func f() int { pkg := 1; return pkg }`, "pkg", "",
			"would become a Go keyword, and no qualifier rule names it"},
		{
			"whole pkg in a string holds a package's path",
			`func f(pkg string) string { return pkg }`,
			"pkg",
			"packagePath",
			outcomeProposed,
		},
		{"whole str would be predeclared, and an int names nothing", `func f() int { str := 1; return str }`, "str", "",
			"would become a predeclared identifier, and no qualifier rule names it"},
		{"whole str in a string is text", `func f(str string) string { return str }`, "str", "text", outcomeProposed},
		{"whole cmd beside an import named command",
			"import \"example.com/command\"\n\nvar _ = command.Name\n\nfunc f() int { cmd := 1; return cmd }",
			"cmd", "", "would become the name of an import in the file, and no qualifier rule names it"},
		{"config of the imported configuration type takes the directory that holds it",
			"import \"example.com/internal/lint/configuration\"\n\nfunc f(config *configuration.Config) string { return config.Root }",
			"config", "lintConfiguration", outcomeProposed},
		{"config of another type beside the import takes its type's name",
			"import \"example.com/internal/lint/configuration\"\n\ntype parsedCommandLine struct{ root string }\n\n" +
				"var _ configuration.Config\n\nfunc f(config parsedCommandLine) string { return config.root }",
			"config", "parsedCommandLine", outcomeProposed},
		{"pos beside a type named position is still refused, since the type cannot say position from positive",
			"type position struct{ line int }\n\ntype lineAndColumn struct{ line int }\n\n" +
				"func at() lineAndColumn { return lineAndColumn{} }\n\n" +
				"func f() *position { pos := at(); return &position{line: pos.line} }",
			"pos", "", `"pos" has several meanings (position, positive)`},
		{"pos one past a field named position is refused, since it may still mean positive",
			"type parser struct{ position int }\n\nfunc position() int { return 0 }\n\n" +
				"func (p *parser) f() int { pos := p.position + 1; return pos + position() }",
			"pos", "", `"pos" has several meanings (position, positive)`},
		{"pos with no rule to qualify it is refused, never given its first meaning",
			"func position() int { return 0 }\n\nfunc f() int { pos := 2; return pos + position() }",
			"pos", "", `"pos" has several meanings (position, positive)`},
		{"pkg ranging over a call's packages is one of them",
			"import \"example.com/internal/gotypes\"\n\nfunc lintPackages() []*gotypes.Package { return nil }\n\n" +
				"func f() { for _, pkg := range lintPackages() { _ = pkg.Path } }",
			"pkg", "lintPackage", outcomeProposed},
		{"pkg ranging over a field is one of what it holds",
			"import \"example.com/internal/gotypes\"\n\ntype loadResult struct{ Source []*gotypes.Package }\n\n" +
				"func f(loaded loadResult) { for _, pkg := range loaded.Source { _ = pkg.Path } }",
			"pkg", "sourcePackage", outcomeProposed},
		{"pkg ranging over a variable is one of what it holds",
			"import \"example.com/internal/gotypes\"\n\nfunc f(loaded []*gotypes.Package) { for _, pkg := range loaded { _ = pkg.Path } }",
			"pkg", "loadedPackage", outcomeProposed},
		{"pkg ranging over bare packages takes the directory that holds its type's package",
			"import \"example.com/go/ast\"\n\nfunc f(packages map[string]*ast.Package) { for _, pkg := range packages { _ = pkg.Name } }",
			"pkg", "goPackage", outcomeProposed},
		{"pkg whose type's package sits in a folder of separated words takes those words as a Go name",
			"import \"example.com/go_tools/ast\"\n\nfunc f(packages map[string]*ast.Package) { for _, pkg := range packages { _ = pkg.Name } }",
			"pkg", "goToolsPackage", outcomeProposed},
		{"pkg whose type's package sits under internal takes nothing from internal",
			"import \"example.com/internal/gotypes\"\n\nfunc f(packages []*gotypes.Package) { for _, pkg := range packages { _ = pkg.Path } }",
			"pkg", "", "would become a Go keyword, and no qualifier rule names it"},
		{"a renamed name writes its initialisms as words", `func f() bool { isURLArg := true; return isURLArg }`,
			"isURLArg", "isUrlArgument", outcomeProposed},
		{"opts beside a parameter named options are the rule's settings",
			"import \"github.com/system-inc/cohere/internal/lint/rule\"\n\n" +
				"type noFloatingOptions struct{ all bool }\n\n" +
				"func f(options any) bool { opts, ok := rule.OptionsAs[noFloatingOptions](options); return ok && opts.all }",
			"opts", "settings", outcomeProposed},
		{
			"Direction holds Dir only inside a word",
			`func f() int { Direction := 1; return Direction }`,
			"Direction",
			"",
			"none",
		},
		{
			"dir is judged only as a segment",
			`func f() string { dirName := "a"; return dirName }`,
			"dirName",
			"",
			"none",
		},
		{"bare dir is judged only as a segment", `func f() string { dir := "a"; return dir }`, "dir", "", "none"},
		{
			"sourceDir has the segment",
			`func f() string { sourceDir := "a"; return sourceDir }`,
			"sourceDir",
			"sourceDirectory",
			outcomeProposed,
		},
		{
			"ch that is a channel",
			`func f() chan int { ch := make(chan int); return ch }`,
			"ch",
			"channel",
			outcomeProposed,
		},
		{"ch in an int is a character", `func f(ch int) int { return ch }`, "ch", "character", outcomeProposed},
		{"ch in a rune is a character", `func f(ch rune) rune { return ch }`, "ch", "character", outcomeProposed},
		{"ch in a uint16 is a code unit", `func f(ch uint16) uint16 { return ch }`, "ch", "codeUnit", outcomeProposed},
		{
			"ch in a float names nothing",
			`func f(ch float64) float64 { return ch }`,
			"ch",
			"",
			`says "ch" is not a channel, and names nothing else`,
		},
		{
			"res that is not a response is a result",
			`func f() []int { res := []int{}; return res }`,
			"res",
			"result",
			outcomeProposed,
		},
		{
			"req in a bool is a requirement",
			`func f(reqSpace bool) bool { return reqSpace }`,
			"reqSpace",
			"requiresSpace",
			outcomeProposed,
		},
		{
			"req in an int names nothing",
			`func f(reqCount int) int { return reqCount }`,
			"reqCount",
			"",
			`says "req" is not a request, and names nothing else`,
		},
		{
			"num before a plural counts",
			`func f() int { numLines := 1; return numLines }`,
			"numLines",
			"lineCount",
			outcomeProposed,
		},
		{
			"num before a Latin plural",
			`func f() int { numQuasis := 1; return numQuasis }`,
			"numQuasis",
			"quasiCount",
			outcomeProposed,
		},
		{
			"num before bytes",
			`func f() int { numBytes := 1; return numBytes }`,
			"numBytes",
			"byteCount",
			outcomeProposed,
		},
		{
			"num before -ies",
			`func f() int { numEntries := 1; return numEntries }`,
			"numEntries",
			"entryCount",
			outcomeProposed,
		},
		{
			"num before two words",
			`func f() int { numOpenFiles := 1; return numOpenFiles }`,
			"numOpenFiles",
			"openFileCount",
			outcomeProposed,
		},
		{
			"num before an abbreviated plural",
			`func f() int { numParams := 1; return numParams }`,
			"numParams",
			"parameterCount",
			outcomeProposed,
		},
		{
			"num before a singular",
			`func f() int { numError := 1; return numError }`,
			"numError",
			"numberError",
			outcomeProposed,
		},
		{"num before -ches", `func f() int { numMatches := 1; return numMatches }`, "numMatches", "", "plural in -es"},
		{"num before an acronym", `func f() int { numIDs := 1; return numIDs }`, "numIDs", "", "opens with an acronym"},
		{
			"maxK keeps a letter its function never names",
			`func f() int { maxK := 1; return maxK }`,
			"maxK",
			"",
			`short camel word "K"`,
		},
		{
			"maxI keeps the name of the i declared before it",
			`func f() int { i := 0; maxI := 1; return maxI + i }`,
			"maxI",
			"maximumI",
			outcomeProposed,
		},
		{
			"maxI keeps the name of an i declared only after it",
			`func f() int { maxI := 1; i := 0; return maxI + i }`,
			"maxI",
			"",
			`short camel word "I"`,
		},
		{
			"minEl keeps two letters that name nothing",
			`func f() int { minEl := 1; nextEl := 2; return minEl + nextEl }`,
			"minEl",
			"",
			`short camel word "El"`,
		},
		{"cePos is refused in its segment form too", `func f(ce int) int { cePos := ce; return cePos }`, "cePos", "",
			`"pos" has several meanings (position, positive), so a person names it (segment form)`},
		{
			"refIndexA keeps the letter that pairs it with indexA",
			`func f(indexA int) int { refIndexA := indexA; return refIndexA }`,
			"refIndexA",
			"referenceIndexA",
			outcomeProposed,
		},
		{"refIndexA keeps a letter only its own name ends with", `func f() int { refIndexA := 1; return refIndexA }`,
			"refIndexA", "", `short camel word "A"`},
		{
			"isSrcToken keeps an English short word",
			`func f() bool { isSrcToken := true; return isSrcToken }`,
			"isSrcToken",
			"isSourceToken",
			outcomeProposed,
		},
		{"whole fn is advice-only", `func f(fn func()) { fn() }`, "fn", "", `advice-only word "fn"`},
		{
			"prefix arg is advice-only",
			`func f() int { argCount := 1; return argCount }`,
			"argCount",
			"",
			`advice-only word "arg" (prefix form)`,
		},
		{
			"suffix Arg is a rename",
			`func f() int { firstArg := 1; return firstArg }`,
			"firstArg",
			"firstArgument",
			outcomeProposed,
		},
		{
			"an allowed segment",
			`func f() int { mysqlInnoDbLimit := 1; return mysqlInnoDbLimit }`,
			"mysqlInnoDbLimit",
			"",
			"none",
		},
		// The allowed segment is capitalized, so a name that opens with its letters does not hold it, and the
		// rule flags that name too.
		{
			"an allowed segment's letters at the start",
			`func f() int { innoDbLimit := 1; return innoDbLimit }`,
			"innoDbLimit",
			"innoDatabaseLimit",
			outcomeProposed,
		},
		{"err is another generator's", `type e struct{}

func (e) Error() string { return "" }

func f() error { var err error = e{}; return err }`, "err", "", otherGeneratorPrefix + "`err`"},
		{
			"an abbreviated error is another generator's",
			`type e struct{}

func (e) Error() string { return "" }

func f() error { var cfgErr error = e{}; return cfgErr }`,
			"cfgErr",
			"",
			otherGeneratorPrefix + "a local of type error",
		},
		{
			"an err word in a non-error local",
			`func f() chan int { errCh := make(chan int); return errCh }`,
			"errCh",
			"",
			"holds err or ctx",
		},
		{"a receiver is another generator's", `type configuration struct{ value int }

func (cfg configuration) get() int { return cfg.value }`, "cfg", "", otherGeneratorPrefix + "receiver"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			renamePackage := typeCheck(test, "package fixture\n\n"+testCase.source+"\n")
			locals, _ := planPackage(renamePackage, loadedVocabulary)
			plan, _ := checkQualified(renamePackage, locals)
			if len(plan.Rejections) > 0 && testCase.want != "" {
				test.Fatalf("the engine rejected %+v", plan.Rejections)
			}

			var object types.Object
			for identifier, defined := range renamePackage.Info.Defs {
				if identifier.Name == testCase.local && defined != nil {
					object = defined
				}
			}
			if object == nil {
				test.Fatalf("no declaration of %s in the fixture", testCase.local)
			}
			if got := plan.Accepted[object]; got != testCase.want {
				test.Errorf("%s became %q, want %q", testCase.local, got, testCase.want)
			}

			outcome := "none"
			for _, local := range locals {
				if local.object == object {
					outcome = local.outcome
				}
			}
			if !strings.Contains(outcome, testCase.outcome) {
				test.Errorf("%s outcome %q, want one containing %q", testCase.local, outcome, testCase.outcome)
			}
		})
	}
}

// TestWordnamesFindAgreesWithTheRulesOwnCases runs the names the rule's own tests pin through this port's
// find: TestAbbreviationSegmentsJudgeOnlyTheWholeWord and TestAbbreviationVocabularyOrderIsObservableWhereItMatters
// in internal/lint/rules/nexus/abbreviation_vocabulary_test.go.
func TestWordnamesFindAgreesWithTheRulesOwnCases(test *testing.T) {
	test.Parallel()
	loadedVocabulary := loadTestVocabulary(test)
	for _, name := range []string{
		"checkName", "widgetCheckName", "changeSet", "posixPath", "postOffice", "bufferSize", "widgetBuffer",
		"muteButton", "multiplier", "temporaryFile", "sourceMap", "commandLine", "packageName", "position",
	} {
		if found, matched := loadedVocabulary.find(name); matched {
			test.Errorf("%q reads as an abbreviation: %+v", name, found)
		}
	}
	for _, name := range []string{"chList", "readCh", "posValue", "bufSize", "widgetMu", "sb"} {
		if _, matched := loadedVocabulary.find(name); !matched {
			test.Errorf("%q holds a whole abbreviated word and was not judged", name)
		}
	}
	for name, want := range map[string]string{
		"themeVarsVar": "themeVariablesVar",
		"timeoutMsRef": "timeoutInMillisecondsRef",
		"dbVal":        "databaseVal",
	} {
		if found, _ := loadedVocabulary.find(name); found.suggestion != want {
			test.Errorf("%q suggests %q, want %q", name, found.suggestion, want)
		}
	}
}

// TestWordnamesCountsNonLocalsWithoutProposing checks a field, a function, a method and a package-level
// variable holding a vocabulary word are counted by kind and never proposed.
func TestWordnamesCountsNonLocalsWithoutProposing(test *testing.T) {
	test.Parallel()
	renamePackage := typeCheck(test, `package fixture

var defaultCfg = 1

type server struct{ srcPath string }

func loadCfg() int { return defaultCfg }

func (server) runCmd() {}
`)
	locals, nonLocals := planPackage(renamePackage, loadTestVocabulary(test))
	if len(locals) != 0 {
		test.Errorf("locals %+v, want none", locals)
	}
	got := map[string]string{}
	for _, nonLocal := range nonLocals {
		got[nonLocal.name] = nonLocal.kind
	}
	want := map[string]string{
		"defaultCfg": "package variable",
		"srcPath":    "struct field",
		"loadCfg":    "function",
		"runCmd":     "method",
	}
	for name, kind := range want {
		if got[name] != kind {
			test.Errorf("%s counted as %q, want %q (all: %v)", name, got[name], kind, got)
		}
	}
}
