package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
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
// one fixture read alike, with extra after the root, and returns the pipeline it ran and its stream, normalized. The
// run must exit wantExit.
func runOver(test *testing.T, root string, wantExit int, extra ...string) (*pipeline, string) {
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
	if exitCode := engine.run(); exitCode != wantExit {
		test.Fatalf("the run exited %d, want %d:\n%s", exitCode, wantExit, standardOutput.String())
	}
	return engine, normalize(standardOutput.String(), root)
}

// Under `--other-builds` a module with a file only windows builds and one only the cohere_extra tag builds is
// checked under three builds, the host's first, and mergeBuildRuns is handed a run for each: windows's closure holds
// the package with its windows file, and the tagged build's the package with its tagged file, each having a say on
// that file. Without the switch nothing is handed over and the run is clean, as it was.
func TestOtherBuildsHandTheMergeARunPerBuild(test *testing.T) {
	test.Parallel()
	off, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "other-builds")), exitClean)
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	on, _ := runOver(test, root, exitFindings, "--other-builds")
	if off.buildRuns != nil {
		test.Errorf("without the switch the merge was handed %d runs, want none", len(off.buildRuns))
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

// A module whose every file the host builds needs no other build, so `--other-builds` hands the merge nothing, and the
// run writes the stream it writes without the switch, byte for byte, with no builds in its lint record.
func TestOtherBuildsOverOneBuildRunNothingExtra(test *testing.T) {
	test.Parallel()
	clean := filepath.Join("testdata", "clean")
	engine, onStream := runOver(test, copyFixture(test, clean), exitClean, "--other-builds")
	if engine.buildRuns != nil {
		test.Errorf("the merge was handed %d runs over a module one build covers", len(engine.buildRuns))
	}
	_, offStream := runOver(test, copyFixture(test, clean), exitClean)
	if onStream != offStream {
		test.Errorf("the switch changed the stream over one build:\nwith it\n%s\nwithout it\n%s", onStream,
			offStream)
	}
}

// Under `--other-builds` the lint phase writes the merge of every build's findings (#72n9mq0): the windows file's else
// after a return, which only windows's run finds, is written as a finding, the run exits with findings, and the lint
// record names the three builds, the host's first, with windows's the one that added it. Without the switch the same
// tree reads clean, since the host never compiles that file.
func TestOtherBuildsWriteTheMergedVerdict(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	engine, stream := runOver(test, root, exitFindings, "--other-builds")
	var lint contract.LintRecord
	var found []contract.FindingRecord
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		switch {
		case strings.Contains(line, `"kind":"lint"`):
			if decodeError := json.Unmarshal([]byte(line), &lint); decodeError != nil {
				test.Fatal(decodeError)
			}
		case strings.Contains(line, `"kind":"finding"`):
			var finding contract.FindingRecord
			if decodeError := json.Unmarshal([]byte(line), &finding); decodeError != nil {
				test.Fatal(decodeError)
			}
			found = append(found, finding)
		}
	}
	windowsFile := "/project/internal/separator/separator_windows.go"
	if len(found) != 1 || found[0].Rule != consistency_no_else_return.Name || found[0].File != windowsFile {
		test.Errorf("the run wrote %+v, want the one %s in %s", found, consistency_no_else_return.Name, windowsFile)
	}
	if len(lint.Builds) != 3 || !lint.Builds[0].Host || lint.Builds[1].Host || lint.Builds[2].Host {
		test.Fatalf("the lint record names the builds %+v, want three, the host's first", lint.Builds)
	}
	// The stream reads every target as `target` (normalize), so each record is known by the run it came from, which
	// the merge writes in the order it was handed the runs.
	for index, build := range lint.Builds {
		wantAdded := 0
		if engine.buildRuns[index].build.OperatingSystem == "windows" {
			wantAdded = 1
			if build.Seeds == 0 || build.AnalyzedPackages == 0 {
				test.Errorf("windows's build %+v names no seed or analyzed nothing", build)
			}
		}
		if build.FindingsAdded != wantAdded || build.FindingsDropped != 0 {
			test.Errorf("the build %s added %d and dropped %d, want %d and 0", engine.buildRuns[index].build,
				build.FindingsAdded, build.FindingsDropped, wantAdded)
		}
	}
}

// A //cohere:ignore in a file only another build compiles is read and honored: the merge indexes the directives of a
// file the host's build constraints leave out, from its contents as the run read them, so the windows file's else
// after a return, ignored with a reason on the line above, is withheld and counted, the run is clean, and no build
// calls the directive unused.
func TestOtherBuildsHonorADirectiveInAFileOnlyAnotherBuildCompiles(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	windowsFile := filepath.Join(root, "internal", "separator", "separator_windows.go")
	source, readError := os.ReadFile(windowsFile)
	if readError != nil {
		test.Fatal(readError)
	}
	lines := strings.Split(string(source), "\n")
	elseAt := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "} else {") })
	if elseAt < 0 {
		test.Fatalf("%s has no else to ignore", windowsFile)
	}
	directive := "\t//cohere:ignore " + consistency_no_else_return.Name + " the fixture's planted finding, ignored here"
	lines = slices.Insert(lines, elseAt, directive)
	if writeError := os.WriteFile(windowsFile, []byte(strings.Join(lines, "\n")), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	_, stream := runOver(test, root, exitClean, "--other-builds")
	if !strings.Contains(stream, `"withheldBySuppression":{"`+consistency_no_else_return.Name+`":1}`) {
		test.Errorf("the run withheld nothing of %s:\n%s", consistency_no_else_return.Name, stream)
	}
}

// sortedKeys is a set's members, sorted and joined, for a message.
func sortedKeys(set map[string]bool) string {
	return strings.Join(slices.Sorted(maps.Keys(set)), ", ")
}
