package types_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/variational_types/canonical"
	"github.com/system-inc/cohere/internal/variational_types/types"
)

// The builds of the fixture: bit 0 is darwin, bit 1 linux, bit 2 windows.
const (
	buildDarwin  types.Mask = 1 << 0
	buildLinux   types.Mask = 1 << 1
	buildWindows types.Mask = 1 << 2
	everyBuild              = buildDarwin | buildLinux | buildWindows
)

// fixtureFiles is the census's fork fixture with the Square's contagion case beside it: a struct whose unexported
// field differs by build, reached through a pointer by a body that only passes the pointer.
var fixtureFiles = []struct {
	name   string
	builds types.Mask
	source string
}{
	{"shared.go", everyBuild, `package fork

import "unsafe"

func F() int { return N * 3 }

func Untouched() int { return 7 }

func WordSize() uintptr { return unsafe.Sizeof(uintptr(0)) }

const IFMT = 0xf000

func IsPipe(stat Stat) bool { return stat.Mode&IFMT != 0 }

func Signal() error { return Kill(1, 9) }

type File struct{ *file }

func Pass(f *File) *File { return f }

func Size() uintptr { return unsafe.Sizeof(File{}) }

func InnerSize() uintptr { return unsafe.Sizeof(file{}) }

func Same(left File, right File) bool { return left == right }

const separator = string(Separator)

func Join(left string, right string) string { return left + separator + right }
`},
	{"n_linux.go", buildLinux, `package fork

const N = 1

var Kill func(pid int, signum int) error

type Stat struct{ Mode uint32 }

type file struct{ fd int }

const Separator = '/'
`},
	{"n_windows.go", buildWindows, `package fork

const N = 2

var Kill func(pid int, sig int) error

type Stat struct{ Mode uint16 }

type file struct {
	handle uintptr
	name   string
}

const Separator = '\\'
`},
	{"n_other.go", buildDarwin, `package fork

const N = 3

var Kill func(pid int, sig int) error

type Stat struct{ Mode uint32 }

type file struct{ fd int }

const Separator = '/'
`},
}

// unsafeImporter imports unsafe, the fixture's one import.
type unsafeImporter struct{}

func (unsafeImporter) Import(path string) (*types.Package, error) { return types.Unsafe, nil }

// answer spells a recorded type and value as the census compares them.
func answer(tv types.TypeAndValue) string { return canonical.Answer(tv) }

// TestVariationalCheckProjectsToEachBuild checks the fixture once for every build, then checks each build alone the
// plain way, and requires every expression's projected answer to be the plain one, in every build.
func TestVariationalCheckProjectsToEachBuild(t *testing.T) {
	t.Parallel()
	fileSet := token.NewFileSet()
	var files []*ast.File
	masks := map[*ast.File]types.Mask{}
	for _, fixture := range fixtureFiles {
		parsed, parseError := parser.ParseFile(fileSet, fixture.name, fixture.source, parser.ParseComments)
		if parseError != nil {
			t.Fatal(parseError)
		}
		files = append(files, parsed)
		masks[parsed] = fixture.builds
	}
	variation := &types.Variation{Builds: everyBuild, Files: masks}
	configuration := &types.Config{Variation: variation, Importer: unsafeImporter{}}
	if _, checkError := configuration.Check("fork", fileSet, files, &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}); checkError != nil {
		t.Fatalf("variational check: %v", checkError)
	}
	if len(variation.Types) == 0 {
		t.Fatal("the variational check recorded no answers")
	}
	forked := map[string]bool{}
	for expression, answers := range variation.Types {
		covered := types.Mask(0)
		for _, built := range answers {
			if covered&built.Builds != 0 {
				t.Errorf("%s: two answers for one build", fileSet.Position(expression.Pos()))
			}
			covered |= built.Builds
		}
		if len(answers) > 1 {
			forked[enclosingFunction(files, fileSet, expression)] = true
		}
	}
	for _, build := range []types.Mask{buildDarwin, buildLinux, buildWindows} {
		var built []*ast.File
		for _, file := range files {
			if masks[file]&build != 0 {
				built = append(built, file)
			}
		}
		plain := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
		plainConfiguration := &types.Config{Importer: unsafeImporter{}}
		if _, checkError := plainConfiguration.Check("fork", fileSet, built, plain); checkError != nil {
			t.Fatalf("plain check of build %b: %v", build, checkError)
		}
		matched := 0
		for expression, want := range plain.Types {
			got := ""
			for _, candidate := range variation.Types[expression] {
				if candidate.Builds&build != 0 {
					got = answer(candidate.TypeAndValue)
				}
			}
			if got != answer(want) {
				t.Errorf("build %03b, %s %s: projected %q, plain %q", build, fileSet.Position(expression.Pos()),
					types.ExprString(expression), got, answer(want))
				continue
			}
			matched++
		}
		if matched == 0 {
			t.Errorf("build %03b: no expression compared", build)
		}
	}
	var forkedNames []string
	for name := range forked {
		forkedNames = append(forkedNames, name)
	}
	sort.Strings(forkedNames)
	t.Logf("functions that forked: %s; units %d, restarts %d", strings.Join(forkedNames, ", "), variation.Units,
		variation.Restarts)
	for _, mustNotFork := range []string{"Untouched", "Signal", "Pass", "Size", "WordSize"} {
		if forked[mustNotFork] {
			t.Errorf("%s forked, though nothing it reads differs by build", mustNotFork)
		}
	}
	for _, mustFork := range []string{"F", "IsPipe", "InnerSize", "Join"} {
		if !forked[mustFork] {
			t.Errorf("%s didn't fork, though it reads what differs by build", mustFork)
		}
	}
}

// enclosingFunction is the name of the function declaration holding expression, or the empty string.
func enclosingFunction(files []*ast.File, fileSet *token.FileSet, expression ast.Expr) string {
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if isFunction && function.Pos() <= expression.Pos() && expression.End() <= function.End() {
				return function.Name.Name
			}
		}
	}
	return ""
}
