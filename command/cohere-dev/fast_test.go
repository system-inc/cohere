package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every entry the fast tier leaves to the landing gate still costs more than fastBudget, measured alone
// now. One that got cheap belongs back in the edit loop, and one that names nothing is refused. Recorded
// costs of zero are refused too, so an entry cannot be added unmeasured. CPU is user plus system time,
// which the load does not inflate the way it inflates wall.
//
// It is itself the landing gate's: under the fast tier it skips, since measuring three entries costs
// about 30s of CPU in an edit loop meant to be instant.
//
// Not parallel: it measures wall, and the package's other tests beside it would add to what it reads.
func TestEveryLandingGateOnlyEntryStillEarnsItsPlace(t *testing.T) {
	if os.Getenv(fastTierVariable) != "" {
		t.Skip("measuring the landing gate's entries is the landing gate's job, left to it by cohere-dev test --fast")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	packages, err := modulePackagesIn(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range landingGateOnly {
		// Not parallel: each entry's measurement would compete with the others' for the cores.
		t.Run(entry.pattern, func(t *testing.T) {
			if !packages[entry.pattern] {
				t.Fatalf("%s is not a package of this module", entry.pattern)
			}
			if entry.wallSeconds <= 0 || entry.cpuSeconds <= 0 || entry.reason == "" {
				t.Fatalf("%s is listed without its measured wall, CPU or reason", entry.pattern)
			}
			wall, cpu := measureTests(t, root, entry.pattern, nil)
			if entry.skippedBy != "" {
				fastWall, fastCPU := measureTests(t, root, entry.pattern, []string{entry.skippedBy + "=1"})
				wall, cpu = wall-fastWall, cpu-fastCPU
			}
			t.Logf("%s: %.1fs wall, %.1fs CPU now (recorded %.1fs, %.1fs)", entry.pattern, wall, cpu, entry.wallSeconds, entry.cpuSeconds)
			if wall <= fastBudget.wallSeconds && cpu <= fastBudget.cpuSeconds {
				t.Errorf("%s costs %.1fs of wall and %.1fs of CPU alone, under the fast budget (%.0fs or %.0fs), so it "+
					"belongs back in the fast tier: remove it from landingGateOnly", entry.pattern, wall, cpu,
					fastBudget.wallSeconds, fastBudget.cpuSeconds)
			}
		})
	}
}

// measureTests runs one package's tests with -count=1 and returns the wall and the CPU, user plus system,
// of go test and everything it waited for. The fast tier's variable is never inherited, so a measurement
// in full is in full whatever ran this test; environment adds it where a caller wants it.
func measureTests(t *testing.T, root string, pattern string, environment []string) (wall float64, cpu float64) {
	t.Helper()
	command := exec.Command("go", "test", "-count=1", pattern)
	command.Dir = root
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, fastTierVariable+"=") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, environment...)
	start := time.Now()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go test -count=1 %s failed, so it measured nothing: %v\n%s", pattern, err, output)
	}
	return time.Since(start).Seconds(), (command.ProcessState.UserTime() + command.ProcessState.SystemTime()).Seconds()
}
