package load

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
)

// A dependency whose cgo declarations the load withholds once every package is checked is final for analysis only
// then. The project's one package, heavy, imports a dependency outside the workspace that wraps its C names as
// wrapperFiles does, and is long enough that its own type check, after the dependency's, takes a while. An analysis
// of the dependency, started beside the load and gated by its readiness, reads Count, a function that reaches cgo,
// and meets it without its body, as an analysis after the load does. Gated at the dependency's own type check, it
// would start while heavy is still checking, before the load withheld the body.
func TestAnAnalysisBesideTheLoadMeetsADependencysCgoDeclarationsWithheld(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	files := map[string]string{
		"project/go.mod": "module example.com/project\n\ngo 1.21\n\nrequire example.com/dependency v0.0.0\n\n" +
			"replace example.com/dependency => ../dependency\n",
		"project/heavy/heavy.go": "package heavy\n\nimport \"example.com/dependency\"\n\n" +
			"// Heavy folds the dependency's count.\nfunc Heavy() uint64 {\n\tvalue := dependency.Count()\n" +
			strings.Repeat("\tvalue = value*31 + 7\n", 40000) + "\treturn value\n}\n",
		"dependency/go.mod": "module example.com/dependency\n\ngo 1.21\n",
	}
	for name, contents := range wrapperFiles("dependency") {
		files["dependency/"+name] = contents
	}
	for name, contents := range files {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	var lock sync.Mutex
	seen := false
	bodied := false
	withheld := &analysis.Analyzer{
		Name: "withheld",
		Doc:  "withheld reads, from the dependency's pass, whether Count came with its body",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				for _, declaration := range file.Decls {
					function, isFunction := declaration.(*ast.FuncDecl)
					if isFunction && function.Name.Name == "Count" {
						lock.Lock()
						seen = true
						bodied = function.Body != nil
						lock.Unlock()
					}
				}
			}
			return nil, nil
		},
	}
	analyzed := make(chan error, 1)
	_, loadError := PackagesOverlapping(filepath.Join(directory, "project"), []string{"./..."}, cgoEnvironment, nil,
		func(overlap *Overlap) {
			var dependency *packages.Package
			for _, analyzedPackage := range overlap.Packages {
				if imported := analyzedPackage.Imports["example.com/dependency"]; imported != nil {
					dependency = imported
				}
			}
			if dependency == nil {
				analyzed <- nil
				return
			}
			go func() {
				_, analyzeError := checker.Analyze([]*analysis.Analyzer{withheld}, []*packages.Package{dependency},
					&checker.Options{Ready: overlap.Ready})
				analyzed <- analyzeError
			}()
		})
	if loadError != nil {
		test.Fatalf("loading the project: %v", loadError)
	}
	select {
	case analyzeError := <-analyzed:
		if analyzeError != nil {
			test.Fatalf("analyzing: %v", analyzeError)
		}
	case <-time.After(60 * time.Second):
		test.Fatalf("want the graph analyzed, and it had not returned after 60 seconds")
	}
	lock.Lock()
	defer lock.Unlock()
	if !seen {
		test.Fatalf("want the dependency analyzed and its Count read, and it was not")
	}
	if bodied {
		test.Errorf("want the dependency's Count without its body, as the load withholds it, and it had one")
	}
}
