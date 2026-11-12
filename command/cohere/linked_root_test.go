//go:build unix

// The stand-in Go engine here is a /bin/sh script, as go_engine_test.go's are, so the file builds on Unix.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A plain cohere at a Go root reached through a symbolic link checks what it checks at the root's real path
// (#yyv7nr2): the same finding, the same count of files checked and the same exit, its output the same but for how
// long each run took. Before the engine resolved its root, the run at the link counted no file and passed green.
func TestAPlainCohereAtALinkedRootChecksWhatItsRealPathDoes(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	real, linked := filepath.Join(base, "real"), filepath.Join(base, "linked")
	writeTree(test, real, map[string]string{
		goModuleMarker:   "module example.com/linked\n\ngo 1.27\n",
		"alpha/alpha.go": "// Package alpha is one.\npackage alpha\n\n// MAX_SIZE shouts.\nconst MAX_SIZE = 1\n",
		"beta/beta.go": "// Package beta is two.\npackage beta\n\nimport \"example.com/linked/alpha\"\n\n" +
			"// Size reads alpha.\nconst Size = alpha.MAX_SIZE\n",
	})
	// The Go engine reads the repository's index, so the fixture is a repository git knows.
	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = real
	if initializeOutput, initializeError := initialize.CombinedOutput(); initializeError != nil {
		test.Fatalf("git init: %v\n%s", initializeError, initializeOutput)
	}
	if linkError := os.Symlink(real, linked); linkError != nil {
		test.Fatalf("linking the root: %v", linkError)
	}
	// The child mark is emptied, since a front door that inherited it would refuse to start the engine at all.
	environment := []string{goEngineChildVariable + "="}
	atReal, realExit := runCohereWithEnvironment(test, binary, real, environment, "--no-fix", "--no-cache")
	// PWD names the link, as a shell that changed into it says: os.Getwd answers with it, and a child given the
	// parent's PWD would read the kernel's answer instead, which is the real path, and never meet the link at all.
	atLink, linkExit := runCohereWithEnvironment(test, binary, linked,
		append(slices.Clone(environment), "PWD="+linked), "--no-fix", "--no-cache")
	if realExit != 1 || !strings.Contains(atReal, "consistency-no-screaming-snake-case") {
		test.Fatalf("the run at the real path exited %d, want 1 with its finding:\n%s", realExit, atReal)
	}
	// What each run checked: the module's line, each finding and the footer, each without how long it took. The rest
	// of the account names what the machine had, as its memory, which moves between two runs.
	durations := regexp.MustCompile(`[0-9]+(\.[0-9]+)?(ms|s)\b`)
	checked := func(output string) string {
		var lines []string
		for line := range strings.SplitSeq(output, "\n") {
			if strings.Contains(line, "Go files in the module") || strings.Contains(line, ".go:") ||
				strings.HasPrefix(line, "✗") || strings.HasPrefix(line, "✓") {
				lines = append(lines, durations.ReplaceAllString(line, ""))
			}
		}
		return strings.Join(lines, "\n")
	}
	if linkExit != realExit || checked(atLink) != checked(atReal) || checked(atReal) == "" {
		test.Errorf("at the link, exit %d:\n%s\nat the real path, exit %d:\n%s", linkExit, atLink, realExit, atReal)
	}
}

// A Go engine whose types record names files its load reached and its census never counted is never complete through
// the front door (#yyv7nr2, #hd5bsqh): a stream that says so and calls itself incomplete fails with the gap, the footer
// counting the files and the account naming the first, and one that calls itself complete beside them is refused,
// since that is a green over files nothing checked.
//
// Not parallel: it drives check runs through engineRun, whose record acceptors set the package-level activeSummary.
func TestACensusThatMissedTheLoadIsNeverComplete(test *testing.T) {
	stream := func(complete bool, exitCode int) string {
		return "cat <<'RECORDS'\n" + strings.Join([]string{
			`{"kind":"provenance","contract":3,"engine":"cohere-go","version":"0.1.0","commit":"dev",` +
				`"sourceTreeModified":false,"toolchain":"go1.27.0 darwin/arm64"}`,
			`{"kind":"project","root":"/project","package":"example.com/project","elapsedMilliseconds":1,` +
				`"filesInPackage":0,"filesOurs":0,"filesInScope":0,"scopeDescription":"","targets":[],"excluded":[]}`,
			`{"kind":"fix","filesConsidered":0,"filesRewritten":0,"fixesApplied":0,"fixesRefused":0,` +
				`"refusalsByReason":{},"filesReformatted":0,"filesNotFormatted":0,"notFormattedReasons":{},` +
				`"formatScope":"every Go file in the module"}`,
			`{"kind":"phase","name":"fix","outcome":"ran","elapsedMilliseconds":0,"detail":""}`,
			`{"kind":"types","diagnostics":0,"files":0,"elapsedMilliseconds":0,"filesWithoutRecord":[],` +
				`"build":"test","censusMissed":[{"package":"example.com/project/a","file":"/project/a/a.go"},` +
				`{"package":"example.com/project/b","file":"/project/b/b.go"}]}`,
			`{"kind":"phase","name":"types","outcome":"ran","elapsedMilliseconds":0,"detail":""}`,
			`{"kind":"lint","findings":0,"rulesRun":5,"filesWalked":0,"nodesVisited":0,"elapsedMilliseconds":0,` +
				`"rulesSilent":[],"rulesWatchedAndQuiet":5,"crashes":[],"rulesScopedOff":{},"rulesNotConfigured":[],` +
				`"configNote":""}`,
			`{"kind":"phase","name":"lint","outcome":"ran","elapsedMilliseconds":0,"detail":""}`,
			`{"kind":"phase","name":"unused","outcome":"skipped","elapsedMilliseconds":0,"detail":"not requested"}`,
			`{"kind":"summary","findings":0,"complete":` + map[bool]string{true: "true", false: "false"}[complete] +
				`,"exitCode":` + map[int]string{0: "0", 1: "1"}[exitCode] + `}`,
		}, "\n") + "\nRECORDS\nexit " + map[int]string{0: "0", 1: "1"}[exitCode]
	}
	missed := "its load reached 2 files in the module that its census never counted, /project/a/a.go in " +
		"example.com/project/a first"

	// Not parallel: it drives a check run through engineRun, which sets the package-level activeSummary.
	test.Run("an incomplete run names the census", func(test *testing.T) {
		activeSummary = runSummary{}
		out, exitCode, runError := runFakeGoEngine(test, stream(false, 1))
		if runError != nil || exitCode != 1 {
			test.Fatalf("exit %d, err %v, want 1 and no refusal\n%s", exitCode, runError, out)
		}
		if len(activeSummary.Gaps.CensusMissed) != 2 || !activeSummary.failed() {
			test.Errorf("the summary's gap names %v and failed %t, want both files and a failure",
				activeSummary.Gaps.CensusMissed, activeSummary.failed())
		}
		got := footer(activeSummary, plain, footerOptions{})
		failing := "2 files the load reached and the census never counted"
		if !strings.Contains(got, failing) || strings.Contains(got, "✓") || strings.Contains(got, "no files to check") {
			test.Errorf("the footer does not fail the run on the census: %q", got)
		}
		if !strings.Contains(out, missed) {
			test.Errorf("the account does not name the census:\n%s", out)
		}
	})

	// Not parallel: it drives a check run through engineRun, which sets the package-level activeSummary.
	test.Run("a run that calls itself complete is refused", func(test *testing.T) {
		activeSummary = runSummary{}
		out, exitCode, runError := runFakeGoEngine(test, stream(true, 0))
		if runError == nil || !strings.Contains(runError.Error(), "calls the run complete, and "+missed) {
			test.Fatalf("exit %d, err %v, want the stream refused for calling itself complete\n%s", exitCode,
				runError, out)
		}
	})
}

// A run through a linked root and a run at its real path share one replay key, and a link pointed at another tree
// shares none with the first (#hd5bsqh): the key holds the resolved root and the engine names its inputs at it, so the
// run at the link replays the real path's run, and once the link names another tree its run checks that tree and
// writes that tree's verdict, never the first tree's.
func TestARunThroughALinkedRootReplaysItsRealPathsRunAndNoOtherTreesRun(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	first, second, linked := filepath.Join(base, "first"), filepath.Join(base, "second"), filepath.Join(base, "linked")
	for root, constant := range map[string]string{first: "MAX_SIZE", second: "MaxSize"} {
		writeTree(test, root, map[string]string{
			goModuleMarker: "module example.com/replayed\n\ngo 1.27\n",
			"alpha/alpha.go": "// Package alpha is one.\npackage alpha\n\n// " + constant + " is a size.\nconst " +
				constant + " = 1\n",
		})
		initialize := exec.Command("git", "init", "--quiet")
		initialize.Dir = root
		if initializeOutput, initializeError := initialize.CombinedOutput(); initializeError != nil {
			test.Fatalf("git init: %v\n%s", initializeError, initializeOutput)
		}
	}
	if linkError := os.Symlink(first, linked); linkError != nil {
		test.Fatalf("linking the root: %v", linkError)
	}
	environment := []string{goEngineChildVariable + "="}
	atLink := append(slices.Clone(environment), "PWD="+linked)
	// The footer's marker, since the account says "not replayed" of a run that was not.
	replayedMarker := "• replayed"
	if output, exitCode := runCohereWithEnvironment(test, binary, first, environment, "--no-fix"); exitCode != 1 ||
		strings.Contains(output, replayedMarker) {
		test.Fatalf("the first run at the real path exited %d, want 1, checked rather than replayed:\n%s", exitCode,
			output)
	}
	if output, exitCode := runCohereWithEnvironment(test, binary, linked, atLink, "--no-fix"); exitCode != 1 ||
		!strings.Contains(output, replayedMarker) {
		test.Errorf("the run at the link exited %d, want 1, the real path's run replayed:\n%s", exitCode, output)
	}
	if replaceError := os.Remove(linked); replaceError != nil {
		test.Fatal(replaceError)
	}
	if linkError := os.Symlink(second, linked); linkError != nil {
		test.Fatalf("pointing the link at the second tree: %v", linkError)
	}
	if output, exitCode := runCohereWithEnvironment(test, binary, linked, atLink, "--no-fix"); exitCode != 0 ||
		strings.Contains(output, replayedMarker) {
		test.Errorf("the run at the link, now naming a clean tree, exited %d, want 0 and checked:\n%s", exitCode,
			output)
	}
}
