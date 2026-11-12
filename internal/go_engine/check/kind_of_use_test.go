package check

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// The kind-of-use fixture's paths (#mjdf7ah): dependency, whose files differ between windows and every other target,
// and format, which stands for fmt with one printf-like function, so the fixture checks with no importer of the
// standard library.
const (
	kindDependency = "example.com/kind/dependency"
	kindFormat     = "example.com/kind/format"
	kindUser       = "example.com/kind/user"
)

// kindFiles are dependency and format. On windows, Record holds a lock and a slice, so it is neither safe to copy nor
// comparable there, and Wrapper declares a field Close that shadows the method Close it promotes from Base everywhere
// else. T embeds common, as testing.T does, whose unexported start field differs by target, as testing.common's does,
// and nothing else of T or common does. Count is an int everywhere but windows, where it is a complex128, which a
// `json` tag's `string` option does not take. Names is a slice of strings everywhere but windows, where it holds ints,
// and Sizer's Size returns an int64 there, so each differs only in its underlying type or its method set.
var kindFiles = map[string]string{
	"dependency/dependency.go": "package dependency\n\n" +
		"type mutex struct{ state int32 }\n\nfunc (*mutex) Lock() {}\n\nfunc (*mutex) Unlock() {}\n\n" +
		"func Consume(Record) {}\n\n" +
		"type Base struct{}\n\nfunc (Base) Close() error { return nil }\n\n" +
		"type T struct{ common }\n\ntype common struct {\n\tstart stamp\n\tname  string\n}\n\n" +
		"func (*common) Errorf(format string, arguments ...any) {}\n",
	"dependency/dependency_windows.go": "package dependency\n\n" +
		"type Record struct {\n\tName    string\n\tlock    mutex\n\thandles []uintptr\n}\n\n" +
		"type Wrapper struct {\n\tBase\n\tClose int\n}\n\n" +
		"type stamp struct{ ticks int64 }\n\n" +
		"type Count complex128\n\n" +
		"type Names []int\n\ntype Sizer struct{}\n\nfunc (Sizer) Size() int64 { return 0 }\n",
	"dependency/dependency_other.go": "//go:build !windows\n\npackage dependency\n\n" +
		"type Record struct {\n\tName  string\n\tcount int\n}\n\n" +
		"type Wrapper struct{ Base }\n\n" +
		"type stamp struct {\n\twall uint64\n\text  int64\n}\n\n" +
		"type Count int\n\n" +
		"type Names []string\n\ntype Sizer struct{}\n\nfunc (Sizer) Size() int { return 0 }\n",
	"format/format.go": "package format\n\n" +
		"func Sprintf(format string, arguments ...any) string { return format }\n",
}

// kindUsers are the user packages, by the use each makes of dependency. Each but member takes what it uses by pointer,
// so the use the case names is the only one of the type whole.
var kindUsers = map[string]string{
	"compared": "func Same(left, right *dependency.Record) bool { return *left == *right }\n",
	"sized": "import \"unsafe\"\n\n" +
		"func Size(record *dependency.Record) uintptr { return unsafe.Sizeof(*record) }\n",
	"printed": "import \"example.com/kind/format\"\n\n" +
		"func Show(record *dependency.Record) string { return format.Sprintf(\"%d\", record) }\n",
	"passed":   "func Keep(record *dependency.Record) { dependency.Consume(*record) }\n",
	"shadowed": "func Shut(wrapper *dependency.Wrapper) error { return wrapper.Close() }\n",
	"instantiated": "func index[Value comparable](values []Value) int { return len(values) }\n\n" +
		"func Count(records []dependency.Record) int { return index(records) }\n",
	"member":       "func Check(t *dependency.T) { t.Errorf(\"x %d\", 1) }\n",
	"stringOption": "type Holder struct {\n\tCount *dependency.Count `json:\",string\"`\n}\n",
	"underlying":   "func Size(names *dependency.Names) int { return len(*names) }\n",
	"methodSet":    "func Hand(sizer *dependency.Sizer) interface{ Size() int } { return sizer }\n",
}

// kindPackages are dependency and format type-checked for goos from the files its build context matches, by path.
func kindPackages(test *testing.T, goos string) map[string]*types.Package {
	test.Helper()

	directory := test.TempDir()
	writeFiles(test, directory, kindFiles)
	context := build.Default
	context.GOOS, context.GOARCH, context.CgoEnabled = goos, "amd64", false
	fileSet := token.NewFileSet()
	checked := map[string]*types.Package{}
	for _, path := range []string{kindDependency, kindFormat} {
		packageDirectory := filepath.Join(directory, filepath.Base(path))
		entries, readError := os.ReadDir(packageDirectory)
		if readError != nil {
			test.Fatal(readError)
		}
		var files []*ast.File
		for _, entry := range entries {
			matched, matchError := context.MatchFile(packageDirectory, entry.Name())
			if matchError != nil {
				test.Fatal(matchError)
			}
			if !matched {
				continue
			}
			parsed, parseError := parser.ParseFile(fileSet, filepath.Join(packageDirectory, entry.Name()), nil,
				parser.SkipObjectResolution|parser.ParseComments)
			if parseError != nil {
				test.Fatal(parseError)
			}
			files = append(files, parsed)
		}
		checkedPackage, checkError := (&types.Config{}).Check(path, fileSet, files, nil)
		if checkError != nil {
			test.Fatalf("type-checking %s for %s: %v", path, goos, checkError)
		}
		checked[path] = checkedPackage
	}
	return checked
}

// kindRestoring is the user package named user checked on the darwin host from source, its reads recorded from its
// type information and syntax, as the run records them.
func kindRestoring(test *testing.T, user string) Restoring {
	test.Helper()

	host := kindPackages(test, "darwin")
	fileSet := token.NewFileSet()
	source := "package user\n\nimport \"" + kindDependency + "\"\n\n" + kindUsers[user]
	file, parseError := parser.ParseFile(fileSet, "user.go", source, parser.SkipObjectResolution|parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if path == "unsafe" {
			return types.Unsafe, nil
		}
		if found, isFound := host[path]; isFound {
			return found, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	checked, checkError := configuration.Check(kindUser, fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("type-checking the %s user on the host: %v", user, checkError)
	}
	reads := &build_reads.Reads{}
	reads.RecordTypes(checked, info, []*ast.File{file})
	if why := reads.ClosureOnly(); why != "" {
		test.Fatalf("the %s user's reads are judged by closure alone, so the fixture tells nothing: %s", user, why)
	}
	imports := make([]string, 0, len(checked.Imports()))
	for _, imported := range checked.Imports() {
		imports = append(imports, imported.Path())
	}
	return Restoring{Path: kindUser, Imports: imports, Reads: reads}
}

// kindHolds is the load's verdict on the user package named user in the build of goos.
func kindHolds(test *testing.T, user string, goos string) (bool, string) {
	test.Helper()

	return TypesHold(kindRestoring(test, user), kindPackages(test, goos), nil, NewReadsBuild(), NewReadsBuild())
}

// Each use of a type that sees all of it is judged by the type's closure, so a user that compares, sizes, prints or
// passes by value a Record, which on windows holds a lock and a slice, is declined on windows by Record, as is one that
// instantiates a comparable generic with it, which windows would refuse; a selection of a method that a windows-only
// field shadows at a shallower depth is declined by its index path; and a field that points to Count, tagged with
// `json`'s `string` option, which SA5008 judges by the basic kind it points to, is declined by Count's name and kind,
// an int on the host and a complex128 on windows. An operation on Names reads its underlying type, and handing a Sizer
// to a non-empty interface reads its method set, each of which windows declares its own way. Each holds on the host's
// twin, darwin checked again, so the decline is the difference and nothing else. Judged by the members each names,
// every one would be restored on windows, and its type error or its findings lost (#mjdf7ah).
func TestAUseOfAWholeTypeIsDeclinedWhereTheTypeDiffers(test *testing.T) {
	test.Parallel()

	recordDiffers := "T object " + kindDependency + ".Record renders differently"
	for _, step := range []struct {
		user string
		want string
	}{
		{"compared", recordDiffers},
		{"sized", recordDiffers},
		{"printed", recordDiffers},
		{"passed", recordDiffers},
		{"instantiated", recordDiffers},
		{"shadowed", "T selection " + kindDependency + ".Wrapper.Close: the build finds it at [1], the host at [0 0]"},
		{"stringOption", "T object " + kindDependency + ".Count: its name or kind differs"},
		{"underlying", "T object " + kindDependency + ".Names: its underlying type differs"},
		{"methodSet", "T object " + kindDependency + ".Sizer: its method set differs"},
	} {
		test.Run(step.user, func(test *testing.T) {
			test.Parallel()

			if holds, why := kindHolds(test, step.user, "windows"); holds || why != step.want {
				test.Errorf("on windows, want the %s user declined, %q, got %t, %q", step.user, step.want, holds, why)
			}
			if holds, why := kindHolds(test, step.user, "darwin"); !holds {
				test.Errorf("on the host's twin, want the %s user restored, got %q", step.user, why)
			}
		})
	}
}

// A user that only calls a method T promotes from common, as a test calls t.Errorf, reads the method's own line and
// the selection's index path, both the same in every build, and none of common's unexported start field, which
// differs on windows: it is restored there, as on the host's twin.
func TestAMemberUseOfATypeWhoseUnexportedFieldDiffersIsRestored(test *testing.T) {
	test.Parallel()

	for _, goos := range []string{"windows", "darwin"} {
		if holds, why := kindHolds(test, "member", goos); !holds {
			test.Errorf("on %s, want the member user restored, got %q", goos, why)
		}
	}
}
