package check

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_triviality"
)

// trivialityModule is a module of two packages for SA4023's question of function_triviality. bottom's Typed and top's
// Local and local each return an error holding a nil pointer after real work, so a comparison of any with nil is
// reported once the callee is known not to be trivial. Each package also declares a trivial function and one trivial
// through it, which no comparison reads: a kind computed whole would answer of them, and a kind asked only by the rule
// never does.
var trivialityModule = map[string]string{
	"go.mod": "module example.com/asked\n\ngo 1.24\n",
	"bottom/bottom.go": `package bottom

type Absent struct{}

func (*Absent) Error() string { return "absent" }

func Typed() error {
	var none *Absent
	println("typed")
	return none
}

func Nothing() error { return nil }

func Chain() error { return Nothing() }
`,
	"top/top.go": `package top

import "example.com/asked/bottom"

func Local() error {
	var none *bottom.Absent
	println("Local")
	return none
}

func local() error {
	var none *bottom.Absent
	println("local")
	return none
}

func idle() error { return nil }

func relay() error { return idle() }

func UseExported() bool { return Local() != nil }

func UseLocal() bool { return local() != nil }

func UseAbroad() bool { return bottom.Typed() != nil }
`,
}

// trivialityAnswers are the objectpaths entry holds an answer of function_triviality at, sorted.
func trivialityAnswers(entry cacheEntry) []objectpath.Path {
	var paths []objectpath.Path
	for _, kind := range entry.Answers {
		if kind.Kind != function_triviality.Kind.Name {
			continue
		}
		for _, answer := range kind.Answers {
			paths = append(paths, answer.Path)
		}
	}
	slices.Sort(paths)
	return paths
}

// function_triviality is deliberately not registered (#h0k644s), so it answers only what SA4023 asks, of the package
// being analyzed. Over a cold run that writes entries: SA4023 reports all three comparisons and crashes nowhere, which
// it would had it asked the kind of bottom.Typed, another package's function; the kind judged two functions, top's
// Local and local, and none of bottom, whose Typed the rule read by its nilness alone; and neither package's entry
// holds an answer of the kind for a function the rule never asked of, as an entry computed whole in the kind would
// for Nothing, Chain, idle and relay.
//
// The run records Local's answer and not local's: the resolver keeps an answer for the store by its object's
// objectpath, and a function that is not exported has none, so its answer reaches a test's hand
// (fact_resolver.Held) and never Recorded. An entry written whole holds the registered kinds' answers alone, so
// neither entry holds any of this kind, which the log shows.
func TestTrivialityIsAnsweredOnlyWhereTheRuleAsks(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	for name, contents := range trivialityModule {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	var asking []rule.Rule
	for _, registered := range rules.All() {
		if registered.Name == rule.Namespace+"SA4023" {
			asking = append(asking, registered)
		}
	}
	if len(asking) != 1 {
		test.Fatalf("the registry holds %d rules named SA4023, want 1", len(asking))
	}
	caching := testCaching(test, asking)
	cold := runWithSession(test, directory, caching, asking)
	if len(cold.result.Crashes) > 0 || cold.result.Cache.Written != 2 {
		test.Fatalf("want both packages' entries written and no crash, got crashes %+v, %+v", cold.result.Crashes,
			cold.result.Cache)
	}
	var found []string
	for _, finding := range cold.result.Findings {
		found = append(found, filepath.Base(finding.File)+" "+finding.Message)
	}
	slices.Sort(found)
	want := []string{"top.go this comparison is always true", "top.go this comparison is always true",
		"top.go this comparison is always true"}
	if !slices.Equal(found, want) {
		test.Fatalf("want SA4023 to report the three comparisons in top, got %q", found)
	}
	resolver := cold.session.resolver
	if resolver == nil {
		test.Fatal("the session let go of its resolver before the test read it")
	}
	if computed := resolver.Counts(function_triviality.Kind).Computed.Load(); computed != 2 {
		test.Errorf("function_triviality judged %d functions, want 2, top's Local and local", computed)
	}
	asked := map[string][]objectpath.Path{"example.com/asked/top": {"Local"}, "example.com/asked/bottom": nil}
	var reachable []*packages.Package
	packages.Visit(cold.loaded.Packages, nil, func(visited *packages.Package) { reachable = append(reachable, visited) })
	judged := 0
	for _, loaded := range reachable {
		wantPaths, isOwn := asked[loaded.PkgPath]
		if !isOwn || loaded.Types == nil {
			continue
		}
		judged++
		recorded := slices.Sorted(maps.Keys(resolver.Recorded(loaded.Types)[function_triviality.Kind.Name]))
		if !slices.Equal(recorded, wantPaths) {
			test.Errorf("%s: the run recorded answers of the kind at %q, want %q", loaded.PkgPath, recorded, wantPaths)
		}
		entry, _, stored := storedEntryAt(caching.Store, sessionKey(test, cold.session, loaded.PkgPath))
		if !stored {
			test.Errorf("%s: the run wrote no entry the test can read", loaded.PkgPath)
			continue
		}
		test.Logf("%s: recorded %q, its entry holds %q", loaded.PkgPath, recorded, trivialityAnswers(entry))
		for _, path := range trivialityAnswers(entry) {
			if !slices.Contains(wantPaths, path) {
				test.Errorf("%s: its entry holds an answer of the kind at %q, which the rule never asked of",
					loaded.PkgPath, path)
			}
		}
	}
	if judged != len(asked) {
		test.Errorf("read %d of the module's %d packages", judged, len(asked))
	}
}
