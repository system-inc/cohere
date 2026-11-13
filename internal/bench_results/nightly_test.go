package bench_results

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/needs"
)

// nightlyScript is bench/nightly.zsh, the nightly speed guard, relative to this package.
var nightlyScript = filepath.Join("..", "..", "bench", "nightly.zsh")

// plantedBench stands in for bench/quiet_machine.sh in TestTheNightlyGuardOpensNoQuietWindow. It looks for a quiet
// window marker under the pool's home while it runs, which is when a window the guard opened would hold, says what it
// found, and records nothing, so the night is lost as a bench with no record.
const plantedBench = `#!/bin/zsh
if [[ -n $(find "$COHERE_DEV_HOME" -name quiet -o -name .quiet 2> /dev/null) ]]; then
  print "planted bench: a quiet window marker is present while the bench runs"
else
  print "planted bench: no quiet window marker while the bench runs"
fi
exit 1
`

// The nightly guard opens no quiet window on this Mac (Kirk, 2026-11-12, #8sqsgee): a window holds every repository's
// test gate, ahra's commit make among them. The guard runs at whatever load the machine has, so no quiet window
// marker exists under the pool's home while its bench runs or after the night ends, and its tonight file names what
// held the cores after the bench rather than a window. The script is run from a stand-in checkout holding this
// checkout's sources and a planted bench, with the pool's home a temporary directory, so a guard that did open a
// window would open it there, never on the machine's real pool.
func TestTheNightlyGuardOpensNoQuietWindow(test *testing.T) {
	test.Parallel()
	zsh := needs.Tool(test, "zsh", "the nightly guard is a zsh script")
	needs.Tool(test, "jq", "the nightly guard writes its tonight file with jq")
	needs.Tool(test, "go", "the nightly guard builds its comparison tool with go")
	// Read here, through os, and written into the stand-in, rather than run in place: go test's cache keys a test on
	// the files its process opens, and one only zsh opened would let an edit to the script replay a cached pass.
	script, readError := os.ReadFile(nightlyScript)
	if readError != nil {
		test.Fatal(readError)
	}
	repository, absoluteError := filepath.Abs(filepath.Join("..", ".."))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	checkout := test.TempDir()
	entries, readDirectoryError := os.ReadDir(repository)
	if readDirectoryError != nil {
		test.Fatal(readDirectoryError)
	}
	for _, entry := range entries {
		if entry.Name() == "bench" || entry.Name() == ".git" {
			continue
		}
		if linkError := os.Symlink(filepath.Join(repository, entry.Name()),
			filepath.Join(checkout, entry.Name())); linkError != nil {
			test.Fatal(linkError)
		}
	}
	bench := filepath.Join(checkout, "bench")
	if createDirectoryError := os.Mkdir(bench, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	for name, contents := range map[string][]byte{"nightly.zsh": script, "quiet_machine.sh": []byte(plantedBench)} {
		if writeError := os.WriteFile(filepath.Join(bench, name), contents, 0o755); writeError != nil {
			test.Fatal(writeError)
		}
	}

	home := test.TempDir()
	state := test.TempDir()
	// The isolation tier, the landing measurement and the live walks are off: they run real tests for minutes,
	// which this test of the bench's quiet window has no use for.
	night := exec.Command(zsh, filepath.Join(bench, "nightly.zsh"), "--state", state, "--projects", "ahra",
		"--cohere", "/usr/bin/true", "--isolation", "off", "--landing", "off", "--live", "off")
	night.Env = append(os.Environ(), "COHERE_DEV_HOME="+home)
	output, runError := night.CombinedOutput()
	if runError != nil {
		test.Fatalf("the night exited %v:\n%s", runError, output)
	}

	logs, globError := filepath.Glob(filepath.Join(state, "logs", "*-ahra.log"))
	if globError != nil || len(logs) != 1 {
		test.Fatalf("the night left logs %q (%v), want one for ahra:\n%s", logs, globError, output)
	}
	log, readLogError := os.ReadFile(logs[0])
	if readLogError != nil {
		test.Fatal(readLogError)
	}
	if !strings.Contains(string(log), "planted bench: no quiet window marker while the bench runs") {
		test.Errorf("the planted bench did not run without a quiet window marker:\n%s", log)
	}
	walkError := filepath.WalkDir(home, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.Name() == "quiet" || entry.Name() == ".quiet" {
			test.Errorf("the night left a quiet window marker at %s", path)
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}

	tonight, readTonightError := os.ReadFile(filepath.Join(state, "tonight-ahra.json"))
	if readTonightError != nil {
		test.Fatalf("the night wrote no tonight file: %v\n%s", readTonightError, output)
	}
	var fields map[string]json.RawMessage
	if decodeError := json.Unmarshal(tonight, &fields); decodeError != nil {
		test.Fatal(decodeError)
	}
	if _, has := fields["window"]; has {
		test.Errorf("the tonight file still records a window:\n%s", tonight)
	}
	if _, has := fields["busiestAfterBench"]; !has {
		test.Errorf("the tonight file does not say what held the cores after the bench:\n%s", tonight)
	}
}
