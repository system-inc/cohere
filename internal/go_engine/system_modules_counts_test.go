package go_engine

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// windowsOnly is a file of the project's only windows builds, so a run of every build checks a windows build beside
// the host's.
var windowsOnly = map[string]string{
	"project_windows.go": "package project\n\n// Separator is windows's.\nconst Separator = `\\`\n",
}

// Every build's modules restored from the system cache are counted, the lint record's sum and each build's own: a run
// under `--no-cache` of a project with a windows build beside the host's fills the system cache with the dependency
// under each build's context, and the next restores it in both, so the record counts two, and each build's record
// says it restored every entry it wrote the run before, the standard library's and the module's.
// Not parallel: it sets the go environment of the process, which every go command of the run in it reads
func TestEveryBuildsModulesFromTheSystemCacheAreCounted(test *testing.T) {
	root, _, _ := writeSystemModulesProject(test, windowsOnly)
	source := standardLibrarySource{stamp: engineTestsStamp, systemCacheRoot: test.TempDir()}
	filling := runEngineWith(test, root, source, rules.All(), "--no-cache")
	restoring := runEngineWith(test, root, source, rules.All(), "--no-cache")
	if restoring.counts.ModulesFromSystemCache != 2 {
		test.Errorf("the record counts %d modules' packages from the system cache, want the dependency in each of "+
			"the two builds: %+v", restoring.counts.ModulesFromSystemCache, restoring.counts)
	}
	written := map[string]int{}
	for _, build := range lintRecordOf(test, filling.stream).Builds {
		written[build.Target] = build.SystemCacheWritten
	}
	builds := lintRecordOf(test, restoring.stream).Builds
	if len(builds) != 2 {
		test.Fatalf("the run checked %d builds, want the host's and windows's", len(builds))
	}
	for _, build := range builds {
		if build.SystemCacheRestored == 0 || build.SystemCacheRestored != written[build.Target] {
			test.Errorf("the %s build restored %d from the system cache, want the %d entries it wrote the run before, "+
				"the module's among them", build.Target, build.SystemCacheRestored, written[build.Target])
		}
	}
}

// A cached run counts a module's package restored from the system cache apart from its store's hits: warm, every
// package the cold run wrote to the store is a hit, and the dependency, which it wrote to the system cache, is counted
// there alone, never a hit too.
// Not parallel: it sets the go environment of the process, which every go command of the run in it reads
func TestAModuleFromTheSystemCacheIsNoHitOfTheStore(test *testing.T) {
	root, _, _ := writeSystemModulesProject(test, nil)
	source := standardLibrarySource{stamp: engineTestsStamp, systemCacheRoot: test.TempDir()}
	cold := runEngineWith(test, root, source, rules.All())
	warm := runEngineWith(test, root, source, rules.All())
	if cold.counts.Written == 0 || warm.counts.ModulesFromSystemCache != 1 || warm.counts.Hit != cold.counts.Written ||
		warm.counts.Missed != 0 {
		test.Errorf("want every package the cold run wrote a hit warm and the dependency from the system cache alone, "+
			"cold %+v, warm %+v", cold.counts, warm.counts)
	}
}
