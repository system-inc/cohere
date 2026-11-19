package go_engine

import (
	"testing"
)

// dependencyPath is the import path of the third-party module writeSystemModulesProject's project requires.
const dependencyPath = "example.com/dependency"

// A module's package restored from the system cache whose entry a run without `--build-dependence` wrote has no
// dependence record, and gets none: the system cache's hits are never backfilled, and only the standard library is
// loaded from source beside the host's lint (buildDependenceGraph). So under `--build-dependence` it is among what the
// host restored with no graph, run after run, and the objects of it a unit of the host reads are answered without
// their package, failing wide. The record comes with a later stage (#57wgzhp S1); this pins what holds until then.
// Not parallel: it sets the go environment of the process, which every go command of the run in it reads
func TestAModuleFromTheSystemCacheReadsAsUnrecordedUnderBuildDependence(test *testing.T) {
	root, _, _ := writeSystemModulesProject(test, windowsOnly)
	systemRoot := test.TempDir()
	runOverNoted(test, root, exitFindings, sharingSystemCache(systemRoot), "--no-fix", "--no-cache")
	for _, run := range []string{"first", "second"} {
		engine, stream, _ := runOverNoted(test, root, exitFindings, sharingSystemCache(systemRoot), "--no-fix",
			"--build-dependence")
		if engine.dependence == nil || !engine.dependence.restored[dependencyPath] {
			test.Fatalf("the %s run under --build-dependence: want %s restored with no record", run, dependencyPath)
		}
		if side := engine.dependence.standardLibrary; side != nil {
			if _, sideLoaded := side.files[dependencyPath]; sideLoaded {
				test.Errorf("the %s run loaded %s beside the host's lint, want only the standard library there", run,
					dependencyPath)
			}
		}
		if restored := restoredOf(test, stream); restored.WithoutPackage == 0 {
			test.Errorf("the %s run answered every object the host reads of what it restored, want %s's without its "+
				"package: %+v", run, dependencyPath, restored)
		}
	}
}
