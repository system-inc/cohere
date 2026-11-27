package check

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/build_reads"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rule"
)

// unreachedCgoPackages is how many packages importing runtime/cgo the unreached fixture holds, each marked never by
// the load, so with runtime/cgo itself several of them are handed on at once.
const unreachedCgoPackages = 4

// unreachedEnvironment is what the unreached fixture loads under: linux/amd64 with cgo on, which go list reads without
// a C compiler, as load's cgo fixtures load, so runtime/cgo has cgo files on every host.
var unreachedEnvironment = []string{"GOWORK=off", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1"}

// unreachedFiles are a module of unreachedCgoPackages packages, cgo00 on, each importing runtime/cgo, so the load marks
// each never, as it marks runtime/cgo for its cgo files, and top, which imports every one when importsCgo is set.
func unreachedFiles(importsCgo bool) map[string]string {
	files := map[string]string{"go.mod": "module example.com/unreached\n\ngo 1.24\n"}
	var imports []string
	var indexes []string
	for index := range unreachedCgoPackages {
		name := fmt.Sprintf("cgo%02d", index)
		files[name+"/"+name+".go"] = "package " + name + "\n\nimport _ \"runtime/cgo\"\n\n" +
			fmt.Sprintf("// Index is %d.\nconst Index = %[1]d\n", index)
		imports = append(imports, fmt.Sprintf("import %q\n\n", "example.com/unreached/"+name))
		indexes = append(indexes, name+".Index")
	}
	files["top/top.go"] = "package top\n\n"
	if importsCgo {
		files["top/top.go"] += strings.Join(imports, "") + "// Sum is every cgo package's Index.\nconst Sum = " +
			strings.Join(indexes, " + ") + "\n"
	}
	return files
}

// handedOn runs rules over analyzed under Declarations, with reads, the load's, and returns the ID of every package
// declarations was handed, in the order handed, once the type-read walks have ended. Each hand-on of a package whose
// reads are marked never waits, ten seconds at most, until a second such hand-on has started, so the walks no action
// reached run two at once wherever the run's walks have two slots free, rather than one after another as their little
// work would end them, and the test fails when one waited out its ten seconds.
func handedOn(
	test *testing.T,
	analyzed []*packages.Package,
	reads map[*packages.Package]*build_reads.Reads,
	rules []rule.Rule,
) []string {
	test.Helper()
	// The walks run as many at once as there are processors (check.Run's NewWalks), so on one they can never run two
	// at once, and the test says so at once rather than after a hand-on's ten seconds: only a real loss of
	// concurrency then reaches the wait's end.
	if processors := runtime.GOMAXPROCS(0); processors < 2 {
		test.Fatalf("the walks of the packages no action reached run one at a time on %d processor, so this run "+
			"can't drive two at once", processors)
	}
	var lock sync.Mutex
	var handed []string
	neverStarted := 0
	secondNever := make(chan struct{})
	// waitedOut is set when a hand-on gave up waiting for a second, so the two never ran at once.
	waitedOut := false
	declarations := func(declared *packages.Package) {
		lock.Lock()
		handed = append(handed, declared.ID)
		never := reads[declared] != nil && reads[declared].Never() != ""
		if never {
			neverStarted++
			if neverStarted == 2 {
				close(secondNever)
			}
		}
		lock.Unlock()
		if !never {
			return
		}
		// Bounded, so a run whose walks have one slot hands on one after another, as it would, and still ends.
		select {
		case <-secondNever:
		case <-time.After(10 * time.Second):
			lock.Lock()
			waitedOut = true
			lock.Unlock()
		}
	}
	result, runError := Run(analyzed, rules, Options{Reads: reads, Declarations: declarations})
	if runError != nil {
		test.Fatalf("running %d rules under declarations: %v", len(rules), runError)
	}
	// The hand-ons run in the type-read walks, which run past Run.
	result.ReadWalks.Wait()
	lock.Lock()
	// A run where a hand-on gave up waiting for a second ran the walks one after another, the next only once the
	// first's ten seconds were out, so it passed without the walks no action reached ever running at once, which is
	// what its claims say it drives (#tncmrsh). It fails rather than skips: a test that may skip never counts toward a
	// site's claims.
	if waitedOut {
		test.Errorf("a walk of a package no action reached waited out its ten seconds for a second (GOMAXPROCS %d), "+
			"so this run never had two at once", runtime.GOMAXPROCS(0))
	}
	defer lock.Unlock()
	return slices.Clone(handed)
}

// everyRead is the ID of every package reads holds, sorted.
func everyRead(reads map[*packages.Package]*build_reads.Reads) []string {
	var identifiers []string
	for recorded := range reads {
		identifiers = append(identifiers, recorded.ID)
	}
	slices.Sort(identifiers)
	return identifiers
}

// loadUnreached writes unreachedFiles to a directory of the test's own and loads patterns of it with reads of every
// package, failing the test on a diagnostic, and checks that the load marked every cgo package never.
func loadUnreached(test *testing.T, importsCgo bool, patterns []string) *load.Loaded {
	test.Helper()
	root := test.TempDir()
	writeFiles(test, root, unreachedFiles(importsCgo))
	loaded, loadError := load.PackagesCached(root, patterns, unreachedEnvironment, nil,
		load.Caching{RecordReads: load.EveryPackage})
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the unreached fixture: %v %+v", loadError, loaded.Diagnostics)
	}
	marked := 0
	for recorded, packageReads := range loaded.Reads {
		if recorded.PkgPath == "runtime/cgo" || slices.Contains(unreachedCgoPaths(), recorded.PkgPath) {
			if packageReads.Never() == "" {
				test.Fatalf("want %s marked never by the load, it is not", recorded.ID)
			}
			marked++
		}
	}
	if marked != unreachedCgoPackages+1 {
		test.Fatalf("want every cgo package and runtime/cgo in the load's reads, got %d of them", marked)
	}
	return loaded
}

// unreachedCgoPaths are the import paths of the unreached fixture's cgo packages, in order.
func unreachedCgoPaths() []string {
	var paths []string
	for index := range unreachedCgoPackages {
		paths = append(paths, fmt.Sprintf("example.com/unreached/cgo%02d", index))
	}
	return paths
}

// Under declarations every package of the run's reads is handed on once (#anfjtr3), and those the load marked never
// that no action reached are too, each in a walk of its own (recordUnreachedTypes): top imports four packages that
// import runtime/cgo, which a rule that learns no facts never runs on, so the cgo packages and runtime/cgo, their
// reads marked never, are handed on beside each other, as is every other dependency, and top in its own action's walk.
// Under -race it is the walks of the packages no action reached handing them on at once.
func TestEveryNeverMarkedDependencyNoActionReachedIsHandedItsDeclarations(test *testing.T) {
	test.Parallel()

	loaded := loadUnreached(test, true, []string{"./top"})
	handed := handedOn(test, loaded.Packages, loaded.Reads, []rule.Rule{houseRule(quietAnalyzer)})
	slices.Sort(handed)
	if want := everyRead(loaded.Reads); !slices.Equal(handed, want) {
		test.Fatalf("want every package of the reads handed on once\n%q\ngot\n%q", want, handed)
	}
}

// A root of the load the run does not analyze is handed its declarations as a dependency no action reached is
// (recordUnreachedTypes): the load's roots are top and four packages that import runtime/cgo, which top does not
// import, and the run analyzes top alone under printf, which learns facts of every package top reaches and so reaches
// none of the cgo packages. Each, its reads marked never, is handed on once beside the others, as is runtime/cgo and
// every other package of the reads.
func TestEveryNeverMarkedRootTheRunDoesNotAnalyzeIsHandedItsDeclarations(test *testing.T) {
	test.Parallel()

	loaded := loadUnreached(test, false, []string{"./..."})
	var analyzed []*packages.Package
	for _, each := range loaded.Packages {
		if each.PkgPath == "example.com/unreached/top" {
			analyzed = append(analyzed, each)
		}
	}
	if len(analyzed) != 1 {
		test.Fatalf("want top among the load's packages once, got %d", len(analyzed))
	}
	handed := handedOn(test, analyzed, loaded.Reads, throughRules()[:1])
	slices.Sort(handed)
	if want := everyRead(loaded.Reads); !slices.Equal(handed, want) {
		test.Fatalf("want every package of the reads handed on once\n%q\ngot\n%q", want, handed)
	}
}
