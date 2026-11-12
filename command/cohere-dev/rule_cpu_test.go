//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// The stand-in changes its verdict without changing the land command: failure
// must stop before ordinary tests and fast-forward, and a later attempt must run
// it again rather than use the previous result.
func TestLandRefusesTheRuleCpuBudgetBeforeTests(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	worktreeA := fixture.worktree("a")
	path := filepath.Join(worktreeA, ruleCpuCheck)
	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(path, []byte("stand-in budget check\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.git(worktreeA, "add", "internal")
	fixture.git(worktreeA, "commit", "-q", "-m", "internal: a rule CPU check (#"+fixtureTask+")")
	bin := test.TempDir()
	marker := filepath.Join(bin, "under-budget")
	log := filepath.Join(bin, "commands")
	stand := `#!/bin/sh
printf '%s\n' "$*" >> '` + log + `'
if [ "$1" = vet ]; then exit 0; fi
case "$*" in
 *` + ruleCpuTest + `*)
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
	if writeError := executable_testing.WriteFile(filepath.Join(bin, "go"), []byte(stand), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	environment := append(fixture.environment, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	land := func() (string, error) {
		command := exec.Command(fixture.wrapper, "land", "--message", "a rule CPU check")
		command.Dir, command.Env = worktreeA, environment
		output, outputError := command.CombinedOutput()
		return string(output), outputError
	}
	before := fixture.git(fixture.repository, "rev-parse", "main")
	if output, landError := land(); landError == nil ||
		!strings.Contains(output, "planted-rule 501ms > 500ms") ||
		strings.Contains(output, "landed ") {
		test.Fatalf("over-budget land was not refused (%v):\n%s", landError, output)
	}
	commands, readError := os.ReadFile(log)
	if readError != nil {
		test.Fatal(readError)
	}
	lines := strings.Split(strings.TrimSpace(string(commands)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "vet ") || !strings.Contains(
		lines[1],
		"-count=1 -timeout=30s -run=^"+ruleCpuTest+"$ -v ./internal/lint/registry",
	) {
		test.Fatalf(
			"commands before refusal: %q; want vet then uncached bounded CPU check, without ordinary tests",
			lines,
		)
	}
	if got := fixture.git(fixture.repository, "rev-parse", "main"); got != before {
		test.Fatalf("refused land moved main: %s -> %s", before, got)
	}
	if writeError := os.WriteFile(marker, nil, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if output, landError := land(); landError != nil ||
		!strings.Contains(output, "rule CPU budget passed") ||
		!strings.Contains(output, "landed ") {
		test.Fatalf("under-budget land did not land (%v):\n%s", landError, output)
	}
}

func TestCohereHasTheRuleCpuCheckTheGateRuns(test *testing.T) {
	test.Parallel()
	source, readError := os.ReadFile(filepath.Join("..", "..", ruleCpuCheck))
	if readError != nil {
		test.Fatal(readError)
	}
	if !strings.Contains(string(source), "func "+ruleCpuTest+"(test *testing.T)") {
		test.Fatal("the gate's named test is missing")
	}
}
