package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	atLink, linkExit := runCohereWithEnvironment(test, binary, linked, environment, "--no-fix", "--no-cache")
	if realExit != 1 || !strings.Contains(atReal, "consistency-no-screaming-snake-case") {
		test.Fatalf("the run at the real path exited %d, want 1 with its finding:\n%s", realExit, atReal)
	}
	durations := regexp.MustCompile(`[0-9]+(\.[0-9]+)?(ms|s)\b`)
	if linkExit != realExit || durations.ReplaceAllString(atLink, "") != durations.ReplaceAllString(atReal, "") {
		test.Errorf("at the link, exit %d:\n%s\nat the real path, exit %d:\n%s", linkExit, atLink, realExit, atReal)
	}
}

// A Go engine whose types record says its load reached packages its census counted no file of is never complete
// through the front door (#yyv7nr2): a stream that says so and calls itself incomplete fails with the sentence that
// names the census, and one that calls itself complete beside it is refused, since that is a green over nothing.
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
				`"build":"test","censusMissedPackages":673}`,
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
	missed := "its load reached 673 packages in the module while its census counted no Go files there"

	// Not parallel: it drives a check run through engineRun, which sets the package-level activeSummary.
	test.Run("an incomplete run names the census", func(test *testing.T) {
		activeSummary = runSummary{}
		out, exitCode, runError := runFakeGoEngine(test, stream(false, 1))
		if runError != nil || exitCode != 1 {
			test.Fatalf("exit %d, err %v, want 1 and no refusal\n%s", exitCode, runError, out)
		}
		if activeSummary.Gaps.CensusMissedPackages != 673 || !activeSummary.failed() {
			test.Errorf("the summary's gap counts %d packages and failed %t, want 673 and a failure",
				activeSummary.Gaps.CensusMissedPackages, activeSummary.failed())
		}
		got := footer(activeSummary, plain, footerOptions{})
		failing := "no files checked: the census counted no file of the 673 packages the load reached"
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
