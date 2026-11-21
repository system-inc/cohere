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
//
// Two rules gate on the import closure, not the direct imports (the Square's must on the three-piece head):
// correctness-no-caller-data-mutation exempts a Read's buffer only where io is in it, and
// correctness-require-handled-error a digest's dropped Write only where hash is. In closure-io and closure-hash the
// gate package's direct imports are the same in both builds, dep alone, and dep imports the gating path in a file of
// the host's alone; closure-io-build and closure-hash-build import it in windows's alone, either half of the
// comparison (differingClosureLookups). The import is blank in closure-io and closure-hash-build, as the reader's
// module has it, and typed in the other two, which holds where dep comes from export data. The reach asks each Closure
// lookup the host's rules recorded of the package again of the build's types, and reaches the gated body, Read or
// Target, by Imports.
func TestAPackageWhoseImportsDifferIsReachedWhole(test *testing.T) {
	test.Parallel()

	// Each module, by the gate package's body whose finding the gate flips.
	for module, gated := range map[string]string{"unsafe": "Target", "cobra": "Target", "goflags": "Target",
		"unsafe-host": "Target", "closure-io": "Source.Read", "closure-io-build": "Source.Read",
		"closure-hash": "Target", "closure-hash-build": "Target"} {
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
			if got[gated] != string(reachedImports) || constantsOnly[gated] != string(reachedImports) {
				test.Errorf("windows's census judges the gate package's bodies %v, and %v under the constants-only "+
					"rule, want %s reached by %s under both", got, constantsOnly, gated, reachedImports)
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
