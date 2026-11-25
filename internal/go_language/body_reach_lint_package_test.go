package go_language

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check"
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

// A build restoring from the host's run leaves a package nothing reaches untouched (load.Loaded.Untouched), analyzed
// by nothing, so its findings are in no result of the build's and findingsByPackage gives it no entry. The census
// never meets one: a run judging dependence restores nothing from the host's run (censusKeepsHostRestoreOff), with the
// findings store on too, so the body differential compares noisy's Write, which nothing changes, with the findings
// of noisy on both sides, unusedwrite's alike (#bed0mnk, the lead's read of the Square's should 1).
func TestACensusRunLeavesNoPackageUntouched(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach-lint-package"))
	on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
		engine.bodyDifferential = true
	}, "--no-fix", "--build-dependence")
	if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
	}
	windows := on.buildRuns[1]
	if len(windows.result.FromHost.Untouched) > 0 {
		test.Errorf("windows left %v untouched, want nothing in a run judging dependence",
			windows.result.FromHost.Untouched)
	}
	if reason := judgedBody(windows.bodyReach, "example.test/gate/noisy", "Write"); reason != "unreached" {
		test.Errorf("windows's census judges noisy's Write %s, want it unreached, so compared", reason)
	}
	// Only gate's Target, which the host lints in its test variant, is linted apart.
	if windows.bodyDifferential == nil || windows.bodyDifferential.lintedApart != 1 {
		test.Error("want one body's findings counted apart, Target's, and noisy's Write compared")
	}
	noisy := func(run buildRun) []string {
		var found []string
		for _, finding := range run.result.Findings {
			if filepath.Base(finding.File) == "noisy.go" {
				found = append(found, finding.Rule)
			}
		}
		return found
	}
	if !slices.Equal(noisy(windows), []string{"cohere-go/unusedwrite"}) {
		test.Errorf("windows's own findings in noisy.go are %v, want unusedwrite's", noisy(windows))
	}
}

// A body of a package neither side lints, a dependency's or the standard library's a build checks from source, is
// counted apart from one whose file a side lints in another package, so the census's count of bodies linted apart says
// only what its name says (#bed0mnk, census's reading at bcb8b9861, where the two were one count and a tree's
// dependencies made up nearly all of it). compareFindings is handed three bodies: one in a package neither side holds
// an entry for, one in a package the build alone lints, and one both lint, alike.
func TestABodyOfAPackageNeitherSideLintsIsCountedApartFromOneLintedElsewhere(test *testing.T) {
	test.Parallel()

	body := func(identifier string) comparedBody {
		file := "/" + identifier + "/target.go"
		return comparedBody{body: reachedBody{packageIdentifier: identifier, file: file, line: 3, name: "Target"},
			host: placedLines{file: file, firstLine: 3, lastLine: 5}, built: placedLines{file: file, firstLine: 3,
				lastLine: 5}}
	}
	differential := &buildBodyDifferential{bodies: []comparedBody{body("example.test/dependency"),
		body("example.test/apart"), body("example.test/linted")}}
	hostFindings := map[string]map[string][]check.Finding{"example.test/linted": {}}
	builtFindings := map[string]map[string][]check.Finding{"example.test/apart": {}, "example.test/linted": {}}
	differential.compareFindings(hostFindings, builtFindings, nil, &findingCauses{}, nil, nil)

	if differential.lintedByNeither != 1 || differential.lintedApart != 1 || len(differential.failures) > 0 {
		test.Errorf("want 1 body linted by neither, 1 apart and no failure, got %d, %d and %v",
			differential.lintedByNeither, differential.lintedApart, differential.failures)
	}
}
