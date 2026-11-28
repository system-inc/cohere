//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
  touch "$GATE_BARRIER/$$"
  waited=0
  while [ "$(ls "$GATE_BARRIER" | wc -l)" -lt "$GATE_BARRIER_COUNT" ]; do
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
				if !strings.Contains(said, boxStepMarker+step+" ") || !strings.Contains(said, " "+boxStepBeside+"\n") {
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
