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
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/variational_types/merged"
	"github.com/system-inc/cohere/internal/variational_types/types"
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
	command := exec.Command("go", "list", "-export", "-deps", "-json=ImportPath,Export", "os", "syscall")
	command.Dir = test.TempDir()
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

func TestMergedImporterKeepsOneObjectPerDeclarationAndItsAnswersByBuild(test *testing.T) {
	exports := make([]map[string]string, len(buildSystems))
	for index, goos := range buildSystems {
		exports[index] = listExports(test, goos)
	}
	importer := merged.New(token.NewFileSet(), exports, everyBuild)
	osPackage, osError := importer.Import("os")
	if osError != nil {
		test.Fatal(osError)
	}
	syscallPackage, syscallError := importer.Import("syscall")
	if syscallError != nil {
		test.Fatal(syscallError)
	}
	defer func() {
		decode, merge := importer.Timing()
		test.Logf("decode %v, merge %v, shortfalls %+v", decode, merge, importer.Shortfalls())
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
	if field := file.Underlying().(*types.Struct).Field(0); types.Unalias(field.Type()).(*types.Pointer).Elem() !=
		types.Type(unexportedFile) {
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

// lookupNamed is pkg's named type of name.
func lookupNamed(test *testing.T, pkg *types.Package, name string) *types.Named {
	test.Helper()
	typeName, isTypeName := pkg.Scope().Lookup(name).(*types.TypeName)
	if !isTypeName {
		test.Fatalf("%s.%s is no type name", pkg.Path(), name)
	}
	named, isNamed := typeName.Type().(*types.Named)
	if !isNamed {
		test.Fatalf("%s.%s is %T, no named type", pkg.Path(), name, typeName.Type())
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

func TestMergedImporterResolvesInsideTheChecksUnits(test *testing.T) {
	exports := make([]map[string]string, len(buildSystems))
	for index, goos := range buildSystems {
		exports[index] = listExports(test, goos)
	}
	fileSet := token.NewFileSet()
	importer := merged.New(fileSet, exports, everyBuild)
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

	var separator, name ast.Expr
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
	decode, merge := importer.Timing()
	test.Logf("decode %v, merge %v, %d units, %d restarts", decode, merge, variation.Units, variation.Restarts)
}
