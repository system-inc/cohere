//go:build unix

// The fixture's one finding is in a `_windows.go` file, which is never the host's build where these tests run.

package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// everyBuildWindowsFile is the fixture's file only windows builds, whose printf finding is the module's only one.
const everyBuildWindowsFile = "greet_windows.go"

// newEveryBuildFixture is a Go module whose only finding is in a file only windows builds, so a run that checked the
// windows build reports it and a run of the host's build alone never loads it (#qqyq61s). The replay fixture's run and
// settings, with files of its own: one every build compiles, clean, and the windows file.
func newEveryBuildFixture(test *testing.T) *goReplayFixture {
	test.Helper()
	parent := test.TempDir()
	fixture := &goReplayFixture{
		test:        test,
		binary:      buildCohere(test),
		parent:      parent,
		root:        filepath.Join(parent, "module"),
		environment: []string{"CGO_ENABLED=0", "GOENV=" + filepath.Join(parent, "go.env")},
	}
	fixture.write("go.mod", "module example.com/every\n\ngo 1.27\n")
	fixture.write("go.sum", "")
	fixture.write(".gitignore", ".cache/\n")
	fixture.write("CohereSettings.json", goReplaySettings)
	fixture.write("greet.go", "package every\n\n// Greet greets.\nfunc Greet() string {\n\treturn \"hello\"\n}\n")
	fixture.write(everyBuildWindowsFile, goReplaySource("every", "OnWindows"))
	return fixture
}

// everyBuildFindingFiles are the files of a `--json` run's finding records, by base name.
func everyBuildFindingFiles(output string) []string {
	var files []string
	for _, line := range strings.Split(output, "\n") {
		var record struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(line), &record) == nil && record.Kind == "finding" {
			files = append(files, filepath.Base(record.Path))
		}
	}
	return files
}

// everyBuildUnchecked is a `--json` run's summary's `gaps.unchecked`, each reason by the file's base name, and how
// many builds the summary names.
func everyBuildUnchecked(test *testing.T, output string) (map[string]string, int) {
	test.Helper()
	var summary struct {
		Builds []checkedBuild `json:"builds"`
		Gaps   struct {
			Unchecked []uncheckedEngineFile `json:"unchecked"`
		} `json:"gaps"`
	}
	if decodeError := json.Unmarshal([]byte(summaryJsonOf(test, output)), &summary); decodeError != nil {
		test.Fatalf("the summary line does not decode: %v\n%s", decodeError, output)
	}
	reasons := map[string]string{}
	for _, unchecked := range summary.Gaps.Unchecked {
		reasons[filepath.Base(unchecked.File)] = unchecked.Reason
	}
	return reasons, len(summary.Builds)
}

// TestAPlainGoRunChecksEveryBuild: a run with no switch checks every build the module's files name beside the host's
// (#qqyq61s), so the finding in the file only windows builds is reported and fails the run, the summary names the
// builds it checked, and the file is no gap.
func TestAPlainGoRunChecksEveryBuild(test *testing.T) {
	test.Parallel()
	fixture := newEveryBuildFixture(test)
	plain := fixture.run(false, "--no-fix", "--json")
	if plain.exitCode != 1 || !slices.Contains(everyBuildFindingFiles(plain.stdout), everyBuildWindowsFile) {
		test.Fatalf("a plain run exited %d without the finding in %s, so it never checked the windows build:\n%s%s",
			plain.exitCode, everyBuildWindowsFile, plain.stdout, plain.stderr)
	}
	unchecked, builds := everyBuildUnchecked(test, plain.stdout)
	if reason, named := unchecked[everyBuildWindowsFile]; named {
		test.Errorf("a plain run names %s as not checked, %q, though the windows build checked it:\n%s",
			everyBuildWindowsFile, reason, plain.stdout)
	}
	if builds < 2 {
		test.Errorf("a plain run's summary names %d builds, want the host's and windows's:\n%s", builds, plain.stdout)
	}
}

// TestHostBuildOnlyLeavesAnotherBuildsFileAGap: `--host-build-only` checks the host's build alone, as a plain run did
// before every build was the default (#72n9mq0, #qqyq61s): the file only windows builds is never loaded, so the run is
// clean with no build named, and the file is named not checked, with its constraint's reason.
func TestHostBuildOnlyLeavesAnotherBuildsFileAGap(test *testing.T) {
	test.Parallel()
	fixture := newEveryBuildFixture(test)
	hostOnly := fixture.run(false, "--no-fix", "--json", "--host-build-only")
	if hostOnly.exitCode != 0 || slices.Contains(everyBuildFindingFiles(hostOnly.stdout), everyBuildWindowsFile) {
		test.Fatalf("a --host-build-only run exited %d, or reported %s, which the host never builds:\n%s%s",
			hostOnly.exitCode, everyBuildWindowsFile, hostOnly.stdout, hostOnly.stderr)
	}
	unchecked, builds := everyBuildUnchecked(test, hostOnly.stdout)
	if reason := unchecked[everyBuildWindowsFile]; !strings.HasPrefix(reason, "excluded by build constraints on ") {
		test.Errorf("a --host-build-only run names %s as not checked for %q, want the constraint's reason:\n%s",
			everyBuildWindowsFile, reason, hostOnly.stdout)
	}
	if builds != 0 {
		test.Errorf("a --host-build-only run's summary names %d builds, want none:\n%s", builds, hostOnly.stdout)
	}
}

// TestTheOtherBuildsSwitchChangesNothing: `--other-builds` asked for what every run now does, and is dropped before
// the flags are read for one land, so the self-hosting gate main still runs can pass it (#qqyq61s): a run naming it
// checks every build, as a plain run does, and the next commit removes it.
func TestTheOtherBuildsSwitchChangesNothing(test *testing.T) {
	test.Parallel()
	fixture := newEveryBuildFixture(test)
	named := fixture.run(false, "--no-fix", "--json", "--other-builds")
	if named.exitCode != 1 || !slices.Contains(everyBuildFindingFiles(named.stdout), everyBuildWindowsFile) {
		test.Errorf("a run naming --other-builds exited %d without the finding in %s, want a plain run's:\n%s%s",
			named.exitCode, everyBuildWindowsFile, named.stdout, named.stderr)
	}
}

// TestTheOtherBuildsSwitchRefusesAValue: `--other-builds` given a value is refused by name for the land it lives
// (#qqyq61s), since `--other-builds=false`, dropped as the bare switch is, would check every build while it asks for
// the host's alone. The run fails before checking anything, so no finding is reported.
func TestTheOtherBuildsSwitchRefusesAValue(test *testing.T) {
	test.Parallel()
	fixture := newEveryBuildFixture(test)
	for _, argument := range []string{"--other-builds=false", "-other-builds=true"} {
		refused := fixture.run(false, "--no-fix", "--json", argument)
		if refused.exitCode != 1 || !strings.Contains(refused.stderr, argument[strings.Index(argument, "o"):]) ||
			len(everyBuildFindingFiles(refused.stdout)) != 0 {
			test.Errorf("a run naming %s exited %d with findings in %v, want it refused by name before any check:\n"+
				"%s%s", argument, refused.exitCode, everyBuildFindingFiles(refused.stdout), refused.stdout,
				refused.stderr)
		}
	}
}

// TestAPlainRunAndAHostBuildOnlyRunNeverReplayEachOther: the run cache keys a plain run, which checks every build, and
// a `--host-build-only` run of the same tree apart, since their verdicts differ (#qqyq61s): each recorded after the
// other is never answered by the other's record, and each run again replays its own.
func TestAPlainRunAndAHostBuildOnlyRunNeverReplayEachOther(test *testing.T) {
	test.Parallel()
	fixture := newEveryBuildFixture(test)
	plainArguments := []string{"--no-fix", "--json"}
	hostOnlyArguments := []string{"--no-fix", "--json", "--host-build-only"}
	plain := fixture.run(true, plainArguments...)
	if plain.exitCode != 1 {
		test.Fatalf("the plain run exited %d without the windows file's finding, so it proves nothing:\n%s%s",
			plain.exitCode, plain.stdout, plain.stderr)
	}
	hostOnly := fixture.run(true, hostOnlyArguments...)
	if isGoReplay(hostOnly) || hostOnly.exitCode != 0 {
		test.Fatalf("the --host-build-only run after a plain one exited %d, replayed %t, want a check of its own "+
			"that exits 0:\n%s", hostOnly.exitCode, isGoReplay(hostOnly), hostOnly.stdout)
	}
	// Each again, so the misses above were the key's and not a cache that never replays.
	if plainAgain := fixture.run(true, plainArguments...); !isGoReplay(plainAgain) || plainAgain.exitCode != 1 {
		test.Errorf("the plain run again exited %d, replayed %t, want its own record replayed:\n%s",
			plainAgain.exitCode, isGoReplay(plainAgain), plainAgain.stdout)
	}
	if hostOnlyAgain := fixture.run(true, hostOnlyArguments...); !isGoReplay(hostOnlyAgain) ||
		hostOnlyAgain.exitCode != 0 {
		test.Errorf("the --host-build-only run again exited %d, replayed %t, want its own record replayed:\n%s",
			hostOnlyAgain.exitCode, isGoReplay(hostOnlyAgain), hostOnlyAgain.stdout)
	}
}
