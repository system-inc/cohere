package checker

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules/vet/printf"
)

// pushedFactAnalyzers are the analyzers of a cohere-go run whose facts travel along the graph's vertical edges:
// ctrlflow's on functions and methods that never return, printf's on wrappers, and deprecated's on objects of every
// kind, types, constants, fields and methods among them, which it lists whole through AllObjectFacts.
var pushedFactAnalyzers = []*analysis.Analyzer{ctrlflow.Analyzer, printf.Analyzer, deprecated.Analyzer}

// broadStandardLibrary is a package that imports a wide slice of the standard library, so its closure is a few hundred
// real packages, every one analyzed for its facts alone, as a cohere-go run analyzes a project's dependencies.
const broadStandardLibrary = `package broad

import (
	_ "crypto/tls"
	_ "database/sql"
	_ "encoding/json"
	_ "encoding/xml"
	_ "go/types"
	_ "html/template"
	_ "net/http"
	_ "os/exec"
)
`

// loadBroadStandardLibrary writes broadStandardLibrary as a module of its own under directory, once, and loads it,
// so each round of the benchmark has every dependency's syntax whole again, which Analyze lets go of.
func loadBroadStandardLibrary(benchmark *testing.B, directory string) []*packages.Package {
	benchmark.Helper()
	module := filepath.Join(directory, "go.mod")
	if _, statError := os.Stat(module); os.IsNotExist(statError) {
		goModule := []byte("module example.com/broad\n\ngo 1.21\n")
		if writeError := os.WriteFile(module, goModule, 0o644); writeError != nil {
			benchmark.Fatalf("writing go.mod: %v", writeError)
		}
		source := filepath.Join(directory, "broad.go")
		if writeError := os.WriteFile(source, []byte(broadStandardLibrary), 0o644); writeError != nil {
			benchmark.Fatalf("writing broad.go: %v", writeError)
		}
	}
	return loadedRoots(benchmark, directory, []string{"GOWORK=off"})
}

// loadedRoots loads every package below directory, with environment added to the current one, and fails the
// benchmark when any of them did not load whole.
func loadedRoots(benchmark *testing.B, directory string, environment []string) []*packages.Package {
	benchmark.Helper()
	loaded, loadError := load.Packages(directory, []string{"./..."}, environment, nil)
	if loadError != nil {
		benchmark.Fatalf("loading %s: %v", directory, loadError)
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			benchmark.Fatalf("loading %s: %s: %v", directory, root.ID, root.Errors)
		}
	}
	return loaded.Roots
}

// liveHeap is the bytes the heap holds once a collection has run, so what it counts is only what something still
// reaches.
func liveHeap() int64 {
	runtime.GC()
	var statistics runtime.MemStats
	runtime.ReadMemStats(&statistics)
	return int64(statistics.HeapAlloc)
}

// BenchmarkFactInheritance analyzes a few hundred standard-library packages with the analyzers whose facts a child
// run pushes along its vertical edges (benchmarkFactInheritance).
func BenchmarkFactInheritance(benchmark *testing.B) {
	directory := benchmark.TempDir()
	benchmarkFactInheritance(benchmark, func() []*packages.Package {
		return loadBroadStandardLibrary(benchmark, directory)
	})
}

// benchmarkFactInheritance analyzes what loaded loads with pushedFactAnalyzers, and reports what the analysis
// allocates, as retained-B/op what its graph alone still holds once it has returned, the facts every action inherited
// and exported among it, which no later action releases, and as object-fact-entries/op how many entries the actions'
// object fact maps hold. Each round loads the packages again with the timer stopped, so no number counts the load.
func benchmarkFactInheritance(benchmark *testing.B, loaded func() []*packages.Package) {
	benchmark.ReportAllocs()
	var retained int64
	entries := 0
	for range benchmark.N {
		benchmark.StopTimer()
		roots := loaded()
		benchmark.StartTimer()
		graph, analyzeError := Analyze(pushedFactAnalyzers, roots, nil)
		benchmark.StopTimer()
		if analyzeError != nil {
			benchmark.Fatalf("analyzing: %v", analyzeError)
		}
		for action := range graph.All() {
			if action.Err != nil {
				benchmark.Fatalf("%s failed: %v", action, action.Err)
			}
			entries += len(action.objectFacts)
		}
		withGraph := liveHeap()
		// The graph is unreachable past this line, while the roots and every package below them stay reachable from
		// roots, so the second count lets go of what the graph alone holds.
		runtime.KeepAlive(graph)
		retained += withGraph - liveHeap()
		runtime.KeepAlive(roots)
		benchmark.StartTimer()
	}
	benchmark.ReportMetric(float64(retained)/float64(benchmark.N), "retained-B/op")
	benchmark.ReportMetric(float64(entries)/float64(benchmark.N), "object-fact-entries/op")
}
