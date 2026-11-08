package go_engine

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_else_return"
)

// separatorPackage is the other-builds fixture's package with a file only windows builds, and labelPackage the one
// with a file only a build with the cohere_extra tag builds.
const (
	separatorPackage = "example.com/builds/internal/separator"
	labelPackage     = "example.com/builds/internal/label"
)

// runOver runs the engine over root with the registry's rules and the findings cache off, so two runs over copies of
// one fixture read alike, with extra after the root, and returns the pipeline it ran and its stream, normalized.
func runOver(test *testing.T, root string, extra ...string) (*pipeline, string) {
	test.Helper()
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-cache"}, extra...)
	request, parseError := parseInvocation(arguments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	registered := rules.All()
	ruleSettings, settingsError := loadSettings(request, registered)
	if settingsError != nil {
		test.Fatal(settingsError)
	}
	if optionsError := configureRules(ruleSettings, registered); optionsError != nil {
		test.Fatal(optionsError)
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	var standardOutput bytes.Buffer
	engine := newPipeline(request, described, ruleSettings, registered, &standardOutput, io.Discard, nil)
	if exitCode := engine.run(); exitCode != exitClean {
		test.Fatalf("the run exited %d, want %d:\n%s", exitCode, exitClean, standardOutput.String())
	}
	return engine, normalize(standardOutput.String(), root)
}

// Under `--other-builds` a module with a file only windows builds and one only the cohere_extra tag builds is
// checked under three builds, the host's first, and mergeBuildRuns is handed a run for each: windows's closure holds
// the package with its windows file, and the tagged build's the package with its tagged file, each having a say on
// that file. Without the switch nothing is handed over, and the two runs write the same stream, byte for byte, since
// until the merge is built the switch writes nothing.
func TestOtherBuildsHandTheMergeARunPerBuild(test *testing.T) {
	test.Parallel()
	off, offStream := runOver(test, copyFixture(test, filepath.Join("testdata", "other-builds")))
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	on, onStream := runOver(test, root, "--other-builds")
	if off.buildRuns != nil {
		test.Errorf("without the switch the merge was handed %d runs, want none", len(off.buildRuns))
	}
	if onStream != offStream {
		test.Errorf("the switch changed the stream:\nwith it\n%s\nwithout it\n%s", onStream, offStream)
	}

	if len(on.buildRuns) != 3 {
		test.Fatalf("the merge was handed %d runs, want 3", len(on.buildRuns))
	}
	host := on.buildRuns[0]
	if slices.Contains(host.build.Tags, "cohere_extra") {
		test.Errorf("the host's build %s sets the fixture's tag", host.build)
	}
	if host.result == nil || len(host.said) == 0 {
		test.Errorf("the host's run has no result (%t) or says nothing (%d files)", host.result == nil,
			len(host.said))
	}
	windowsFile := filepath.Join(root, "internal", "separator", "separator_windows.go")
	taggedFile := filepath.Join(root, "internal", "label", "prefix_extra.go")
	if host.compiled[windowsFile] || host.compiled[taggedFile] {
		test.Errorf("the host compiled a file only another build compiles")
	}

	// The extra builds are in the order BuildsFor prefers them, which puts the host's own `GOOS` first, so the tagged
	// build comes before windows on any host but windows, and each is found by what it builds.
	windowsAt := slices.IndexFunc(
		on.buildRuns,
		func(run buildRun) bool { return run.build.OperatingSystem == "windows" },
	)
	taggedAt := slices.IndexFunc(on.buildRuns, func(run buildRun) bool {
		return slices.Contains(run.build.Tags, "cohere_extra")
	})
	if windowsAt < 1 || taggedAt < 1 || windowsAt == taggedAt {
		test.Fatalf("the extra builds are %s and %s, want one for windows and one with the cohere_extra tag",
			on.buildRuns[1].build, on.buildRuns[2].build)
	}
	windows, tagged := on.buildRuns[windowsAt], on.buildRuns[taggedAt]
	if !windows.analyzed[separatorPackage] || !windows.said[windowsFile] {
		test.Errorf("windows's closure %s leaves out %s, or it has no say on its windows file",
			sortedKeys(windows.analyzed), separatorPackage)
	}
	if windows.analyzed[labelPackage] || windows.said[taggedFile] {
		test.Errorf("windows's closure %s holds %s, which compiles there as the host's does",
			sortedKeys(windows.analyzed), labelPackage)
	}
	if !tagged.analyzed[labelPackage] || !tagged.said[taggedFile] || tagged.analyzed[separatorPackage] {
		test.Errorf("%s's closure is %s, want %s and not %s", tagged.build, sortedKeys(tagged.analyzed),
			labelPackage, separatorPackage)
	}
	for _, extra := range on.buildRuns[1:] {
		if extra.result == nil || len(extra.typeErrors) > 0 {
			test.Errorf("%s ran no rules (%t) or found type errors: %v", extra.build, extra.result == nil,
				extra.typeErrors)
		}
	}

	// The windows file's else after a return is found by windows's run, which ran the rules over its closure, and by
	// no other, since no other build compiles that file.
	elseReturn := consistency_no_else_return.Name
	foundIn := func(run buildRun) bool {
		return slices.ContainsFunc(run.result.Findings, func(finding check.Finding) bool {
			return finding.Rule == elseReturn && finding.File == windowsFile
		})
	}
	if !foundIn(windows) {
		test.Errorf("windows's run found %v, want %s in %s", windows.result.Findings, elseReturn, windowsFile)
	}
	if foundIn(host) || foundIn(tagged) {
		test.Errorf("a build that never compiles %s found %s in it", windowsFile, elseReturn)
	}
}

// A module whose every file the host builds needs no other build, so `--other-builds` hands the merge nothing.
func TestOtherBuildsOverOneBuildRunNothingExtra(test *testing.T) {
	test.Parallel()
	engine, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "clean")), "--other-builds")
	if engine.buildRuns != nil {
		test.Errorf("the merge was handed %d runs over a module one build covers", len(engine.buildRuns))
	}
}

// sortedKeys is a set's members, sorted and joined, for a message.
func sortedKeys(set map[string]bool) string {
	return strings.Join(slices.Sorted(maps.Keys(set)), ", ")
}
