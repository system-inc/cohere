//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
	"github.com/system-inc/cohere/internal/testing/needs"
	"github.com/system-inc/cohere/internal/testing/own_go_cache"
)

// barrierGo stands in for a shared box's go whose test runs wait for each other: each go test it is asked for leaves
// a file in GATE_BARRIER and waits up to 20 seconds for GATE_BARRIER_COUNT of them, so the runs pass only when they
// run at once, and a run asked after another ended waits out its time and fails. GATE_MODULE_FAILS fails the
// module's own run, its package `example.com/module`, after the wait.
const barrierGo = `#!/bin/bash
case "$1" in
env) [ "$2" = GOOS ] && echo darwin; [ "$2" = GOARCH ] && echo arm64; exit 0 ;;
mod) [ "$2" = verify ] && echo "all modules verified"; exit 0 ;;
test)
  # A job whose end is planted unwritable, a directory in the end file's place, ends before it says how it ended.
  case " $* " in
  *" -race -run "*) ;;
  *" -race "*) [ "${GATE_UNSPOKEN:-}" != race ] || mkdir "$(echo "$TMPDIR"/cohere-gate-beside.*)/race.end" ;;
  *) [ "${GATE_UNSPOKEN:-}" != tests ] || mkdir "$(echo "$TMPDIR"/cohere-gate-beside.*)/tests.end" ;;
  esac
  # With GATE_ALONE, a run that finds another in flight beside it says so and fails, so runs one after another pass.
  if [ -n "${GATE_ALONE:-}" ]; then
    mkdir "$GATE_BARRIER/running.$$"; sleep 0.5
    if [ "$(ls -d "$GATE_BARRIER"/running.* | wc -l)" -gt 1 ]; then
      echo '{"Action":"fail","Package":"example.com/beside"}'; rmdir "$GATE_BARRIER/running.$$"; exit 1
    fi
    rmdir "$GATE_BARRIER/running.$$"
  fi
  touch "$GATE_BARRIER/$$"
  waited=0
  while [ "$(ls "$GATE_BARRIER" | grep -cv '^running\.')" -lt "$GATE_BARRIER_COUNT" ]; do
    sleep 0.1; waited=$((waited + 1))
    if [ "$waited" -ge 200 ]; then echo '{"Action":"fail","Package":"example.com/alone"}'; exit 1; fi
  done
  case " $* " in
  *" -race "*) echo '{"Action":"pass","Package":"example.com/raced"}' ;;
  *)
    if [ -n "${GATE_MODULE_FAILS:-}" ]; then echo '{"Action":"fail","Package":"example.com/module"}'; exit 1; fi
    echo '{"Action":"pass","Package":"example.com/module"}' ;;
  esac ;;
esac
`

// The module's run, the -race run of the plain tokens and the narrowed -race run run beside each other (#df7s7ej):
// each waits for the others to start, so run one after another the first would wait out its time and fail. Each job
// says its own step, marked beside, and the stage's wall is said once, as the step beside. Their output is printed in
// the order they ran one after another before, the race marker between, so the verdict reads as it did. And a module
// run that fails prints no -race run, which never ran then, so the gate fails on the module alone.
func TestTheGatesTestRunsRunBesideEachOther(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate is a bash script")
	needs.Tool(test, "sha256sum", "the gate digests the toolchain with it, as the boxes do")
	for _, row := range []struct {
		name        string
		moduleFails bool
	}{{"all passing", false}, {"the module failing", true}} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			goroot := filepath.Join(test.TempDir(), "go")
			writeFixtureFile(test, filepath.Join(goroot, "src", "fmt", "print.go"), "package fmt\n")
			if makeError := os.MkdirAll(filepath.Join(goroot, "bin"), 0o755); makeError != nil {
				test.Fatal(makeError)
			}
			if writeError := executable_testing.WriteFile(filepath.Join(goroot, "bin", "go"), []byte(barrierGo),
				0o755); writeError != nil {
				test.Fatal(writeError)
			}
			digest := toolchainDigest(test, goroot)
			tree := test.TempDir()
			writeFixtureFile(test, filepath.Join(tree, "go.mod"), "module example.com/module\n\ngo 1.27\n")
			barrier := test.TempDir()
			environment := append(own_go_cache.Environment(withoutModuleCache(os.Environ()), test.TempDir()),
				"TMPDIR="+test.TempDir(), "GATE_BARRIER="+barrier, "GATE_BARRIER_COUNT=3")
			if row.moduleFails {
				environment = append(environment, "GATE_MODULE_FAILS=1")
			}
			script := "set -euo pipefail\n" +
				`goroot="$1"; digest="$2"; node="$3"; nodeDigest="$4"; races="$5"; shift 5` + "\n" + sharedBoxGate
			gate := exec.Command("bash", "-c", script, "gate", goroot, digest, filepath.Join(tree, "no-node"),
				strings.Repeat("0", 64), "./internal/edit,./internal/go_language@AtOnce", "./...")
			gate.Dir, gate.Env = tree, environment
			started := time.Now()
			output, gateError := gate.CombinedOutput()
			said := string(output)
			if strings.Contains(said, "example.com/alone") {
				test.Fatalf("a run waited out its time alone, so the runs ran one after another (%s):\n%s",
					time.Since(started).Round(time.Second), said)
			}
			if (gateError != nil) != row.moduleFails {
				test.Fatalf("the gate ended %v, want it failed %t:\n%s", gateError, row.moduleFails, said)
			}
			module := strings.Index(said, `"Package":"example.com/module"`)
			marker := strings.Index(said, boxRaceMarker+"\n")
			raced := strings.Index(said, `"Package":"example.com/raced"`)
			if module < 0 {
				test.Fatalf("the module's run wasn't printed:\n%s", said)
			}
			if row.moduleFails {
				if marker >= 0 || raced >= 0 {
					test.Errorf("a failed module's run printed its -race run:\n%s", said)
				}
				return
			}
			if marker < module || raced < marker {
				test.Errorf("the runs aren't printed module, marker, -race:\n%s", said)
			}
			for _, step := range []string{"tests", "race", "race-runs"} {
				if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(boxStepMarker+step) + ` [0-9]+ ` +
					boxStepBeside + `$`).MatchString(said) {
					test.Errorf("the run doesn't say the step %s beside the others:\n%s", step, said)
				}
			}
			if !strings.Contains(said, boxStepMarker+"beside ") {
				test.Errorf("the run doesn't say the stage's own wall:\n%s", said)
			}
		})
	}
}

// A step said beside others is listed with its own time and never added to the run's wall: the stage's step, said
// once, holds it (#df7s7ej).
func TestAStepBesideOthersIsListedAndNotAddedToTheWall(test *testing.T) {
	test.Parallel()
	verdict := readTestJson([]byte(boxStepMarker + "toolchain 1\n" + boxStepMarker + "tests 700 " + boxStepBeside +
		"\n" + boxStepMarker + "sharded 280 " + boxStepBeside + "\n" + boxStepMarker + "beside 705\n"))
	if verdict.stepsWall != 706*time.Second {
		test.Errorf("the run's wall is %s, want 11m46s, the toolchain's and the stage's", verdict.stepsWall)
	}
	want := []string{"toolchain 1s", "tests 11m40s " + boxStepBeside, "sharded 4m40s " + boxStepBeside,
		"beside 11m45s"}
	if strings.Join(verdict.steps, "|") != strings.Join(want, "|") {
		test.Errorf("the steps read %q, want %q", verdict.steps, want)
	}
}

// gateRun is the gate script run for real with bash against the barrier go, in a tree that is a git repository so the
// run says what it tested, with the race tokens of TestTheGatesTestRunsRunBesideEachOther and the environment given.
func gateRun(test *testing.T, environment ...string) (string, []byte, error) {
	test.Helper()
	goroot := filepath.Join(test.TempDir(), "go")
	writeFixtureFile(test, filepath.Join(goroot, "src", "fmt", "print.go"), "package fmt\n")
	if makeError := os.MkdirAll(filepath.Join(goroot, "bin"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := executable_testing.WriteFile(filepath.Join(goroot, "bin", "go"), []byte(barrierGo),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	digest := toolchainDigest(test, goroot)
	tree := test.TempDir()
	writeFixtureFile(test, filepath.Join(tree, "go.mod"), "module example.com/module\n\ngo 1.27\n")
	gitEnvironment := append(append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com"), gitWithoutMaintenance...)
	for _, arguments := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-q", "-m", "a module"}} {
		git := exec.Command("git", arguments...)
		git.Dir, git.Env = tree, gitEnvironment
		if output, gitError := git.CombinedOutput(); gitError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, gitError, output)
		}
	}
	commit := exec.Command("git", "rev-parse", "HEAD")
	commit.Dir, commit.Env = tree, gitEnvironment
	head, headError := commit.Output()
	if headError != nil {
		test.Fatal(headError)
	}
	script := "set -euo pipefail\n" +
		`goroot="$1"; digest="$2"; node="$3"; nodeDigest="$4"; races="$5"; shift 5` + "\n" + sharedBoxGate
	gate := exec.Command("bash", "-c", script, "gate", goroot, digest, filepath.Join(tree, "no-node"),
		strings.Repeat("0", 64), "./internal/edit,./internal/go_language@AtOnce", "./...")
	gate.Dir = tree
	gate.Env = append(append(own_go_cache.Environment(withoutModuleCache(os.Environ()), test.TempDir()),
		"TMPDIR="+test.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"), environment...)
	output, gateError := gate.CombinedOutput()
	return strings.TrimSpace(string(head)), output, gateError
}

// A job that ends before it says how it ended, its end file never written, fails the run in words, for a module job as
// for a -race job, and the box's run reads as refused or as no verdict naming it, never as passed (the Go Square's
// must on #df7s7ej): read as passed, a job killed before its end would land.
func TestAJobThatNeverSaidHowItEndedRefusesTheRun(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate is a bash script")
	needs.Tool(test, "sha256sum", "the gate digests the toolchain with it, as the boxes do")
	races := []string{"./internal/edit", "./internal/go_language@AtOnce"}
	for _, kind := range []string{"tests", "race"} {
		test.Run(kind, func(test *testing.T) {
			test.Parallel()
			commit, output, gateError := gateRun(test, "GATE_BARRIER="+test.TempDir(), "GATE_BARRIER_COUNT=3",
				"GATE_UNSPOKEN="+kind)
			want := "the " + kind + " run ended before it said how it ended"
			if gateError == nil || !strings.Contains(string(output), sharedBoxRefusedMarker+want) {
				test.Fatalf("the gate didn't fail saying %q (%v):\n%s", want, gateError, output)
			}
			verdict, unavailable := readBoxRun(output, gateError, "Shackler", commit, races)
			if !strings.Contains(verdict.refused, want) && !strings.Contains(unavailable, want) {
				test.Errorf("the box's run reads as refused %q, no verdict %q, want either to say %q", verdict.refused,
					unavailable, want)
			}
		})
	}
}

// A box with a core budget runs the test jobs one after another, saying so: each job sizes its go to the whole
// budget, so beside each other they would hold the budget several times over (the Go Square's must on #df7s7ej).
// Each go test the barrier go is asked fails when it finds another in flight beside it, so the run passes only one
// after another.
func TestABudgetedBoxRunsItsTestJobsOneAfterAnother(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate is a bash script")
	needs.Tool(test, "sha256sum", "the gate digests the toolchain with it, as the boxes do")
	_, output, gateError := gateRun(test, "GATE_BARRIER="+test.TempDir(), "GATE_BARRIER_COUNT=1", "GATE_ALONE=1",
		boxCoreBudgetVariable+"=4")
	said := string(output)
	if gateError != nil || strings.Contains(said, "example.com/beside") {
		test.Fatalf("the budgeted run ran its jobs beside each other (%v):\n%s", gateError, said)
	}
	if !strings.Contains(said, boxSeriesMarker+"a core budget of 4 cores runs the test jobs one after another") {
		test.Errorf("the budgeted run doesn't say it runs one job after another:\n%s", said)
	}
	for _, step := range []string{"tests", "race", "race-runs"} {
		if !strings.Contains(said, boxStepMarker+step+" ") {
			test.Errorf("the budgeted run doesn't say the step %s:\n%s", step, said)
		}
	}
}
