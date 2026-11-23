package merged_test

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/variational_types/merged"
	"github.com/system-inc/go-types/canonical"
	"github.com/system-inc/go-types/types"
)

// The test's builds: bit 0 is darwin, bit 1 linux, bit 2 windows, every one arm64 without cgo.
const (
	buildDarwin  types.Mask = 1 << 0
	buildLinux   types.Mask = 1 << 1
	buildWindows types.Mask = 1 << 2
	everyBuild              = buildDarwin | buildLinux | buildWindows
)

var buildSystems = []string{"darwin", "linux", "windows"}

// listExports maps each package os and syscall import, for goos on arm64 without cgo, to its export data file. go list
// compiles what the build cache lacks, from the local toolchain's own sources.
func listExports(test *testing.T, goos string) map[string]string {
	test.Helper()
	return listExportsIn(test, test.TempDir(), goos, "os", "syscall")
}

// listExportsIn maps each package patterns import, listed in directory for goos on arm64 without cgo, to its export
// data file.
func listExportsIn(test *testing.T, directory string, goos string, patterns ...string) map[string]string {
	test.Helper()
	command := exec.Command("go", append([]string{"list", "-export", "-deps", "-json=ImportPath,Export"},
		patterns...)...)
	command.Dir = directory
	command.Env = append(withoutVariable(os.Environ(), "GOWORK"), "GOOS="+goos, "GOARCH=arm64", "CGO_ENABLED=0",
		"GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=")
	output, listError := command.Output()
	if listError != nil {
		var exitError *exec.ExitError
		if errors.As(listError, &exitError) {
			test.Fatalf("go list for %s: %v\n%s", goos, listError, exitError.Stderr)
		}
		test.Fatalf("go list for %s: %v", goos, listError)
	}
	exports := map[string]string{}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for {
		var listed struct {
			ImportPath string
			Export     string
		}
		if decodeError := decoder.Decode(&listed); errors.Is(decodeError, io.EOF) {
			break
		} else if decodeError != nil {
			test.Fatal(decodeError)
		}
		if listed.Export != "" {
			exports[listed.ImportPath] = listed.Export
		}
	}
	return exports
}

// exportSources is a source for each build, reading the export data files exports[b] names for build b.
func exportSources(fileSet *token.FileSet, exports []map[string]string) []merged.Source {
	sources := make([]merged.Source, len(exports))
	for index, files := range exports {
		sources[index] = merged.ExportData(fileSet, files)
	}
	return sources
}

// withoutVariable is environment without name's entry.
func withoutVariable(environment []string, name string) []string {
	var kept []string
	for _, entry := range environment {
		if !strings.HasPrefix(entry, name+"=") {
			kept = append(kept, entry)
		}
	}
	return kept
}

// Not parallel: the fork holds a variational check's unit state in package variables, which a check writes and
// every merge and lookup reads.
func TestMergedImporterKeepsOneObjectPerDeclarationAndItsAnswersByBuild(test *testing.T) {
	exports := make([]map[string]string, len(buildSystems))
	for index, goos := range buildSystems {
		exports[index] = listExports(test, goos)
	}
	fileSet := token.NewFileSet()
	importer := merged.New(fileSet, exportSources(fileSet, exports), everyBuild)
	osPackage, osError := importer.Import("os")
	if osError != nil {
		test.Fatal(osError)
	}
	syscallPackage, syscallError := importer.Import("syscall")
	if syscallError != nil {
		test.Fatal(syscallError)
	}
	defer func() {
		timing := importer.Timing()
		test.Logf("decode %v, waited %v, merge %v, shortfalls %+v", timing.Decode, timing.DecodeWait, timing.Merge,
			importer.Shortfalls())
	}()

	again, againError := importer.Import("os")
	if againError != nil || again != osPackage {
		test.Errorf("a second import of os is %p (%v), not the first's %p", again, againError, osPackage)
	}

	file := lookupNamed(test, osPackage, "File")
	if variants := types.UnderlyingVariantsOf(file); variants != nil {
		test.Errorf("os.File has underlying variants %v, and every build declares it struct{*file}", variants)
	}
	if spelled := types.TypeString(file.Underlying(), nil); spelled != "struct{*os.file}" {
		test.Errorf("os.File's underlying type is %s", spelled)
	}

	unexportedFile := lookupNamed(test, osPackage, "file")
	if variants := types.UnderlyingVariantsOf(unexportedFile); len(variants) < 2 {
		test.Errorf("os.file has underlying variants %v, and its fields differ by platform", variants)
	} else {
		for _, variant := range variants {
			test.Logf("os.file in builds %03b: %s", variant.Builds, variant.Underlying)
		}
	}
	// os.File's one field is the one os.file, whatever its underlying type in a build.
	fileStruct, isStruct := file.Underlying().(*types.Struct)
	if !isStruct {
		test.Fatalf("os.File's underlying type is %s, no struct", file.Underlying())
	}
	field := fileStruct.Field(0)
	if pointer, isPointer := types.Unalias(field.Type()).(*types.Pointer); !isPointer ||
		pointer.Elem() != types.Type(unexportedFile) {
		test.Errorf("os.File's field is %s, not a pointer to the merged os.file", field.Type())
	}

	esrch := syscallPackage.Scope().Lookup("ESRCH")
	assertValues(test, esrch, func(builds types.Mask, value constant.Value) bool {
		three := constant.MakeInt64(3)
		if builds&buildWindows != 0 {
			return builds == buildWindows && !constant.Compare(value, token.EQL, three)
		}
		return builds == buildDarwin|buildLinux && constant.Compare(value, token.EQL, three)
	})

	pathSeparator := osPackage.Scope().Lookup("PathSeparator")
	assertValues(test, pathSeparator, func(builds types.Mask, value constant.Value) bool {
		if builds == buildWindows {
			return constant.Compare(value, token.EQL, constant.MakeInt64('\\'))
		}
		return builds == buildDarwin|buildLinux && constant.Compare(value, token.EQL, constant.MakeInt64('/'))
	})

	kill := syscallPackage.Scope().Lookup("Kill")
	if kill == nil {
		test.Fatal("syscall has no Kill in the merged universe")
	}
	if presence := types.PresenceOf(kill); presence != buildDarwin|buildLinux {
		test.Errorf("syscall.Kill is present in builds %03b, and only darwin and linux declare it", presence)
	}
	if presence := types.PresenceOf(syscallPackage.Scope().Lookup("Getpid")); presence != 0 {
		test.Errorf("syscall.Getpid is present in builds %03b, and every build declares it", presence)
	}
}

// lookupNamed is fromPackage's named type of name.
func lookupNamed(test *testing.T, fromPackage *types.Package, name string) *types.Named {
	test.Helper()
	typeName, isTypeName := fromPackage.Scope().Lookup(name).(*types.TypeName)
	if !isTypeName {
		test.Fatalf("%s.%s is no type name", fromPackage.Path(), name)
	}
	named, isNamed := typeName.Type().(*types.Named)
	if !isNamed {
		test.Fatalf("%s.%s is %T, no named type", fromPackage.Path(), name, typeName.Type())
	}
	return named
}

// assertValues checks that object, a constant, has two answers by build and that expected holds of each.
func assertValues(test *testing.T, object types.Object, expected func(builds types.Mask, value constant.Value) bool) {
	test.Helper()
	if object == nil {
		test.Fatal("no such constant in the merged universe")
	}
	variants := types.VariantsOf(object)
	if len(variants) != 2 {
		test.Errorf("%s has %d answers by build, %v, and wants two", object.Name(), len(variants), variants)
		return
	}
	for _, variant := range variants {
		test.Logf("%s in builds %03b: %s", object.Name(), variant.Builds, variant.Value)
		if !expected(variant.Builds, variant.Value) {
			test.Errorf("%s is %s in builds %03b", object.Name(), variant.Value, variant.Builds)
		}
	}
}

// probeSource reads imported objects only inside the check's units, so each is merged lazily while a unit runs: the
// separator's value differs by build, and os.File's merge reaches os.file and the platform packages beneath it, some
// of whose objects only some builds declare, which a unit would split on.
const probeSource = `package probe

import "os"

func Separator() rune { return os.PathSeparator }

func Name(file *os.File) string { return file.Name() }
`

// Not parallel: the fork holds a variational check's unit state in package variables, which a check writes and
// every merge and lookup reads.
func TestMergedImporterResolvesInsideTheChecksUnits(test *testing.T) {
	exports := make([]map[string]string, len(buildSystems))
	for index, goos := range buildSystems {
		exports[index] = listExports(test, goos)
	}
	fileSet := token.NewFileSet()
	importer := merged.New(fileSet, exportSources(fileSet, exports), everyBuild)
	file, parseError := parser.ParseFile(fileSet, "probe.go", probeSource, 0)
	if parseError != nil {
		test.Fatal(parseError)
	}
	variation := &types.Variation{Builds: everyBuild}
	var reported []string
	configuration := &types.Config{
		Variation: variation,
		Importer:  importer,
		Error:     func(checkError error) { reported = append(reported, checkError.Error()) },
	}
	if _, checkError := configuration.Check("probe", fileSet, []*ast.File{file}, &types.Info{}); checkError != nil {
		test.Fatalf("the check failed: %v (every error: %q)", checkError, reported)
	}

	var separator ast.Expr
	var name ast.Expr
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			if node.Sel.Name == "PathSeparator" {
				separator = node
			}
		case *ast.CallExpr:
			name = node
		}
		return true
	})
	values := map[types.Mask]string{}
	for answer := range variation.Types[separator].All() {
		values[answer.Builds] = answer.Value.ExactString()
	}
	if values[buildDarwin|buildLinux] != "47" || values[buildWindows] != "92" || len(values) != 2 {
		test.Errorf("os.PathSeparator answers %v by build, and wants 47 on darwin and linux, 92 on windows", values)
	}
	for answer := range variation.Types[name].All() {
		if answer.Builds != everyBuild || types.TypeString(answer.Type, nil) != "string" {
			test.Errorf("file.Name() answers %s in builds %03b", answer.Type, answer.Builds)
		}
	}

	osPackage, osError := importer.Import("os")
	if osError != nil {
		test.Fatal(osError)
	}
	if variants := types.UnderlyingVariantsOf(lookupNamed(test, osPackage, "file")); len(variants) != 2 {
		test.Errorf("os.file, merged inside a unit, has underlying variants %v", variants)
	}
	timing := importer.Timing()
	test.Logf("decode %v, waited %v, merge %v, %d units, %d restarts", timing.Decode, timing.DecodeWait, timing.Merge,
		variation.Units, variation.Restarts)
}

// lookedUpNames are names of os and syscall the concurrent lookups ask for: some differ by build, some exist in only
// some builds, and some reach the others through their types.
var lookedUpNames = []string{
	"os.File", "os.file", "os.PathSeparator", "os.Getpid", "os.Open", "os.ErrNotExist", "os.FileInfo", "os.Process",
	"os.Signal", "os.ProcAttr", "syscall.Kill", "syscall.ESRCH", "syscall.Errno", "syscall.Stat_t", "syscall.Getpid",
	"syscall.SysProcAttr", "syscall.Signal", "syscall.RawConn",
}

// Not parallel: the fork holds a variational check's unit state in package variables, which a check writes and
// every merge and lookup reads.
func TestMergedImporterIsSafeForLookupsFromManyGoroutinesAtOnce(test *testing.T) {
	exports := make([]map[string]string, len(buildSystems))
	for index, goos := range buildSystems {
		exports[index] = listExports(test, goos)
	}
	fileSet := token.NewFileSet()
	importer := merged.New(fileSet, exportSources(fileSet, exports), everyBuild)
	const goroutines = 32
	found := make([]map[string]types.Object, goroutines)
	var group sync.WaitGroup
	for index := range goroutines {
		group.Go(func() {
			found[index] = map[string]types.Object{}
			// Each goroutine starts at another name, so some merge different objects at once and others the same one.
			for offset := range lookedUpNames {
				qualified := lookedUpNames[(index+offset)%len(lookedUpNames)]
				path, name, _ := strings.Cut(qualified, ".")
				importedPackage, importError := importer.Import(path)
				if importError != nil {
					test.Errorf("goroutine %d: importing %s: %v", index, path, importError)
					return
				}
				object := importedPackage.Scope().Lookup(name)
				if object == nil {
					test.Errorf("goroutine %d: %s looked up nil", index, qualified)
					continue
				}
				found[index][qualified] = object
				// Reading what the lookup made reads the types it reached, which other goroutines are merging.
				_ = types.ObjectString(object, nil)
				if typeName, isTypeName := object.(*types.TypeName); isTypeName {
					_ = types.NewMethodSet(types.NewPointer(typeName.Type())).Len()
				}
			}
		})
	}
	group.Wait()
	for _, qualified := range lookedUpNames {
		first := found[0][qualified]
		for index := 1; index < goroutines; index++ {
			if found[index][qualified] != first {
				test.Errorf(
					"%s is %p in goroutine 0 and %p in goroutine %d: two merges of one object",
					qualified,
					first,
					found[index][qualified],
					index,
				)
			}
		}
	}
	if failures := importer.Failures(); len(failures) > 0 {
		test.Errorf("the merge met failures reading sound export data: %v", failures)
	}
}

// handSource is a build's source made by hand, its packages given whole, for export data that can't be read.
type handSource struct {
	packages map[string]*types.Package
}

func (source handSource) Import(path string) (*types.Package, error) {
	return source.packages[path], nil
}

func (source handSource) Reached(path string) *types.Package { return source.packages[path] }

func (source handSource) Elapsed() time.Duration { return 0 }

// handPackage is package p with one constant, X, whose value is given, or whose lazy entry panics as reading export
// data the reader can't make sense of does when value is nil.
func handPackage(value constant.Value) *types.Package {
	madePackage := types.NewPackage("example.com/p", "p")
	if value == nil {
		madePackage.Scope().InsertLazy("X", func() types.Object { panic("export data: unhandled object tag") })
	} else {
		madePackage.Scope().Insert(types.NewConst(token.NoPos, madePackage, "X", types.Typ[types.UntypedInt], value))
	}
	madePackage.MarkComplete()
	return madePackage
}

// failureProbeSource uses p.X, which linux's export data can't read.
const failureProbeSource = `package probe

import "example.com/p"

const Y = p.X
`

// Not parallel: the fork holds a variational check's unit state in package variables, which a check writes and
// every merge and lookup reads.
func TestMergedImporterReadsAFailedLazyLookupAsAbsentInItsBuild(test *testing.T) {
	sources := []merged.Source{
		handSource{packages: map[string]*types.Package{"example.com/p": handPackage(constant.MakeInt64(1))}},
		handSource{packages: map[string]*types.Package{"example.com/p": handPackage(nil)}},
	}
	builds := buildDarwin | buildLinux
	fileSet := token.NewFileSet()
	importer := merged.New(fileSet, sources, builds)
	importedPackage, importError := importer.Import("example.com/p")
	if importError != nil {
		test.Fatal(importError)
	}
	constantX := importedPackage.Scope().Lookup("X")
	if constantX == nil {
		test.Fatal("p.X looked up nil, and darwin declares it")
	}
	if presence := types.PresenceOf(constantX); presence != buildDarwin {
		test.Errorf("p.X is present in builds %03b, and only darwin could read it", presence)
	}
	if again := importedPackage.Scope().Lookup("X"); again != constantX {
		test.Errorf("a second lookup of p.X is %v, not the first's %v", again, constantX)
	}
	failures := importer.Failures()
	if len(failures) != 1 || failures[0].Builds != buildLinux || failures[0].Name != "X" {
		test.Fatalf("the failures are %v, and want linux's one reading p.X", failures)
	}
	test.Logf("failure: %v", failures[0])

	file, parseError := parser.ParseFile(fileSet, "probe.go", failureProbeSource, 0)
	if parseError != nil {
		test.Fatal(parseError)
	}
	variation := &types.Variation{Builds: builds}
	var reported []string
	configuration := &types.Config{
		Variation: variation,
		Importer:  importer,
		Error:     func(checkError error) { reported = append(reported, checkError.Error()) },
	}
	// Ignored: the error is the one the check must report, read from reported below.
	_, _ = configuration.Check("probe", fileSet, []*ast.File{file}, &types.Info{})
	if len(reported) != 1 || !strings.Contains(reported[0], "p.X") {
		test.Errorf("the check reported %q, and wants the one use of p.X that linux can't read", reported)
	}
	for expression, answers := range variation.Types {
		if selector, isSelector := expression.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "X" {
			for answer := range answers.All() {
				if answer.Builds&buildDarwin != 0 && (answer.Value == nil || answer.Value.ExactString() != "1") {
					test.Errorf("p.X answers %v in builds %03b, and darwin reads 1", answer.Value, answer.Builds)
				}
			}
		}
	}
}

// Not parallel: the fork holds a variational check's unit state in package variables, which a check writes and
// every merge and lookup reads.
func TestMergedImporterReadsALazyLookupNoBuildCouldReadAsInvalid(test *testing.T) {
	sources := []merged.Source{
		handSource{packages: map[string]*types.Package{"example.com/p": handPackage(nil)}},
		handSource{packages: map[string]*types.Package{"example.com/p": handPackage(nil)}},
	}
	importer := merged.New(token.NewFileSet(), sources, buildDarwin|buildLinux)
	importedPackage, importError := importer.Import("example.com/p")
	if importError != nil {
		test.Fatal(importError)
	}
	invalid := importedPackage.Scope().Lookup("X")
	if invalid == nil {
		test.Fatal("p.X looked up nil, and wants an invalid object")
	}
	if invalid.Type() != types.Typ[types.Invalid] {
		test.Errorf("p.X is %v, and wants the invalid type", invalid.Type())
	}
	if again := importedPackage.Scope().Lookup("X"); again != invalid {
		test.Errorf("a second lookup of p.X is %v, not the first's %v", again, invalid)
	}
	if failures := importer.Failures(); len(failures) != 2 {
		test.Errorf("the failures are %v, and want darwin's and linux's", failures)
	}
}

// embeddingModule is a module whose interface I embeds J, whose methods differ by build: windows declares J with
// other methods than darwin and linux do.
var embeddingModule = map[string]string{
	"go.mod": "module example.com/q\n\ngo 1.27\n",
	"q.go": `package q

// I embeds J, whose methods differ by build.
type I interface{ J }

func Get() I { return nil }
`,
	"j_darwin.go": "package q\n\ntype J interface{ M() int }\n",
	"j_linux.go":  "package q\n\ntype J interface{ M() int }\n",
	"j_windows.go": `package q

type J interface {
	M() string
	N()
}
`,
}

// embeddingProbeSource calls I's method M, which is J's, inside the check's units: a function body and a variable's
// declaration.
const embeddingProbeSource = `package probe

import "example.com/q"

func Use(i q.I) any { return i.M() }

var Value = q.Get().M()
`

// Not parallel: the fork holds a variational check's unit state in package variables, which a check writes and
// every merge and lookup reads.
func TestMergedImporterCompletesAnInterfaceAsItsOwnBuildHasIt(test *testing.T) {
	directory := test.TempDir()
	for name, source := range embeddingModule {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(source), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	exports := make([]map[string]string, len(buildSystems))
	for index, goos := range buildSystems {
		exports[index] = listExportsIn(test, directory, goos, "example.com/q")
	}
	fileSet := token.NewFileSet()
	importer := merged.New(fileSet, exportSources(fileSet, exports), everyBuild)
	file, parseError := parser.ParseFile(fileSet, "probe.go", embeddingProbeSource, 0)
	if parseError != nil {
		test.Fatal(parseError)
	}
	variation := &types.Variation{Builds: everyBuild}
	configuration := &types.Config{Variation: variation, Importer: importer}
	if _, checkError := configuration.Check("probe", fileSet, []*ast.File{file}, &types.Info{}); checkError != nil {
		test.Fatalf("the variational check failed: %v", checkError)
	}
	called := map[string]bool{}
	for index, build := range []types.Mask{buildDarwin, buildLinux, buildWindows} {
		plain := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
		plainConfiguration := &types.Config{Importer: importer.Plain(index)}
		if _, checkError := plainConfiguration.Check("probe", fileSet, []*ast.File{file}, plain); checkError != nil {
			test.Fatalf("the plain check of build %03b failed: %v", build, checkError)
		}
		projected := map[string]string{}
		for expression, answers := range variation.Types {
			for answer := range answers.All() {
				if answer.Builds&build != 0 {
					projected[expressionKey(fileSet, expression)] = canonical.Answer(answer.TypeAndValue)
				}
			}
		}
		if len(projected) != len(plain.Types) {
			test.Errorf("build %03b: %d answers projected, and the plain check has %d", build, len(projected),
				len(plain.Types))
		}
		for expression, want := range plain.Types {
			key := expressionKey(fileSet, expression)
			if got := projected[key]; got != canonical.Answer(want) {
				test.Errorf("build %03b, %s: projected %q, plain %q", build, key, got, canonical.Answer(want))
			}
			if call, isCall := expression.(*ast.CallExpr); isCall && types.ExprString(call.Fun) == "i.M" {
				called[canonical.Answer(want)] = true
			}
		}
	}
	if !called["int"] || !called["string"] {
		test.Errorf("i.M() answers %v across the builds, and wants int and string", called)
	}
	if shortfalls := importer.Shortfalls(); shortfalls.MergedTypeSets != 0 {
		test.Errorf("%d type sets were completed from the merged view", shortfalls.MergedTypeSets)
	}
}

// expressionKey names expression by where it is and what it says, the same in every check of one file.
func expressionKey(fileSet *token.FileSet, expression ast.Expr) string {
	return fileSet.Position(expression.Pos()).String() + " " + types.ExprString(expression)
}
