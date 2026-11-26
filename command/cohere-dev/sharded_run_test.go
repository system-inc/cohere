//go:build unix

package main

import (
	"cmp"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
	"github.com/system-inc/cohere/internal/testing/needs"
)

// shardGo stands in for go in the shards' block. It records each call with its `GOMAXPROCS` to `$SHARD_CALLS`, answers
// a `-list` with a fixed listing, a benchmark among it, or with a build failure when `$SHARD_LIST_FAILS` is set, and a
// `-run` with a pass for exactly the top-level tests its regexp names, each with a subtest, then its package's pass,
// whose Elapsed is how many tests it ran; the test `$SHARD_FAILS` names fails, and its package with it. A shard waits
// until `$SHARD_EXPECTED` shards have started, so shards run one after another are caught waiting alone. A go test
// with neither runs the package whole and passes it.
const shardGo = `#!/bin/sh
printf '%s GOMAXPROCS=%s\n' "$*" "${GOMAXPROCS:-unset}" >> "$SHARD_CALLS"
package=""; run=""; list=""
while [ "$#" -gt 0 ]; do
  case "$1" in
  -list) list=1; shift ;;
  -run) run="$2"; shift ;;
  -timeout) shift ;;
  -*) ;;
  *) package="$1" ;;
  esac
  shift
done
if [ -n "$list" ]; then
  if [ -n "${SHARD_LIST_FAILS:-}" ]; then echo "# $package"; echo "checker_test.go:3:1: expected declaration"; exit 1; fi
  printf '%s\n' TestOne TestTwo BenchmarkThree TestFour ExampleFive TestSix FuzzSeven TestEight TestNine TestTen \
    TestEleven TestTwelve
  printf 'ok  \t%s\t0.1s\n' "$package"
  exit 0
fi
if [ -z "$run" ]; then echo '{"Action":"pass","Package":"'"$package"'","Elapsed":1}'; exit 0; fi
names=$(printf '%s' "$run" | sed 's/^\^(//; s/)\$$//' | tr '|' ' ')
touch "$SHARD_STARTED/$$"
waited=0
while [ "$(ls "$SHARD_STARTED" | wc -l)" -lt "$SHARD_EXPECTED" ] && [ "$waited" -lt 400 ]; do
  sleep 0.05; waited=$((waited + 1))
done
[ "$waited" -lt 400 ] || echo "a shard waited alone: $names" >> "$SHARD_CALLS"
count=0; failed=0
for name in $names; do
  echo '{"Action":"run","Package":"'"$package"'","Test":"'"$name"'"}'
  echo '{"Action":"pass","Package":"'"$package"'","Test":"'"$name"'/a subtest"}'
  if [ "$name" = "${SHARD_FAILS:-}" ]; then
    echo '{"Action":"fail","Package":"'"$package"'","Test":"'"$name"'","Elapsed":0.1}'; failed=1
  else
    echo '{"Action":"pass","Package":"'"$package"'","Test":"'"$name"'","Elapsed":0.1}'
  fi
  count=$((count + 1))
done
if [ "$failed" = 1 ]; then echo '{"Action":"fail","Package":"'"$package"'","Elapsed":'"$count"'}'; exit 1; fi
echo '{"Action":"pass","Package":"'"$package"'","Elapsed":'"$count"'}'
`

// shardListed is what shardGo lists of its package's tests that go test runs, in the listing's order: its benchmark
// is left out.
var shardListed = []string{"TestOne", "TestTwo", "TestFour", "ExampleFive", "TestSix", "FuzzSeven", "TestEight",
	"TestNine", "TestTen", "TestEleven", "TestTwelve"}

// The box's shards' block, run with bash as sharedBoxGate spells it, against a stand-in go: it lists the sharded
// package's tests, keeping its tests, examples and fuzz targets and never its benchmark, makes a shard per two cores,
// the cores Marin's budget when she has one, else sysctl's count, else nproc's on Linux, at least one and never more
// than the tests, deals the tests round-robin in the listing's order, runs every shard at once on 2 threads, and prints
// the listing and each shard's marker before its JSON. Kirk's Mac reads that as one package, passed once, timed by its
// slowest shard, every listed test run exactly once. A shard that fails fails the run; a listing that fails runs the
// package as one go test, with no marker.
func TestTheGatesShardsDealEveryListedTestOnceAcrossTwoThreadProcesses(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate's script is a bash script")
	for _, row := range []struct {
		name string
		// budget is the box's core budget, sysctl what sysctl says of her cores, failing when empty, and nproc nproc's.
		budget, sysctl, nproc string
		listFails             bool
		fails                 string
		// shards is how many shards the block deals, 0 for none.
		shards int
	}{
		{name: "Marin's budget of 10", budget: "10", sysctl: "64", shards: 5},
		{name: "8 cores", sysctl: "8", shards: 4},
		{name: "Linux's 6 cores", nproc: "6", shards: 3},
		{name: "more cores than tests", sysctl: "64", shards: len(shardListed)},
		{name: "one core", sysctl: "1", shards: 1},
		{name: "a shard fails a test", sysctl: "8", fails: "TestSix", shards: 4},
		{name: "a listing that fails", budget: "10", listFails: true},
	} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			stand := test.TempDir()
			calls := filepath.Join(test.TempDir(), "calls")
			started := test.TempDir()
			for name, contents := range map[string]string{
				"go": shardGo,
				"sysctl": "#!/bin/sh\n[ \"$*\" = \"-n hw.ncpu\" ] && [ -n \"$SHARD_SYSCTL\" ] || exit 1\n" +
					"echo \"$SHARD_SYSCTL\"\n",
				"nproc": "#!/bin/sh\necho \"$SHARD_NPROC\"\n",
			} {
				if writeError := executable_testing.WriteFile(filepath.Join(stand, name), []byte(contents),
					0o755); writeError != nil {
					test.Fatal(writeError)
				}
			}
			listFails := ""
			if row.listFails {
				listFails = "1"
			}
			bash := exec.Command("bash", "-s")
			bash.Stdin = strings.NewReader("set -euo pipefail\nsharded=" + boxShardedPackage +
				"\ncountFlag=-count=1\ntestsFailed=0\n" + sharedBoxShards + "echo \"testsFailed=$testsFailed\"\n")
			// Only what the block reads: nothing of the machine's own Go settings reaches it.
			bash.Env = []string{"PATH=" + stand + ":/usr/bin:/bin", "TMPDIR=" + test.TempDir(),
				"SHARD_CALLS=" + calls, "SHARD_STARTED=" + started, fmt.Sprintf("SHARD_EXPECTED=%d", row.shards),
				"SHARD_SYSCTL=" + row.sysctl, "SHARD_NPROC=" + row.nproc, "SHARD_LIST_FAILS=" + listFails,
				"SHARD_FAILS=" + row.fails}
			if row.budget != "" {
				bash.Env = append(bash.Env, boxCoreBudgetVariable+"="+row.budget)
			}
			output, bashError := bash.CombinedOutput()
			if bashError != nil {
				test.Fatalf("the block failed (%v):\n%s", bashError, output)
			}
			said := string(output)
			wantFailed := "testsFailed=0\n"
			if row.fails != "" {
				wantFailed = "testsFailed=1\n"
			}
			if !strings.HasSuffix(said, wantFailed) {
				test.Errorf("the block ended without %q:\n%s", wantFailed, said)
			}
			ran := strings.Split(strings.TrimSpace(string(readMaybeMissing(test, calls))), "\n")
			if row.shards == 0 {
				if strings.Contains(said, boxShardListingMarker) || strings.Contains(said, boxShardMarker) ||
					len(ran) != 2 || strings.Contains(ran[1], "-run") || !strings.HasSuffix(ran[1],
					" -json "+boxShardedPackage+" GOMAXPROCS=unset") {
					test.Errorf("a listing that failed didn't run the package as one go test, unsharded:\n%s\n%s",
						said, strings.Join(ran, "\n"))
				}
				return
			}
			// What each shard should hold: the listing dealt round-robin.
			dealt := make([][]string, row.shards)
			for index, name := range shardListed {
				dealt[index%row.shards] = append(dealt[index%row.shards], name)
			}
			lines := strings.Split(said, "\n")
			if want := boxShardListingMarker +
				boxShardedPackage +
				" " +
				strings.Join(shardListed, " "); !slices.Contains(
				lines, want) {
				test.Errorf("the block printed no listing %q:\n%s", want, said)
			}
			markers := []string{}
			for _, line := range lines {
				if strings.HasPrefix(line, boxShardMarker) {
					markers = append(markers, line)
				}
			}
			wantMarkers := []string{}
			for index, names := range dealt {
				wantMarkers = append(wantMarkers, fmt.Sprintf("%s%d of %d %s %s", boxShardMarker, index+1, row.shards,
					boxShardedPackage, strings.Join(names, " ")))
			}
			if !slices.Equal(markers, wantMarkers) {
				test.Errorf("the shards' markers read\n%s\nwant\n%s", strings.Join(markers, "\n"),
					strings.Join(wantMarkers, "\n"))
			}
			// Every test in exactly one shard's marker.
			inShards := map[string]int{}
			for _, marker := range markers {
				for _, name := range strings.Fields(marker)[6:] {
					inShards[name]++
				}
			}
			for _, name := range shardListed {
				if inShards[name] != 1 {
					test.Errorf("%s is in %d shards' markers, want 1", name, inShards[name])
				}
			}
			runs := 0
			for _, call := range ran {
				switch {
				case strings.HasPrefix(call, "a shard waited alone"):
					test.Errorf("the shards didn't run at once: %s", call)
				case strings.Contains(call, " -run "):
					runs++
					if !strings.HasSuffix(call, " GOMAXPROCS=2") || !strings.Contains(call, "-count=1 -timeout "+
						sharedBoxTestTimeout+" -json -run ^(") {
						test.Errorf("a shard ran as %q, want -count=1, the gate's timeout and 2 threads", call)
					}
				}
			}
			if runs != row.shards {
				test.Errorf("%d shards ran, want %d:\n%s", runs, row.shards, strings.Join(ran, "\n"))
			}
			verdict := readTestJson([]byte(boxMarker + "0123 on darwin/arm64\n" + said))
			verdict.box = "the box"
			// A red run's refusal is its failing test, so its gaps are named whole.
			if len(verdict.shardGaps) > 0 {
				test.Errorf("the shards' coverage refused: %s", cmp.Or(verdict.shardRefusal(),
					strings.Join(verdict.shardGaps, "; ")))
			}
			if !slices.Equal(verdict.packagesReported, []string{boxShardedPackage}) {
				test.Errorf("the package was reported %v, want once", verdict.packagesReported)
			}
			if row.fails != "" {
				if !slices.Equal(verdict.failedTests, []string{boxShardedPackage + " " + row.fails}) ||
					!slices.Equal(verdict.packagesFailed, []string{boxShardedPackage}) || verdict.packagesPassed != 0 {
					test.Errorf("a shard's failure read as tests %v, packages %v failed and %d passed",
						verdict.failedTests, verdict.packagesFailed, verdict.packagesPassed)
				}
				return
			}
			if verdict.packagesPassed != 1 || verdict.failed() {
				test.Errorf("the shards read as %d packages passed, %v failed", verdict.packagesPassed,
					verdict.packagesFailed)
			}
			// The slowest shard is the first, which ran the most tests, a second each.
			if want := fmt.Sprintf("slowest test binaries %s %d.0s (slowest of %d shards)", boxShardedPackage,
				len(dealt[0]), row.shards); verdict.timingReport() != want {
				test.Errorf("the shards' timing reads %q, want %q", verdict.timingReport(), want)
			}
		})
	}
}

// A sharded package's shards read as one package: passed only when every shard passed, its seconds the slowest
// shard's, reported once. And its shards' coverage is the Square's condition: a listed test in no shard, in two, or
// ending other than once across the shards' JSON, a shard never said or never ending, each is a gap naming it, and a
// gap is no verdict. A red shard is said first, its failing test named, whatever gap its run also holds.
func TestAShardedPackageReadsAsOnePackageAndItsShardsCoverItsListing(test *testing.T) {
	test.Parallel()
	event := func(action string, name string) string {
		return `{"Action":"` + action + `","Package":"m/checker","Test":"` + name + `"}`
	}
	end := func(action string, seconds float64) string {
		return fmt.Sprintf(`{"Action":"%s","Package":"m/checker","Elapsed":%g}`, action, seconds)
	}
	listing := boxShardListingMarker + "m/checker TestA TestB TestC"
	shard := func(index int, count int, names string) string {
		return fmt.Sprintf("%s%d of %d m/checker %s", boxShardMarker, index, count, names)
	}
	// green is the whole run of two shards, TestA and TestC in the first and TestB in the second, every test passing.
	green := []string{`{"Action":"pass","Package":"m/plain","Elapsed":2}`, listing,
		shard(1, 2, "TestA TestC"), event("pass", "TestA"), event("pass", "TestA/a subtest"), event("pass", "TestC"),
		end("pass", 4), shard(2, 2, "TestB"), event("pass", "TestB"), end("pass", 9)}
	for _, row := range []struct {
		name  string
		lines []string
		// failedTests is the run's failing tests, and gaps its shards' coverage gaps.
		failedTests []string
		gaps        []string
	}{
		{name: "every shard green", lines: green},
		{name: "a red shard", lines: slices.Concat(green[:7], []string{shard(2, 2, "TestB"), event("fail", "TestB"),
			end("fail", 9)}), failedTests: []string{"m/checker TestB"}},
		// Its shards fell short too, which the red is said before.
		{name: "a red shard whose run also left a test unended", lines: slices.Concat(green[:5],
			[]string{end("pass", 4), shard(2, 2, "TestB"), event("fail", "TestB"), end("fail", 9)}),
			failedTests: []string{"m/checker TestB"},
			gaps:        []string{"TestC (m/checker) ended 0 times across its shards"}},
		{name: "a shard's JSON missing a listed test", lines: slices.Concat(green[:5], green[6:]),
			gaps: []string{"TestC (m/checker) ended 0 times across its shards"}},
		{name: "a test in two shards", lines: slices.Concat(green[:7], []string{shard(2, 2, "TestB TestC"),
			event("pass", "TestB"), event("pass", "TestC"), end("pass", 9)}), gaps: []string{
			"TestC (m/checker) is in shards 1 and 2",
			"TestC (m/checker) ended 2 times across its shards, in shards 1 and 2"}},
		{name: "a test in no shard", lines: slices.Concat(green[:2], []string{shard(1, 2, "TestC"),
			event("pass", "TestC"), end("pass", 4)}, green[7:]), gaps: []string{
			"TestA (m/checker), which its listing holds, is in no shard",
			"TestA (m/checker) ended 0 times across its shards"}},
		{name: "a shard never said", lines: green[:7], gaps: []string{
			"shard 2 of 2 of m/checker never said its tests", "shard 2 of 2 of m/checker ended its test binary 0 times",
			"TestB (m/checker), which its listing holds, is in no shard",
			"TestB (m/checker) ended 0 times across its shards"}},
		{name: "a shard whose test binary never ended", lines: green[:len(green)-1], gaps: []string{
			"shard 2 of 2 of m/checker ended its test binary 0 times"}},
		{name: "a shard's test the listing doesn't hold", lines: slices.Concat(green[:7], []string{
			shard(2, 2, "TestB TestD"), event("pass", "TestB"), event("pass", "TestD"), end("pass", 9)}),
			gaps: []string{"TestD (m/checker) is in shard 2, which its listing doesn't hold",
				"TestD (m/checker) ended, which its listing doesn't hold"}},
		{name: "a shard past its count", lines: slices.Concat(green, []string{shard(3, 2, "TestB")}), gaps: []string{
			"shard 3 of m/checker is past its 2 shards", "TestB (m/checker) is in shards 2 and 3"}},
	} {
		verdict := readTestJson([]byte(boxMarker + "0123 on darwin/arm64\n" + strings.Join(row.lines, "\n") + "\n"))
		verdict.box = "Sun2"
		if !slices.Equal(verdict.shardGaps, row.gaps) {
			test.Errorf("%s: the gaps read\n%q\nwant\n%q", row.name, verdict.shardGaps, row.gaps)
		}
		if !slices.Equal(verdict.failedTests, row.failedTests) {
			test.Errorf("%s: the failing tests read %v, want %v", row.name, verdict.failedTests, row.failedTests)
		}
		refusal := verdict.shardRefusal()
		switch {
		case len(row.failedTests) > 0:
			if refusal != "" || !slices.Equal(verdict.packagesFailed, []string{"m/checker"}) ||
				!strings.Contains(verdict.failureReport(), "m/checker TestB") {
				test.Errorf("%s: a red shard read as refusal %q, packages %v failed, report:\n%s", row.name, refusal,
					verdict.packagesFailed, verdict.failureReport())
			}
		case len(row.gaps) > 0:
			if want := "Sun2's shards didn't run each listed test exactly once, so its verdict is none: " +
				strings.Join(row.gaps, "; "); refusal != want {
				test.Errorf("%s: the refusal reads\n%s\nwant\n%s", row.name, refusal, want)
			}
		default:
			if refusal != "" || verdict.packagesPassed != 2 ||
				!slices.Equal(verdict.packagesReported, []string{"m/plain", "m/checker"}) ||
				verdict.packageSeconds["m/checker"] != 9 {
				test.Errorf("%s: read as refusal %q, %d passed, %v reported, %gs", row.name, refusal,
					verdict.packagesPassed, verdict.packagesReported, verdict.packageSeconds["m/checker"])
			}
			if want := "slowest test binaries m/checker 9.0s (slowest of 2 shards), m/plain 2.0s"; verdict.
				timingReport() != want {
				test.Errorf("%s: the timing reads %q, want %q", row.name, verdict.timingReport(), want)
			}
		}
	}
	// Nothing sharded, nothing to cover: a plain run reads as it always has.
	if plain := readTestJson(
		[]byte(boxMarker + "0123 on darwin/arm64\n" + green[0] + "\n"),
	); len(plain.shardGaps) > 0 ||
		plain.packagesPassed != 1 {
		test.Errorf("a plain run read as %d passed, gaps %v", plain.packagesPassed, plain.shardGaps)
	}
}
