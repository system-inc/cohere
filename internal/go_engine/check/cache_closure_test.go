package check

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis/passes/printf"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// closureFiles are a module whose one package wraps Printf in a local closure and calls it with a verb that does not
// fit its argument: printf marks the closure's variable as a wrapper and reports the call through that fact.
var closureFiles = map[string]string{
	"go.mod": "module example.com/closure\n\ngo 1.24\n",
	"closure/closure.go": "package closure\n\nimport \"fmt\"\n\n// Run wraps Printf in a closure.\nfunc Run() {\n" +
		"\tfail := func(format string, arguments ...any) { fmt.Printf(format, arguments...) }\n" +
		"\tfail(\"%d\\n\", \"three\")\n}\n",
}

// A printf-style wrapper closure leaves its package storable: the fact printf exports on the closure's variable,
// which has no objectpath, is left out of the entry and counted, every package missed is written, and a warm run
// hits them all and gives the findings a run without the cache gives.
func TestAPrintfWrapperClosureLeavesItsPackageStorable(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	for name, contents := range closureFiles {
		path := filepath.Join(root, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	// Each run below that analyzes a dependency lets go of its syntax, so the run without the cache has a load of
	// its own.
	loaded, loadError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("loading the closure module: %v", loadError)
	}
	loadedAgain, loadAgainError := load.Packages(root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadAgainError != nil {
		test.Fatalf("loading the closure module again: %v", loadAgainError)
	}
	rules := []rule.Rule{
		{Name: rule.Namespace + "printf", Origin: rule.OriginVet, UpstreamName: "printf", Analyzer: printf.Analyzer},
	}
	caching := testCaching(test, rules)
	cold := runCached(test, loaded.Packages, caching, rules)
	if cold.Cache.Unstorable != 0 || cold.Cache.FactsDropped != 1 || cold.Cache.Written != cold.Cache.Missed {
		test.Fatalf("want every miss written, none unstorable and the closure's fact dropped, got %+v", cold.Cache)
	}
	warm := runCached(test, loaded.Packages, caching, rules)
	if warm.Cache.Hit != cold.Cache.Written || warm.Cache.Missed != 0 {
		test.Fatalf("want every package hit warm, cold %+v, warm %+v", cold.Cache, warm.Cache)
	}
	uncached, runError := Run(loadedAgain.Packages, rules, Options{})
	if runError != nil {
		test.Fatalf("running printf without the cache: %v", runError)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(warm), withoutTimesOrCache(uncached)) {
		test.Fatalf("warm\n%+v\nwithout the cache\n%+v", withoutTimesOrCache(warm), withoutTimesOrCache(uncached))
	}
	if len(uncached.Findings) != 1 {
		test.Fatalf("want the closure's call reported, got %+v", uncached.Findings)
	}
}
