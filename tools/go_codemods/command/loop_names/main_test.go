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

// fixtureVocabulary is a slice of policy/Abbreviations.json in its own shape: a plain word and an advice-only
// word.
const fixtureVocabulary = `{
	"abbreviations": [
		{"abbreviation": "cmd", "expansions": ["command"], "whole": {"style": "orDescriptive"}, "prefix": {"phase": "late"}, "segment": {}},
		{"abbreviation": "arg", "expansions": ["argument"], "advice": "Use argument.", "whole": {"style": "advice"}, "prefix": {"phase": "late", "style": "advice"}, "suffix": {}}
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
	return typeCheckFile(test, "fixture.go", source)
}

// typeCheckFile type-checks source as the one file of package fixture, under the given base name.
func typeCheckFile(test *testing.T, baseName string, source string) fixture {
	test.Helper()
	filename := filepath.Join(test.TempDir(), baseName)
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

// outcomes prints each candidate in declaration order as `letter:newName`, or `letter=category` when it is
// left for the waves.
func outcomes(candidates []candidate) []string {
	printed := []string{}
	for _, decided := range candidates {
		if len(decided.names) > 0 {
			printed = append(printed, decided.variable.Name()+":"+strings.Join(decided.names, "|"))
		} else {
			printed = append(printed, decided.variable.Name()+"="+decided.category)
		}
	}
	return printed
}

func TestLoopNames(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "slices.SortFunc comparator parameters become left and right",
			body: "func caller(numbers []int) { slices.SortFunc(numbers, func(a, b int) int { return a - b }) }",
			want: []string{"a:left", "b:right"},
		},
		{
			name: "sort.Slice comparator parameters become left and right",
			body: "func caller(items []int) { sort.Slice(items, func(i, j int) bool { return items[i] < items[j] }) }",
			want: []string{"i:left", "j:right"},
		},
		{
			name: "a pair of parameters of one type in a literal passed elsewhere are left and right",
			body: "func apply(combine func(int, int) int) int { return combine(1, 2) }\nfunc caller() int { return apply(func(a, b int) int { return a + b }) }",
			want: []string{"a:left", "b:right"},
		},
		{
			name: "range key and value over a plural slice",
			body: "func caller(lines []string) int { total := 0; for i, l := range lines { total += i + len(l) }; return total }",
			want: []string{"i:index|lineIndex", "l:line"},
		},
		{
			name: "range value over entries is entry",
			body: "func caller(entries []string) int { total := 0; for _, e := range entries { total += len(e) }; return total }",
			want: []string{"e:entry"},
		},
		{
			name: "range value over a selected plural",
			body: "type file struct{ fileNames []string }\nfunc caller(current file) int { total := 0; for _, f := range current.fileNames { total += len(f) }; return total }",
			want: []string{"f:fileName"},
		},
		{
			name: "range value over a collection whose name is not plural takes its letter's convention",
			body: "func caller(text string) int { total := 0; for _, c := range text { total += int(c) }; return total }",
			want: []string{"c:character"},
		},
		{
			name: "range value singular expanded through the vocabulary",
			body: "func caller(cmds []string) int { total := 0; for _, c := range cmds { total += len(c) }; return total }",
			want: []string{"c:command"},
		},
		{
			name: "range value singular holding an advice-only word",
			body: "func caller(args []string) int { total := 0; for _, a := range args { total += len(a) }; return total }",
			want: []string{"a=range value: advice-only word in the name"},
		},
		{
			name: "range value singular that would be predeclared takes its letter's convention",
			body: "func caller(bytes []byte) int { total := 0; for _, b := range bytes { total += int(b) }; return total }",
			want: []string{"b:character"},
		},
		{
			name: "map key and value",
			body: "func caller(counts map[string]int) int { total := 0; for k, v := range counts { total += len(k) + v }; return total }",
			want: []string{"k:key|countKey", "v:value|countValue"},
		},
		{
			name: "three-clause counter",
			body: "func caller(items []int) int { total := 0; for i := 0; i < len(items); i++ { total += items[i] }; return total }",
			want: []string{"i:index|itemIndex"},
		},
		{
			name: "nested pair of ranges takes singular-based names",
			body: "func caller(lines []string, words []string) int { total := 0; for i := range lines { for j := range words { total += i * j } }; return total }",
			want: []string{"i:lineIndex", "j:wordIndex"},
		},
		{
			name: "nested three-clause counters with no collection are left",
			body: "func caller() int { total := 0; for i := 0; i < 3; i++ { for j := 0; j < 4; j++ { total += i * j } }; return total }",
			want: []string{
				"i=nested loop: the condition measures no collection with len",
				"j=nested loop: the condition measures no collection with len",
			},
		},
		{
			name: "a nested range and a map range want different names",
			body: "func caller(lines []string, counts map[string]int) int { total := 0; for i := range lines { for k := range counts { total += i + len(k) } }; return total }",
			want: []string{"i:index|lineIndex", "k:key|countKey"},
		},
		{
			name: "range over an integer is an index loop",
			body: "func caller() int { total := 0; for i := range 10 { total += i }; return total }",
			want: []string{"i:index"},
		},
		{
			name: "range key letter other than i, j, k or n",
			body: "func caller(lines []string) int { total := 0; for p := range lines { total += p }; return total }",
			want: []string{"p=index key letter other than i, j, k or n"},
		},
		{
			name: "coordinates and blanks are never candidates",
			body: "func caller(xs []float64) float64 { total := 0.0; for _, x := range xs { y := x * 2; total += y }; return total }",
			want: []string{},
		},
		{
			name: "letter conventions for basic types, and a letter with none left for the waves",
			body: "func caller(s string) (r int) { n := len(s); return n }",
			want: []string{
				"s:text",
				"r=no rule names a basic type (int) held by r",
				"n=no rule names a basic type (int) held by n",
			},
		},
	}
	words := fixtureWords(test)
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			source := "package fixture\n\nimport (\n\t\"slices\"\n\t\"sort\"\n)\n\n" +
				"var _, _ = slices.Sort[[]int], sort.Ints\n\n" +
				testCase.body +
				"\n"
			checked := typeCheck(test, source)
			got := outcomes(findCandidates(checked.packageTypes, checked.info, checked.fileSet, checked.files, words))
			if !reflect.DeepEqual(got, testCase.want) {
				test.Fatalf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestSingularWord(test *testing.T) {
	test.Parallel()
	testCases := []struct {
		plural   string
		singular string
		isPlural bool
	}{
		{plural: "entries", singular: "entry", isPlural: true},
		{plural: "properties", singular: "property", isPlural: true},
		{plural: "matches", singular: "match", isPlural: true},
		{plural: "classes", singular: "class", isPlural: true},
		{plural: "boxes", singular: "box", isPlural: true},
		{plural: "prefixes", singular: "prefix", isPlural: true},
		{plural: "lines", singular: "line", isPlural: true},
		{plural: "values", singular: "value", isPlural: true},
		{plural: "caches", singular: "cache", isPlural: true},
		{plural: "children", singular: "child", isPlural: true},
		{plural: "status", isPlural: false},
		{plural: "analysis", isPlural: false},
		{plural: "series", isPlural: false},
		{plural: "text", isPlural: false},
		{plural: "ids", isPlural: false},
	}
	for _, testCase := range testCases {
		singular, plural := singularWord(testCase.plural)
		if singular != testCase.singular || plural != testCase.isPlural {
			test.Errorf(
				"%s: got %q %v, want %q %v",
				testCase.plural,
				singular,
				plural,
				testCase.singular,
				testCase.isPlural,
			)
		}
	}
}

// TestCollisionIsRejectedByTheEngine proposes index for a range key whose loop body already declares index
// and then reads the key, and confirms the engine refuses it while a sibling loop's key is accepted.
func TestCollisionIsRejectedByTheEngine(test *testing.T) {
	test.Parallel()
	source := `package fixture

func colliding(items []int) int {
	total := 0
	for i := range items {
		index := 2
		total += index + items[i]
	}
	return total
}

func clean(items []int) int {
	total := 0
	for i := range items {
		total += items[i]
	}
	return total
}
`
	checked := typeCheck(test, source)
	candidates := findCandidates(checked.packageTypes, checked.info, checked.fileSet, checked.files, fixtureWords(test))
	var proposals []rename.Proposal
	for _, decided := range candidates {
		if len(decided.names) == 0 || decided.names[0] != "index" {
			test.Fatalf("%s: got names %q category %q, want index", decided.variable, decided.names, decided.category)
		}
		proposals = append(proposals, rename.Proposal{Object: decided.variable, NewName: decided.names[0]})
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
		test.Fatalf("rejected the key declared on line %d, want the colliding one on line 5", line)
	}
}
