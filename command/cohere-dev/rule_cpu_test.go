//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The stand-in changes its verdict without changing the land command: failure
// must stop before ordinary tests and fast-forward, and a later attempt must run
// it again rather than use the previous result.
func TestLandRefusesTheRuleCPUBudgetBeforeTests(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	a := fixture.worktree("a")
	path := filepath.Join(a, ruleCPUCheck)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stand-in budget check\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.git(a, "add", "internal")
	fixture.git(a, "commit", "-q", "-m", "a rule CPU check")
	bin := t.TempDir()
	marker := filepath.Join(bin, "under-budget")
	log := filepath.Join(bin, "commands")
	stand := `#!/bin/sh
printf '%s\n' "$*" >> '` + log + `'
if [ "$1" = vet ]; then exit 0; fi
case "$*" in
 *TestRegisteredRuleCPUBudget*)
  echo "rule CPU coverage: rules run=1 registered=1 corpus nodes=1 files=1 wall=1ms"
  if [ ! -f '` + marker + `' ]; then
   echo "rule CPU budget exceeded: planted-rule 501ms > 500ms"
   exit 1
  fi
  exit 0
  ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(stand), 0o755); err != nil {
		t.Fatal(err)
	}
	environment := append(fixture.environment, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	land := func() (string, error) {
		command := exec.Command(fixture.wrapper, "land")
		command.Dir, command.Env = a, environment
		output, err := command.CombinedOutput()
		return string(output), err
	}
	before := fixture.git(fixture.repository, "rev-parse", "main")
	if output, err := land(); err == nil || !strings.Contains(output, "planted-rule 501ms > 500ms") || strings.Contains(output, "landed ") {
		t.Fatalf("over-budget land was not refused (%v):\n%s", err, output)
	}
	commands, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(commands)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "vet ") || !strings.Contains(lines[1], "-count=1 -timeout=30s -run=^TestRegisteredRuleCPUBudget$ -v ./internal/lint/registry") {
		t.Fatalf("commands before refusal: %q; want vet then uncached bounded CPU check, without ordinary tests", lines)
	}
	if got := fixture.git(fixture.repository, "rev-parse", "main"); got != before {
		t.Fatalf("refused land moved main: %s -> %s", before, got)
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := land(); err != nil || !strings.Contains(output, "rule CPU budget passed") || !strings.Contains(output, "landed ") {
		t.Fatalf("under-budget land did not land (%v):\n%s", err, output)
	}
}

func TestCohereHasTheRuleCPUCheckTheGateRuns(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("..", "..", ruleCPUCheck))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "func TestRegisteredRuleCPUBudget(t *testing.T)") {
		t.Fatal("the gate's named test is missing")
	}
}
