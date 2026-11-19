package go_engine

import (
	"cmp"
	"path/filepath"
	"strings"
	"testing"
)

// A rule may judge a body by what its package imports anywhere, a gate no use of the body's own reads (#bed0mnk,
// Rules's audit on #zvwfe26): unusedwrite stands down in a package that imports `unsafe`, SA1015 in one that imports
// cobra, and SA5008 accepts a repeated struct tag key in one that imports go-flags. Each module of body-reach-imports,
// the seat's shapes, imports its path in windows's file alone, cobra and go-flags stubs by their modules' replaces,
// beside a shared Target whose finding the gate flips, and a quiet package whose body nothing changes. The reach
// reaches a package whole when its files import other paths in the build than on the host, Imports, so Target is never
// compared and the body differential reads no failure.
func TestAPackageWhoseImportsDifferIsReachedWhole(test *testing.T) {
	test.Parallel()

	for _, module := range []string{"unsafe", "cobra", "goflags"} {
		test.Run(module, func(test *testing.T) {
			test.Parallel()

			root := copyFixture(test, filepath.Join("testdata", "body-reach-imports", module))
			on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
				engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
				engine.bodyDifferential = true
			}, "--no-cache", "--no-fix", "--build-dependence")

			if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
				test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
			}
			census, differential := on.buildRuns[1].bodyReach, on.buildRuns[1].bodyDifferential
			if census == nil || differential == nil {
				test.Fatal("windows's run has no census or no body differential")
			}
			got := map[string]string{}
			for _, body := range census.bodies {
				if body.packageIdentifier == "example.test/gate" {
					got[body.name] = cmp.Or(string(body.reason), "unreached")
				}
			}
			if got["Target"] != string(reachedImports) {
				test.Errorf("windows's census judges the gate package's bodies %v, want Target reached by %s", got,
					reachedImports)
			}
			var failures []string
			for _, failure := range differential.failures {
				failures = append(failures, strings.ReplaceAll(failure, root+string(filepath.Separator), ""))
			}
			if len(failures) > 0 {
				test.Errorf("want the body differential to read no failure, got\n%s", strings.Join(failures, "\n"))
			}
		})
	}
}
