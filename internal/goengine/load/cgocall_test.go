package load

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/cgocall"

	"github.com/system-inc/cohere/internal/goengine/check/checker"
)

// cgocallFiles are a project's cgo package that passes C a pointer to a Go slice, which the cgo pointer rules
// forbid, and the call cgocall exists to find.
var cgocallFiles = map[string]string{
	"go.mod": "module example.com/cgocall\n\ngo 1.21\n",
	"native.go": "package native\n\n// static void take(void *pointer) {}\nimport \"C\"\n\nimport \"unsafe\"\n\n" +
		"// Pass hands values to C.\nfunc Pass(values []int) {\n\tC.take(unsafe.Pointer(&values))\n}\n",
}

// vet's cgocall reads a package's raw cgo files, and runs only on a package whose types import runtime/cgo, as
// cgo's translation makes them. A project's own cgo package sends the load through go/packages, whose cgo tool
// translates it, so cgocall runs over it as it would under go vet, and finds the slice passed to C. It needs cgo,
// so it skips where the toolchain has none; Loom's runner, which refuses the go list -compiled that go/packages
// runs, cannot reach it at all.
func TestCgocallFindsItsViolationInAProjectsCgoPackage(test *testing.T) {
	test.Parallel()
	requireCgo(test)

	directory := test.TempDir()
	for name, contents := range cgocallFiles {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	loaded, loadError := Packages(directory, []string{"./..."}, []string{"GOWORK=off", "CGO_ENABLED=1"}, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	if !loaded.CgoTranslated {
		test.Fatal("a project with cgo of its own was loaded from source, its package \"C\" faked")
	}
	if len(loaded.Diagnostics) > 0 {
		test.Fatalf("the cgo package loaded with diagnostics: %+v", loaded.Diagnostics)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{cgocall.Analyzer}, loaded.Packages, nil)
	if analyzeError != nil {
		test.Fatal(analyzeError)
	}
	var found []string
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("cgocall did not finish on %s: %v", action.Package.ID, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			found = append(found, action.Package.Fset.Position(diagnostic.Pos).String()+": "+diagnostic.Message)
		}
	}
	want := filepath.Join(directory, "native.go") + ":10:9: possibly passing Go type with embedded pointer to C"
	if len(found) != 1 || found[0] != want {
		test.Fatalf("want cgocall's one finding\n%s\ngot\n%q", want, found)
	}
}
