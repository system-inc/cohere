package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/release/dispatch"
	"github.com/system-inc/cohere/internal/release/packaging"
)

// `cohere --version` may say which commit's Swift sources the engine was built from only when the
// launcher's committed path vouches for it. Claiming a commit for an engine nothing vouches for is the
// provenance failure this line exists to avoid, so each test pins one direction of that.

func TestTheSwiftEngineLineNamesACommitOnlyWhenOneIsVouchedFor(test *testing.T) {
	test.Parallel()
	provenance := contractFixture(test, "Clean.jsonl")[0]
	commit := "a97349d9534c52c831ec0835fe7ceba880637fd1"

	render := func(sourceCommit string) string {
		var out bytes.Buffer
		run := newEngineRun(&out, engineModeVersion, swiftLanguage, "", time.Now())
		run.engineSourceCommit = sourceCommit
		if acceptError := run.accept([]byte(provenance)); acceptError != nil {
			test.Fatal(acceptError)
		}
		if _, finishError := run.finish(0, "exited 0"); finishError != nil {
			test.Fatal(finishError)
		}
		return out.String()
	}

	vouched := render(commit)
	requireLines(test, vouched,
		"swift engine: cohere-swift 0.1.0 (commit dev, swiftlang-6.4.0.34.1, swift-syntax 604.0.0, swift-format 604.0.0)\n",
		"  the Swift engine was built from Swift sources identical to commit a97349d9534c, as the launcher's cache key requires\n",
	)

	// The engine's own `commit dev` stays: it is true of the binary, which serves many commits.
	unvouched := render("")
	if strings.Contains(unvouched, "identical to commit") {
		test.Fatalf("an engine nothing vouches for was given a commit:\n%s", unvouched)
	}
	if !strings.Contains(unvouched, "(commit dev,") {
		test.Fatalf("the engine's own provenance line is missing:\n%s", unvouched)
	}
}

func TestOnlyACleanNamedCohereOffersACommitToTheSwiftEngine(test *testing.T) {
	test.Parallel()
	commit := "a97349d9534c52c831ec0835fe7ceba880637fd1"
	cases := map[string]struct {
		provenance release.Provenance
		want       string
	}{
		"clean, named":       {release.Provenance{SelfCommit: commit}, commit},
		"dirty, named":       {release.Provenance{SelfCommit: commit, SourceTreeModified: true}, ""},
		"no commit to offer": {release.Provenance{}, ""},
	}
	for name, testCase := range cases {
		if got := committedSwiftSource(testCase.provenance); got != testCase.want {
			test.Errorf("%s: offered %q, expected %q", name, got, testCase.want)
		}
	}
}

// Not parallel: it sets the Swift engine override variable with test.Setenv, which a parallel test may not
func TestAnEngineNamedByTheOverrideVouchesForNoCommit(test *testing.T) {
	engine := filepath.Join(test.TempDir(), "cohere-swift")
	if writeError := os.WriteFile(engine, []byte("#!/bin/sh\n"), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	test.Setenv(dispatch.SwiftEngineOverrideVariable, engine)

	// A clean named commit, so a resolver that handed the committed source to the override would show it.
	clean := release.Provenance{SelfCommit: "a97349d9534c52c831ec0835fe7ceba880637fd1"}
	binaryPath, sourceCommit, resolveSwiftEngineBinaryError := resolveSwiftEngineBinary(projectLocation{}, clean)
	if resolveSwiftEngineBinaryError != nil {
		test.Fatal(resolveSwiftEngineBinaryError)
	}
	if binaryPath != engine {
		test.Fatalf("resolved %s, the override named %s", binaryPath, engine)
	}
	if sourceCommit != "" {
		test.Fatalf("an engine named by %s vouched for commit %s, which nothing guarantees", dispatch.SwiftEngineOverrideVariable, sourceCommit)
	}
}
