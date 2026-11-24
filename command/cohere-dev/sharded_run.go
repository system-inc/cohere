package main

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// The gate runs internal/go_engine's tests as several 2-thread processes rather than one (#78xwy8k). Its test process
// starts about 6,450 children, and on darwin each fork write-protects the whole heap, so the faults grow with the heap
// and with the threads faulting together: one process on 8 threads took 426s wall and 2,143s of system time, and four
// processes of 2 threads ran the same 431 top-level tests in 127s wall and about 440s of system time (#2htryg8). In
// the last split gate Sun2's half was go_engine alone, 707s.
//
// The box lists the package's top-level tests, deals them round-robin into a shard per two of its cores, runs every
// shard at once and prints each one's JSON after the marker naming its tests (sharedBoxShards). Kirk's Mac reads the
// shards as one package result, and proves, on the Square's terms, that the shards covered the listing exactly once:
// every listed test in one shard's marker and ending once across them all, none missing, none in two. A gap refuses
// the landing, naming each test, after any failing test, which is said first. The verdict reads the markers and
// finishes the package's one result in box_verdict.go, with the rest of a run's reading.

// boxShardedPackage is the one package the gate runs in shards, by its import path. The script shards it only when
// this path is among its arguments, which a split half's and a Mac's whole gate are; the land-gate job's patterns,
// `./...`, never name it, so that road runs it unsharded.
const boxShardedPackage = "github.com/system-inc/cohere/internal/go_engine"

// boxShardListingMarker opens the line a box's run prints when it shards a package: the package's import path, then
// every top-level test its listing held, in the listing's order, each separated by a space.
const boxShardListingMarker = "cohere-dev-gate: listed "

// boxShardMarker opens the line a box's run prints before each shard's JSON: `<index> of <count> <import path>`, the
// index from 1, then the shard's tests, each separated by a space.
const boxShardMarker = "cohere-dev-gate: shard "

// sharedBoxShards is sharedBoxGate's run of the sharded package, given its import path as sharded and testsFailed,
// which it sets to 1 when a shard fails. A listing that fails is a package that doesn't build, and one listing no test
// has nothing to deal, so either runs as one go test, which reports it as the gate always has. Otherwise there is a
// shard per two cores, the cores being the box's core budget when she has one, at least one shard and never more than
// the tests listed, and each runs on 2 threads. The shards' outputs are printed after all have ended, each whole after
// its marker, so Kirk's Mac reads every event under the shard that ran it.
const sharedBoxShards = `listing=$(go test -count=1 -list '.*' "$sharded" 2>&1) || listing=""
# go test runs tests, examples and fuzz targets' seeds; a benchmark runs only under -bench, so none is dealt.
listed=$(printf '%s\n' "$listing" | grep -E '^(Test|Example|Fuzz)[^[:space:]]*$' || true)
if [ -z "$listed" ]; then
  go test -count=1 -timeout ` + sharedBoxTestTimeout + ` -json "$sharded" |
    grep -vE '"Action":"(start|run|pause|cont)"' || testsFailed=1
else
  cores="${` + boxCoreBudgetVariable + `:-}"
  [ -n "$cores" ] || cores=$(sysctl -n hw.ncpu 2>/dev/null || nproc)
  shardCount=$((cores / 2))
  listedCount=$(($(printf '%s\n' "$listed" | wc -l)))
  [ "$shardCount" -le "$listedCount" ] || shardCount=$listedCount
  [ "$shardCount" -ge 1 ] || shardCount=1
  echo "` + boxShardListingMarker + `$sharded $(printf '%s' "$listed" | tr '\n' ' ')"
  # Dealt round-robin in the listing's order, read a line at a time so no name is ever globbed.
  shardNames=()
  index=0
  while read -r name; do
    shard=$((index % shardCount))
    shardNames[shard]="${shardNames[shard]:-}${shardNames[shard]:+ }$name"
    index=$((index + 1))
  done <<< "$listed"
  shards=$(mktemp -d "${TMPDIR:-/tmp}/cohere-gate-shards.XXXXXX")
  shardProcesses=""
  shard=0
  while [ "$shard" -lt "$shardCount" ]; do
    GOMAXPROCS=2 go test -count=1 -timeout ` + sharedBoxTestTimeout + ` -json -run "^(${shardNames[shard]// /|})$" \
      "$sharded" > "$shards/$shard" 2>&1 &
    shardProcesses="$shardProcesses $!"
    shard=$((shard + 1))
  done
  # A failing shard is remembered rather than ending the script under set -e, so every shard is waited for and read.
  shardsFailed=0
  for shardProcess in $shardProcesses; do wait "$shardProcess" || shardsFailed=1; done
  shard=0
  while [ "$shard" -lt "$shardCount" ]; do
    echo "` + boxShardMarker + `$((shard + 1)) of $shardCount $sharded ${shardNames[shard]}"
    # A shard that printed nothing the filter keeps ended no test, which Kirk's Mac names as a gap.
    grep -vE '"Action":"(start|run|pause|cont)"' "$shards/$shard" || true
    shard=$((shard + 1))
  done
  [ "$shardsFailed" = 0 ] || testsFailed=1
fi
`

// shardedRun is what a box's run said of a package it ran in shards: the tests its listing held, each shard's tests
// by its index from 1, and what the shards' JSON ended.
type shardedRun struct {
	listed []string
	// count is how many shards the shards' markers said there were, and shards each marker's tests by its index, a
	// marker said twice holding both its lists.
	count  int
	shards map[int][][]string
	// packageEnds is how many times each shard's test binary ended, its package's pass, fail or skip, by its index,
	// failed whether any shard's failed, and slowest the slowest shard's Elapsed.
	packageEnds map[int]int
	failed      bool
	slowest     float64
	// testEnds is each top-level test's pass, fail or skip, by the index of the shard whose JSON said it, in order.
	testEnds map[string][]int
}

// readShardEvent reads a JSON event of a sharded package, said under the shard at index, into run: a package's pass,
// fail or skip as its shard's test binary ending, and a top-level test's as that test ending. It reports whether the
// event was a package's end, which the package's one result is read from in its place (finishShardedRuns).
func (run *shardedRun) readShardEvent(event testEvent, index int) bool {
	if event.Action != "pass" && event.Action != "fail" && event.Action != "skip" {
		return false
	}
	if event.Test != "" {
		// A subtest stays with its parent, which ended in the same shard.
		if !strings.Contains(event.Test, "/") {
			run.testEnds[event.Test] = append(run.testEnds[event.Test], index)
		}
		return false
	}
	run.packageEnds[index]++
	run.failed = run.failed || event.Action == "fail"
	run.slowest = max(run.slowest, event.Elapsed)
	return true
}

// coverageGaps is every way run's shards fail to cover its listing exactly once, each naming its test or its shard:
// a listed test in no shard's marker or in two, a shard's test the listing doesn't hold, a shard said twice or never,
// a shard whose test binary didn't end exactly once, and a listed test that didn't end exactly once across every
// shard's JSON, or ended in a shard whose marker doesn't hold it.
func (run *shardedRun) coverageGaps(importPath string) []string {
	gaps := []string{}
	// dealt is the shards each test's markers put it in, in index order, a shard past the count among them.
	dealt := map[string][]int{}
	for _, index := range slices.Sorted(maps.Keys(run.shards)) {
		if index < 1 || index > run.count {
			gaps = append(gaps, fmt.Sprintf("shard %d of %s is past its %d shards", index, importPath, run.count))
		}
		for _, names := range run.shards[index] {
			for _, name := range names {
				dealt[name] = append(dealt[name], index)
			}
		}
	}
	for index := 1; index <= run.count; index++ {
		switch len(run.shards[index]) {
		case 0:
			gaps = append(gaps, fmt.Sprintf("shard %d of %d of %s never said its tests", index, run.count, importPath))
		case 1:
		default:
			gaps = append(gaps, fmt.Sprintf("shard %d of %d of %s was said %d times", index, run.count, importPath,
				len(run.shards[index])))
		}
		if ends := run.packageEnds[index]; ends != 1 {
			gaps = append(gaps, fmt.Sprintf("shard %d of %d of %s ended its test binary %d times", index, run.count,
				importPath, ends))
		}
	}
	for _, name := range run.listed {
		in := dealt[name]
		switch len(in) {
		case 0:
			gaps = append(gaps, fmt.Sprintf("%s (%s), which its listing holds, is in no shard", name, importPath))
		case 1:
		default:
			gaps = append(gaps, fmt.Sprintf("%s (%s) is in shards %s", name, importPath, shardIndexes(in)))
		}
		ends := run.testEnds[name]
		switch {
		case len(ends) != 1:
			gaps = append(gaps, fmt.Sprintf("%s (%s) ended %d times across its shards%s", name, importPath,
				len(ends), shardsSaid(ends)))
		case len(in) == 1 && ends[0] != in[0]:
			gaps = append(gaps, fmt.Sprintf("%s (%s) ended in shard %d, though shard %d holds it", name,
				importPath, ends[0], in[0]))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(dealt)) {
		if !slices.Contains(run.listed, name) {
			gaps = append(gaps, fmt.Sprintf("%s (%s) is in shard %s, which its listing doesn't hold", name,
				importPath, shardIndexes(dealt[name])))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(run.testEnds)) {
		if !slices.Contains(run.listed, name) {
			gaps = append(gaps, fmt.Sprintf("%s (%s) ended, which its listing doesn't hold", name, importPath))
		}
	}
	return gaps
}

// shardIndexes is shard indexes as a sentence says them: `2`, `1 and 3`, `1, 2 and 3`.
func shardIndexes(indexes []int) string {
	said := []string{}
	for _, index := range indexes {
		said = append(said, strconv.Itoa(index))
	}
	if len(said) < 2 {
		return strings.Join(said, "")
	}
	return strings.Join(said[:len(said)-1], ", ") + " and " + said[len(said)-1]
}

// shardsSaid is where a test's ends were said, `, in shards 1 and 3`, empty when it never ended.
func shardsSaid(indexes []int) string {
	if len(indexes) == 0 {
		return ""
	}
	return ", in shards " + shardIndexes(indexes)
}
