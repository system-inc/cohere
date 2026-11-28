//go:build unix

package main

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
	"github.com/system-inc/cohere/internal/testing/needs"
	"github.com/system-inc/cohere/internal/testing/own_go_cache"
)

// shardGo stands in for go in the shards' block and in the gate around it. It answers the gate's `go env` and `go mod`
// as a darwin/arm64 toolchain whose modules verify, records each go test with its `GOMAXPROCS` to `$SHARD_CALLS`,
// answers a `-list` with a fixed listing, a benchmark among it, shardBetaListed for a package ending in beta, or with
// a build failure when `$SHARD_LIST_FAILS` is set, and a `-run` with a pass for exactly the top-level tests its regexp
// names, each with a subtest, then its package's pass, whose Elapsed is how many tests it ran; the test `$SHARD_FAILS`
// names fails, and its package with it. A shard waits until `$SHARD_EXPECTED` shards have started, so shards run one
// after another are caught waiting alone. A go test with neither runs the package whole and passes it.
const shardGo = `#!/bin/sh
case "$1" in
env) [ "$2" = GOOS ] && echo darwin; [ "$2" = GOARCH ] && echo arm64; exit 0 ;;
mod) exit 0 ;;
esac
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
  case "$package" in
  *beta) printf '%s\n' TestBetaOne TestBetaTwo TestBetaThree TestBetaFour ;;
  *)
    printf '%s\n' TestOne TestTwo BenchmarkThree TestFour ExampleFive TestSix FuzzSeven TestEight TestNine TestTen \
      TestEleven TestTwelve ;;
  esac
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
// is left out. shardBetaListed is its listing of a package ending in beta.
var (
	shardListed = []string{"TestOne", "TestTwo", "TestFour", "ExampleFive", "TestSix", "FuzzSeven", "TestEight",
		"TestNine", "TestTen", "TestEleven", "TestTwelve"}
	shardBetaListed = []string{"TestBetaOne", "TestBetaTwo", "TestBetaThree", "TestBetaFour"}
)

// shardFixturePackage is the package the shards' block is handed in its test, which shardGo lists as shardListed.
const shardFixturePackage = "example.com/module/checker"

// shardMarkerLine reads a shard's marker as sharedBoxShards prints it: its index, count, package, wall and tests.
var shardMarkerLine = regexp.MustCompile(
	`^` + regexp.QuoteMeta(boxShardMarker) + `([0-9]+) of ([0-9]+) (\S+) ([0-9]+)s (.+)$`,
)

// dealtShards is how sharedBoxShards deals listed into shards: each pin a shard of its own, first, and the rest
// round-robin in the listing's order over the shards the count leaves.
func dealtShards(listed []string, pins []string, count int) [][]string {
	dealt := [][]string{}
	for _, pin := range pins {
		dealt = append(dealt, []string{pin})
	}
	rest := slices.DeleteFunc(slices.Clone(listed), func(name string) bool { return slices.Contains(pins, name) })
	restShards := min(max(count-len(pins), 1), len(rest))
	dealt = append(dealt, make([][]string, restShards)...)
	for index, name := range rest {
		dealt[len(pins)+index%restShards] = append(dealt[len(pins)+index%restShards], name)
	}
	return dealt
}

// shardMarkers is every shard's marker output holds of importPath, read by shardMarkerLine, each shard's tests by its
// index from 0, and fails test on a marker of the package that doesn't read or says no wall.
func shardMarkers(test *testing.T, output string, importPath string) [][]string {
	test.Helper()
	markers := [][]string{}
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(line, boxShardMarker) || !strings.Contains(line, " "+importPath+" ") {
			continue
		}
		read := shardMarkerLine.FindStringSubmatch(line)
		if read == nil {
			test.Errorf("a shard's marker reads %q, want its index, count, package, wall and tests", line)
			continue
		}
		markers = append(markers, strings.Fields(read[5]))
	}
	return markers
}

// The box's shards' block, run with bash as sharedBoxGate spells it, against a stand-in go: it lists the sharded
// package's tests, keeping its tests, examples and fuzz targets and never its benchmark, makes the shards its row asks
// for or, asking 0, a shard per two cores, the cores Marin's budget when she has one, else sysctl's count, else
// nproc's on Linux, at least one and never more than the tests, deals each pin a shard of its own and the rest
// round-robin in the listing's order, runs every shard at once on 2 threads, and prints the listing and each shard's
// marker, its wall in it, before its JSON. Kirk's Mac reads that as one package, passed once, timed by its slowest
// shard, every listed test run exactly once, every shard's wall said. A shard that fails fails the run; a pin naming
// no listed test refuses it in words, naming the pin and the package; a listing that fails runs the package as one go
// test, with no marker, whatever its pins.
func TestTheGatesShardsDealEveryListedTestOnceAcrossTwoThreadProcesses(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate's script is a bash script")
	for _, row := range []struct {
		name string
		// budget is the box's core budget, sysctl what sysctl says of her cores, failing when empty, and nproc nproc's.
		budget, sysctl, nproc string
		// asked is the shards the row asks for, 0 for a shard per two cores, and pins its pins.
		asked     int
		pins      []string
		listFails bool
		fails     string
		// shards is how many shards the block deals, 0 for none, and refuses the pins it refuses.
		shards  int
		refuses string
	}{
		{name: "Marin's budget of 10", budget: "10", sysctl: "64", shards: 5},
		{name: "8 cores", sysctl: "8", shards: 4},
		{name: "Linux's 6 cores", nproc: "6", shards: 3},
		{name: "more cores than tests", sysctl: "64", shards: len(shardListed)},
		{name: "one core", sysctl: "1", shards: 1},
		{name: "a shard fails a test", sysctl: "8", fails: "TestSix", shards: 4},
		{name: "a row asking 3 shards on 64 cores", sysctl: "64", asked: 3, shards: 3},
		{name: "a pin alone in its shard", sysctl: "64", asked: 3, pins: []string{"TestSix"}, shards: 3},
		{name: "two pins on 8 cores", sysctl: "8", pins: []string{"FuzzSeven", "TestOne"}, shards: 4},
		{name: "fewer shards asked than pins", asked: 1, pins: []string{"TestTwo", "TestTen"}, shards: 3},
		{name: "a pin naming no listed test", sysctl: "8", pins: []string{"TestSix", "TestGone"}, refuses: "TestGone"},
		{name: "a listing that fails", budget: "10", listFails: true},
		{name: "a listing that fails, with a pin", budget: "10", pins: []string{"TestGone"}, listFails: true},
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
			bash.Stdin = strings.NewReader(fmt.Sprintf("set -euo pipefail\nsharded=%s\nshardsAsked=%d\n"+
				"shardPins='%s'\ncountFlag=-count=1\ntestsFailed=0\n", shardFixturePackage, row.asked,
				strings.Join(row.pins, " ")) + sharedBoxShards + "echo \"testsFailed=$testsFailed\"\n")
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
			if row.fails != "" || row.refuses != "" {
				wantFailed = "testsFailed=1\n"
			}
			if !strings.HasSuffix(said, wantFailed) {
				test.Errorf("the block ended without %q:\n%s", wantFailed, said)
			}
			ran := strings.Split(strings.TrimSpace(string(readMaybeMissing(test, calls))), "\n")
			if row.refuses != "" {
				want := sharedBoxRefusedMarker + "the shard table pins " + row.refuses + " to " + shardFixturePackage +
					", whose listing holds no such test\n"
				if !strings.HasPrefix(said, want) || strings.Contains(said, boxShardMarker) || len(ran) != 1 {
					test.Errorf("a pin naming no listed test didn't refuse, saying it, before any shard ran:\n%s\n%s",
						said, strings.Join(ran, "\n"))
				}
				if verdict := readTestJson(output); !strings.Contains(verdict.refused, row.refuses+" to "+
					shardFixturePackage) {
					test.Errorf("the verdict's refusal reads %q, want it naming %s and its package", verdict.refused,
						row.refuses)
				}
				return
			}
			if row.shards == 0 {
				if strings.Contains(said, boxShardListingMarker) ||
					strings.Contains(said, boxShardMarker) ||
					strings.Contains(said, sharedBoxRefusedMarker) ||
					len(ran) != 2 ||
					strings.Contains(ran[1], "-run") ||
					!strings.HasSuffix(ran[1], " -json "+shardFixturePackage+" GOMAXPROCS=unset") {
					test.Errorf("a listing that failed didn't run the package as one go test, unsharded:\n%s\n%s",
						said, strings.Join(ran, "\n"))
				}
				return
			}
			dealt := dealtShards(shardListed, row.pins, row.shards)
			if len(dealt) != row.shards {
				test.Fatalf("the test deals %d shards, want %d", len(dealt), row.shards)
			}
			lines := strings.Split(said, "\n")
			if want := boxShardListingMarker + shardFixturePackage + " " + strings.Join(shardListed, " "); !slices.
				Contains(lines, want) {
				test.Errorf("the block printed no listing %q:\n%s", want, said)
			}
			markers := shardMarkers(test, said, shardFixturePackage)
			if !slices.EqualFunc(markers, dealt, slices.Equal) {
				test.Errorf("the shards' markers hold %q, want %q", markers, dealt)
			}
			for index, pin := range row.pins {
				if index >= len(markers) || !slices.Equal(markers[index], []string{pin}) {
					test.Errorf("the pin %s isn't alone in shard %d: %q", pin, index+1, markers)
				}
			}
			// Every test in exactly one shard's marker.
			inShards := map[string]int{}
			for _, names := range markers {
				for _, name := range names {
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
			if !slices.Equal(verdict.packagesReported, []string{shardFixturePackage}) {
				test.Errorf("the package was reported %v, want once", verdict.packagesReported)
			}
			if row.fails != "" {
				if !slices.Equal(verdict.failedTests, []string{shardFixturePackage + " " + row.fails}) ||
					!slices.Equal(verdict.packagesFailed, []string{shardFixturePackage}) ||
					verdict.packagesPassed != 0 {
					test.Errorf("a shard's failure read as tests %v, packages %v failed and %d passed",
						verdict.failedTests, verdict.packagesFailed, verdict.packagesPassed)
				}
				return
			}
			if verdict.packagesPassed != 1 || verdict.failed() {
				test.Errorf("the shards read as %d packages passed, %v failed", verdict.packagesPassed,
					verdict.packagesFailed)
			}
			// The slowest shard is the one that ran the most tests, a second each, and every shard's wall follows.
			slowest := slices.MaxFunc(dealt, func(one []string, other []string) int {
				return cmp.Compare(len(one), len(other))
			})
			want := regexp.MustCompile(`^` + regexp.QuoteMeta(fmt.Sprintf(
				"slowest test binaries %s %d.0s (slowest of %d shards); %s by shard ", shardFixturePackage,
				len(slowest), row.shards, shardFixturePackage)) + `[0-9]+s(, [0-9]+s){` + fmt.Sprint(row.shards-1) +
				`}$`)
			if !want.MatchString(verdict.timingReport()) {
				test.Errorf("the shards' timing reads %q, want it matching %s", verdict.timingReport(), want)
			}
		})
	}
}

// shardTableRow is a row of boxShardTable as its grammar reads: its import path, the shards it asks for and its pins.
type shardTableRow struct {
	importPath string
	shards     int
	pins       []string
}

// readShardTable reads a shard table by its grammar (boxShardTable), failing test on a row that breaks it.
func readShardTable(test *testing.T, table string) []shardTableRow {
	test.Helper()
	rows := []shardTableRow{}
	for line := range strings.SplitSeq(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			test.Errorf("the shard table's row %q holds no import path and shards", line)
			continue
		}
		shards, countError := strconv.Atoi(fields[1])
		if countError != nil || shards < 0 {
			test.Errorf("the shard table's row %q asks for %q shards, want 0 or more", line, fields[1])
		}
		rows = append(rows, shardTableRow{importPath: fields[0], shards: shards, pins: fields[2:]})
	}
	return rows
}

// The shard table holds to its grammar, as the script reads it: a row a package, said once, each asking for 0 or
// more shards, each pin a top-level test's name said once, and nothing a single quote would end the script's line at.
// Its first row is the package the split gate weighs as sharded, by its directory (gateShardedDirectory).
func TestTheShardTableReads(test *testing.T) {
	test.Parallel()
	if strings.ContainsAny(boxShardTable, "'\t\r") || strings.Count(sharedBoxGate, boxShardTableLine) != 1 {
		test.Errorf("the gate's script doesn't set the shard table once, whole, in single quotes")
	}
	rows := readShardTable(test, boxShardTable)
	paths := []string{}
	for _, row := range rows {
		if slices.Contains(paths, row.importPath) {
			test.Errorf("the shard table has %s twice", row.importPath)
		}
		paths = append(paths, row.importPath)
		for index, pin := range row.pins {
			if !regexp.MustCompile(`^(Test|Example|Fuzz)[A-Za-z0-9_]*$`).MatchString(pin) ||
				slices.Contains(row.pins[:index], pin) {
				test.Errorf("the shard table pins %q to %s, want a top-level test's name, once", pin, row.importPath)
			}
		}
	}
	if len(rows) == 0 || !strings.HasSuffix(rows[0].importPath, "/"+gateShardedDirectory) {
		test.Errorf("the shard table's first row is %v, want the package at %s", rows, gateShardedDirectory)
	}
}

// The gate runs each package of its shard table it is handed as a job of its own, beside the module's run, each in
// the shards its row asks for (#d80tydg): here two, handed after a plain package and out of the table's order, the
// first asking 3 shards with a pin, the second 2. Every shard of both runs at once, each package's shards cover its
// listing exactly once, the pin is alone in its shard, each package's output is printed in the table's order with its
// own step, and every shard's wall is said, in its marker and in the timing report. A dealing that drops one listed
// name of one package refuses the verdict, naming that test and that package and no other. And a pin naming no test
// its package lists refuses the gate, in words naming the pin and the package.
func TestTheGateShardsEachPackageOfItsTableAsAJobOfItsOwn(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate is a bash script")
	needs.Tool(test, "sha256sum", "the gate digests the toolchain with it, as the boxes do")
	const alpha = "example.com/module/alpha"
	const beta = "example.com/module/beta"
	const plain = "example.com/module/plain"
	for _, row := range []struct {
		name, table string
		// shards is how many shards run in all, and refuses the pin the gate refuses, empty when it passes.
		shards  int
		refuses string
	}{
		{name: "two packages", table: alpha + " 3 TestSix\n" + beta + " 2", shards: 5},
		{name: "a pin naming no listed test", table: alpha + " 3 TestGone\n" + beta + " 2", shards: 2,
			refuses: "TestGone"},
	} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			goroot := filepath.Join(test.TempDir(), "go")
			writeFixtureFile(test, filepath.Join(goroot, "src", "fmt", "print.go"), "package fmt\n")
			if makeError := os.MkdirAll(filepath.Join(goroot, "bin"), 0o755); makeError != nil {
				test.Fatal(makeError)
			}
			if writeError := executable_testing.WriteFile(filepath.Join(goroot, "bin", "go"), []byte(shardGo),
				0o755); writeError != nil {
				test.Fatal(writeError)
			}
			digest := toolchainDigest(test, goroot)
			tree := test.TempDir()
			writeFixtureFile(test, filepath.Join(tree, "go.mod"), "module example.com/module\n\ngo 1.27\n")
			calls := filepath.Join(test.TempDir(), "calls")
			environment := append(own_go_cache.Environment(withoutModuleCache(os.Environ()), test.TempDir()),
				"TMPDIR="+test.TempDir(), "SHARD_CALLS="+calls, "SHARD_STARTED="+test.TempDir(),
				fmt.Sprintf("SHARD_EXPECTED=%d", row.shards), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
			// The table is the script's sixth argument, read in place of the gate's own line, and the script is read
			// from its input, as a gate box reads it.
			gateScript, replaceError := replacedOnce(sharedBoxGate, boxShardTableLine, `shardTable="$table"`)
			if replaceError != nil {
				test.Fatal(replaceError)
			}
			gate := exec.Command("bash", "-s", "--", goroot, digest, filepath.Join(tree, "no-node"),
				strings.Repeat("0", 64), "", row.table, plain, beta, alpha)
			gate.Stdin = strings.NewReader("set -euo pipefail\n" +
				`goroot="$1"; digest="$2"; node="$3"; nodeDigest="$4"; races="$5"; table="$6"; shift 6` + "\n" +
				gateScript)
			gate.Dir, gate.Env = tree, environment
			output, gateError := gate.CombinedOutput()
			said := string(output)
			if ran := string(readMaybeMissing(test, calls)); strings.Contains(ran, "a shard waited alone") {
				test.Errorf("the shards didn't all run at once:\n%s", ran)
			}
			verdict := readTestJson(output)
			verdict.box = "the box"
			if row.refuses != "" {
				want := "the shard table pins " + row.refuses + " to " + alpha + ", whose listing holds no such test"
				if gateError == nil || verdict.refused != want {
					test.Errorf("the gate ended %v, its refusal %q, want it failed refusing %q:\n%s", gateError,
						verdict.refused, want, said)
				}
				return
			}
			if gateError != nil {
				test.Fatalf("the gate failed (%v):\n%s", gateError, said)
			}
			if len(verdict.shardGaps) > 0 || verdict.packagesPassed != 3 || verdict.failed() ||
				!slices.Equal(verdict.shardedPackages, []string{alpha, beta}) {
				test.Errorf("the gate read as %d passed, %v failed, sharded %v, gaps %q:\n%s", verdict.packagesPassed,
					verdict.packagesFailed, verdict.shardedPackages, verdict.shardGaps, said)
			}
			for _, sharded := range []struct {
				importPath string
				listed     []string
				dealt      [][]string
			}{
				{alpha, shardListed, dealtShards(shardListed, []string{"TestSix"}, 3)},
				{beta, shardBetaListed, dealtShards(shardBetaListed, nil, 2)},
			} {
				if markers := shardMarkers(test, said, sharded.importPath); !slices.EqualFunc(markers, sharded.dealt,
					slices.Equal) {
					test.Errorf("%s's shards' markers hold %q, want %q", sharded.importPath, markers, sharded.dealt)
				}
				run := verdict.shardedRuns[sharded.importPath]
				if run == nil || len(run.walls) != len(sharded.dealt) {
					test.Errorf("%s's shards said their walls as %v, want one a shard", sharded.importPath, run)
				}
				if !strings.Contains(verdict.timingReport(), "; "+sharded.importPath+" by shard ") ||
					!strings.Contains(said, boxStepMarker+"sharded:"+sharded.importPath+" ") {
					test.Errorf("%s's shards' walls or its step went unsaid: %s\n%s", sharded.importPath,
						verdict.timingReport(), said)
				}
			}
			if strings.Index(said, boxShardListingMarker+alpha) > strings.Index(said, boxShardListingMarker+beta) ||
				strings.Index(said, `"Package":"`+plain+`"`) > strings.Index(said, boxShardListingMarker+alpha) {
				test.Errorf("the runs aren't printed the module's, then the table's order:\n%s", said)
			}
			// The Square's mutant: beta's dealing drops TestBetaTwo, from its marker and so from its shard's run.
			dropped := []string{}
			for line := range strings.SplitSeq(said, "\n") {
				if strings.HasPrefix(line, boxShardMarker) && strings.Contains(line, " "+beta+" ") {
					line = strings.Replace(line, " TestBetaTwo", "", 1)
				}
				if strings.Contains(line, `"Test":"TestBetaTwo`) {
					continue
				}
				dropped = append(dropped, line)
			}
			mutant := readTestJson([]byte(strings.Join(dropped, "\n")))
			mutant.box = "the box"
			want := "the box's shards didn't run each listed test exactly once, so its verdict is none: " +
				"TestBetaTwo (" + beta + "), which its listing holds, is in no shard; TestBetaTwo (" + beta +
				") ended 0 times across its shards"
			if refusal := mutant.shardRefusal(); refusal != want {
				test.Errorf("a dealing that dropped a test of %s refused as\n%s\nwant\n%s", beta, refusal, want)
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
