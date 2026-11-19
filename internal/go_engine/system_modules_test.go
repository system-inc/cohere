package go_engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// screamingRule is the house rule both the project's file and the dependency's break, so a run that reports it of the
// project's shows the rule ran, and one that never reports it of the dependency's shows a module's file never reaches
// the output.
const screamingRule = `"rule":"cohere-go/consistency-no-screaming-snake-case"`

// writeSystemModulesProject writes, below a directory of the test's own that every go command of the run is told is
// the module cache, a project and the third-party module it requires, example.com/dependency, which a replace points
// at its directory there, so go list names its files below the module cache in a module that is not a main one, as it
// names a module it downloaded, with no network and no real module cache (#57wgzhp); it reads strings, so its key
// holds the standard library's. The project sits below the
// module cache too, as a project checked out there would, so only its being a main module keeps it the project's. The
// dependency's file and the project's own break one house rule. It returns the project's root, its one file in scope
// and the module cache.
// Not parallel: it sets the go environment of the process, which every go command of the run in it reads
func writeSystemModulesProject(test *testing.T) (string, string, string) {
	test.Helper()
	// Resolved, so the module cache go env reports and the replace's directory name the files as the root's resolved
	// path does, the engine's root being resolved (resolveRoot).
	moduleCache, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	dependency := filepath.Join(moduleCache, "example.com", "dependency@v1.0.0")
	writeFixtureFiles(test, dependency, map[string]string{
		"go.mod": "module example.com/dependency\n\ngo 1.21\n",
		"dependency.go": "// Package dependency is a third-party module the project requires.\npackage dependency\n\n" +
			"import \"strings\"\n\n// LOUD_VALUE breaks the house rule against screaming snake case.\n" +
			"const LOUD_VALUE = 1\n\n// Trimmed reads the standard library, which the system cache keeps beside it.\n" +
			"var Trimmed = strings.TrimSpace(\" trimmed \")\n",
	})
	project := filepath.Join(moduleCache, "project")
	writeFixtureFiles(test, project, map[string]string{
		"go.mod": "module example.com/project\n\ngo 1.21\n\nrequire example.com/dependency v1.0.0\n\n" +
			"replace example.com/dependency => " + filepath.ToSlash(dependency) + "\n",
		"project.go": "// Package project reads the dependency and a helper of its own.\npackage project\n\n" +
			"import (\n\t\"example.com/dependency\"\n\t\"example.com/project/helper\"\n)\n\n" +
			"// PROJECT_VALUE breaks the house rule the dependency breaks.\n" +
			"const PROJECT_VALUE = dependency.LOUD_VALUE + helper.Offset\n",
		"helper/helper.go": "// Package helper is the project's own, outside the run's scope.\npackage helper\n\n" +
			"// Offset is two.\nconst Offset = 2\n",
	})
	test.Setenv("GOMODCACHE", moduleCache)
	test.Setenv("GOPROXY", "off")
	test.Setenv("GOSUMDB", "off")
	test.Setenv("GOTOOLCHAIN", "local")
	test.Setenv("GOWORK", "off")
	// The cache's files are written read-only otherwise, should go write any, and the test's cleanup could not
	// remove them.
	test.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -modcacherw"))
	return project, filepath.Join(project, "project.go"), moduleCache
}

// A third-party module's package, every file of it in the module cache, is kept by the system cache: a run under
// `--no-cache` writes it there, and the next restores it, counted apart from the standard library's, with the same
// stream. The project's own packages are never kept there, though their files are below the module cache too: its
// helper, outside the run's scope so analyzed for its facts alone as the dependency is, is checked from source each
// run, so exactly one module's package comes from the system cache. No finding names the dependency's file in either
// run, though it breaks the rule the project's file is reported for.
// Not parallel: it sets the go environment of the process, which every go command of the run in it reads
func TestTheSystemCacheKeepsAThirdPartyModuleAndNeverTheProject(test *testing.T) {
	root, inScope, _ := writeSystemModulesProject(test)
	source := standardLibrarySource{stamp: engineTestsStamp, systemCacheRoot: test.TempDir()}
	filling := runEngineWith(test, root, source, rules.All(), "--no-cache", inScope)
	if filling.counts.ModulesFromSystemCache != 0 || filling.counts.SystemCacheWritten == 0 {
		test.Fatalf("want the first run to write the system cache and restore nothing of it, got %+v",
			filling.counts)
	}
	restoring := runEngineWith(test, root, source, rules.All(), "--no-cache", inScope)
	if restoring.counts.ModulesFromSystemCache != 1 || restoring.counts.FromSystemCache == 0 ||
		restoring.counts.SystemCacheWritten != 0 {
		test.Fatalf("want the dependency's one package and the standard library from the system cache, nothing "+
			"written and the project's own helper never kept there, got %+v", restoring.counts)
	}
	if restoring.withoutCounts != filling.withoutCounts {
		test.Fatalf("restoring\n%s\nfilling\n%s", restoring.withoutCounts, filling.withoutCounts)
	}
	for name, run := range map[string]cachedRun{"filling": filling, "restoring": restoring} {
		reported := false
		for line := range strings.Lines(run.stream) {
			if strings.Contains(line, `"kind":"finding"`) && strings.Contains(line, "dependency.go") {
				test.Errorf("the %s run reported the dependency's file: %s", name, line)
			}
			reported = reported || strings.Contains(line, screamingRule) && strings.Contains(line, "project.go")
		}
		if !reported {
			test.Errorf("the %s run reported no %s of the project's file, so the rule never ran:\n%s", name,
				screamingRule, run.stream)
		}
	}
}
