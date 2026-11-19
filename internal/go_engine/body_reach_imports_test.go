package go_engine

import (
	"cmp"
	"path/filepath"
	"strings"
	"testing"
)

// A rule may judge a body by what its package imports anywhere, a gate no use of the body's own reads (#bed0mnk,
// Rules's audit on #zvwfe26): unusedwrite stands down in a package that imports `unsafe`, SA1015 in one that imports
// cobra, and SA5008 accepts a repeated struct tag key in one that imports go-flags. Each of unsafe, cobra and goflags
// in body-reach-imports, the seat's shapes, imports its path in windows's file alone, cobra and go-flags stubs by
// their modules' replaces, beside a shared Target whose finding the gate flips, and a quiet package whose body nothing
// changes. unsafe-host is unsafe turned round, importing its path in the host's file alone, so the host's build
// stands unusedwrite down and windows's reports it: the first three hold the paths windows's build alone imports and
// the fourth the paths the host's alone does, either half of differingImports (the Square's surviving mutant). The
// reach reaches a package whole when its files import other paths in the build than on the host, Imports, so Target
// is never compared and the body differential reads no failure.
func TestAPackageWhoseImportsDifferIsReachedWhole(test *testing.T) {
	test.Parallel()

	for _, module := range []string{"unsafe", "cobra", "goflags", "unsafe-host"} {
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
			got, constantsOnly := map[string]string{}, map[string]string{}
			for _, body := range census.bodies {
				if body.packageIdentifier == "example.test/gate" {
					got[body.name] = cmp.Or(string(body.reason), "unreached")
					constantsOnly[body.name] = cmp.Or(string(body.constantsOnlyReason), "unreached")
				}
			}
			// Under the constants-only load mode rule too, whose count the census reads beside the general one's.
			if got["Target"] != string(reachedImports) || constantsOnly["Target"] != string(reachedImports) {
				test.Errorf("windows's census judges the gate package's bodies %v, and %v under the constants-only "+
					"rule, want Target reached by %s under both", got, constantsOnly, reachedImports)
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
