package go_engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Each side lints a file in one package of those that compile it, the in-package test variant where the package has
// test files there and the package itself where it has none (load's analyzedOfGraph), while the census pairs a body
// by its package's ID, and standing reads the host's entry of that same ID (#bed0mnk, the Square's should 1 on the
// three-piece head). In body-reach-lint-package the host alone has gate's in-package test file, which imports unsafe,
// so the host lints shared.go in "gate [gate.test]", where unusedwrite stands down, and windows lints it in gate and
// reports Target. Target's imports are gate's on both sides, nothing, so it is unreached. Read by the file alone, the
// differential compared windows's gate with the host's test variant and failed a body no real run stands. Read by the
// paired ID, the host lints shared.go in no package of gate's ID, so Target's findings are not compared, counted apart.
// The second check holds the real run: windows's own findings with the standing bodies' switch off and on are alike,
// and both report Target, so the case has a finding to lose.
func TestTheBodyDifferentialReadsEachSidesFindingsByThePairedPackage(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach-lint-package"))
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
	if reason := judgedBody(census, "example.test/gate", "Target"); reason != "unreached" {
		test.Fatalf("windows's census judges Target %s, want it unreached, so compared by its types", reason)
	}
	var failures []string
	for _, failure := range differential.failures {
		failures = append(failures, strings.ReplaceAll(failure, root+string(filepath.Separator), ""))
	}
	if len(failures) > 0 {
		test.Errorf("want the body differential to read no failure, got\n%s", strings.Join(failures, "\n"))
	}
	if differential.lintedApart == 0 {
		test.Error("want Target's findings counted apart, linted in another package on the host, got none")
	}

	var found [2][]string
	for index, switched := range []bool{false, true} {
		standing, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "body-reach-lint-package")),
			exitFindings, func(engine *pipeline) {
				engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
				engine.standingBodies = switched
			}, "--no-cache", "--no-fix", "--build-dependence")
		if len(standing.buildRuns) != 2 || standing.buildRuns[1].build.OperatingSystem != "windows" {
			test.Fatalf("want the host's build and windows's, got %d runs", len(standing.buildRuns))
		}
		for _, finding := range standing.buildRuns[1].result.Findings {
			if filepath.Base(finding.File) == "shared.go" {
				found[index] = append(found[index], finding.Rule)
			}
		}
	}
	if !slices.Equal(found[0], found[1]) || !slices.Contains(found[0], "cohere-go/unusedwrite") {
		test.Errorf("want windows's findings in shared.go alike with standing off and on, unusedwrite among them, "+
			"got off %v, on %v", found[0], found[1])
	}
}
