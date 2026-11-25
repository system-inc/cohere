package main

// A shared box is one where every job runs as one account and writes one Go cache: the house Macs the land gate runs
// on, every org there running as loom, and Loom's scratch boxes, where a land-gate job runs. There a candidate's tests
// could leave a test result, a module or a toolchain file that a later landing's gate trusts, and Go's test cache would
// answer "ok" for a test that landing never ran. So a gate there trusts nothing the box kept (#adbkt5t, on Loom
// Security's terms on #1v2shq7):
//
//   - The Go toolchain is the one pinned here, by a digest of its whole tree, read before anything of it runs. A
//     swapped `go`, `compile` or standard-library source differs from the Go project's own archive.
//   - The account's Go settings are never read: no env.sh, no go env file, no toolchain switch, the path set here.
//   - `go mod download` fetches every module the workspace names, other platforms' included, and `go mod verify`
//     passes before any test runs, so the module cache is whole and matches go.sum. A box that never held a module
//     only another platform needs failed the tests that list for that platform, though its own build never asked
//     (TestEveryDirectoryReadsAsGoBuildReadsItOnCoheresBuilds' windows listing on Sun2, Nov 16, #rr818c8).
//   - Every test runs with -count=1 and a Go cache made empty for this job and removed after it.
//
// It is the exception to the house's rule against -count=1 in a gate. That rule holds because Go's test cache is
// keyed honestly, and on a cache any job can write it isn't. Kirk's Mac's own runs keep the cache.

// sharedBoxTrust is what the land line says of a verdict from a shared box beside it: it holds against our own
// mistakes, not against a candidate written to forge one, since every job there runs as one account (#1v2shq7).
const sharedBoxTrust = "scratch box, shared uid"

// sharedBoxRefusedMarker opens the line a shared box's gate prints when it refuses to run the tests at all, for a
// toolchain that isn't the pinned one or a module cache that doesn't match go.sum. Kirk's Mac reads it as a refusal of
// the landing in the box's own words, never as a box that gave no verdict.
const sharedBoxRefusedMarker = "cohere-dev-gate: refused "

// sharedBoxGoroot is where the scratch boxes keep the Go toolchain, ~/adamic-tools/go (#p3rd3vn).
const sharedBoxGoroot = "$HOME/adamic-tools/go"

// sharedBoxToolchainDigest is the digest of go1.27.1's linux/amd64 tree as the Go project ships it: every regular file,
// by its path from the root in byte order, through sha256sum, and that list through sha256sum again. It was read from
// go1.27.1.linux-amd64.tar.gz, whose sha256 63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445 matches
// go.dev's published one, holding 15,639 files and no links (Nov 13). A box on another Go refuses until this moves.
const sharedBoxToolchainDigest = "aa737bb2de5f152e1bdaad9902816611d1cdbe3298238a2207c71e875df8ceeb"

// sharedBoxNode is where Machines lays node on the scratch boxes, root-owned, outside any job's home: under
// /srv/loom-*, since on a scratch box nothing but a release lays anything under /opt, which is in the house stamp
// (#myvcam7). And sharedBoxNodeDigest is the sha256 of its bin/node: node v24.19.0 linux-x64 as nodejs.org ships it
// (node-v24.19.0-linux-x64.tar.xz, sha256 14b342e7..., matching its SHASUMS256.txt, Nov 13). A box holding it puts
// it on the gate's path, so the tests whose oracle is node run on Linux. A box without it skips them by name, which
// the land line names as run nowhere (#myvcam7).
const (
	sharedBoxNode       = "/srv/loom-tools/node-v24.19.0"
	sharedBoxNodeDigest = "bc17c508ffeed0ec622934f9b7fa72f8e78da65350e63c3eceb56fa688aa5e12"
)

// macBoxGoroot is where Machines lays the Go toolchain on the house Macs, root-owned, outside loom's home, and
// macBoxToolchainDigest is sharedBoxGate's digest of it: go1.27.1 darwin/arm64 as go.dev ships it
// (go1.27.1.darwin-arm64.tar.gz, sha256 ee215d57..., matching go.dev's published one), read by this digest on Sun2 and
// on Marin (#rr818c8, Nov 16). A Mac on another Go refuses until this moves.
const (
	macBoxGoroot          = "/opt/loom-tools/go"
	macBoxToolchainDigest = "4256a1b77627be7a90f81396a371991e6232f1d1f0d55df55d4ad1f58f43405b"
)

// macBoxNode is where Machines lays node on the house Macs, beside the toolchain, and macBoxNodeDigest is the sha256 of
// its bin/node: node v24.19.0 darwin-arm64 as nodejs.org ships it (node-v24.19.0-darwin-arm64.tar.gz, sha256
// 3f1cf157..., matching its SHASUMS256.txt), read on Sun2 and on Marin (#rr818c8, Nov 16). macOS's /sbin/sha256sum
// prints the digests Linux's does, so sharedBoxGate reads both the same way.
const (
	macBoxNode       = "/opt/loom-tools/node-v24.19.0"
	macBoxNodeDigest = "27db838bb204ef7c21df2931f5656e4c8fb32e6e947f363a402b49714d32b5b1"
)

// sharedBoxTestTimeout is how long one package's tests may run on a shared box, past go test's default of 10 minutes:
// there every package builds and runs on an empty cache beside every other, and internal/go_language alone took 484s on
// an idle Sun2, so in the whole run it crossed 600s and failed a landing that changed none of it (#er5mbvc, Nov 16).
// The gap's own bound, gateGapBound, still ends a run that hangs.
const sharedBoxTestTimeout = "30m"

// sharedBoxSystemPath is the system's own directories on the tests' path, after the pinned toolchain and node: the
// sbin directories too, since macOS keeps tools a test runs there, sha256sum in /sbin and lsof in /usr/sbin, and
// without them three tests skipped on every gate Mac and ran nowhere (#5r0vrte, Nov 16). Nothing there is the
// account's to change, so it trusts nothing the box kept any more than /usr/bin does.
const sharedBoxSystemPath = "/usr/bin:/bin:/usr/sbin:/sbin"

// sharedBoxToolchainCheck refuses, through the refuse its script defines first, a Go toolchain at goroot that isn't the
// one digest pins: none there, one holding a link, or one whose files digest to another. It runs before anything of the
// toolchain does, in the gate (sharedBoxGate) and in the landing's measurement on Shackler (land_performance.go), which
// builds the binary it times with it.
const sharedBoxToolchainCheck = `[ -x "$goroot/bin/go" ] || refuse "there is no go at $goroot"
link=$(find -P "$goroot" -type l | head -n 1)
[ -z "$link" ] || refuse "the Go toolchain at $goroot holds a link, $link, which its digest can't vouch for"
found=$(cd "$goroot" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1)
[ "$found" = "$digest" ] || refuse "the Go toolchain at $goroot digests to $found, not the pinned $digest"
`

// boxCoreBudgetVariable carries a gate box's core budget (macBoxCoreBudgets) to her script, unset for a box with none.
const boxCoreBudgetVariable = "COHERE_DEV_GATE_CORES"

// boxBudgetMarker opens the line a gate box's run prints when it kept to a core budget, the budget after it.
const boxBudgetMarker = "cohere-dev-gate: budget "

// sharedBoxGate is the gate's run inside a tree checked out at the commit being landed, on a shared box. It takes
// goroot, digest, node, nodeDigest and races (racePatterns' tokens, comma-separated) as variables, and the packages
// to test as its arguments: the module's patterns, or import paths, which a gate Mac is handed. It prints boxMarker
// before the tests, boxRaceMarker before the -race run once the module passes, and sharedBoxRefusedMarker, ending the
// run, when it won't trust the box. When boxShardedPackage is among its arguments, the plain go test runs the rest,
// and that package runs after it in shards (sharedBoxShards), the run failing once both have when either failed. The
// -race run is one go test of the plain tokens, then one of each token's package with its run, every one through the
// same filter so the run after the marker reads as one stream, and a failure of any is the run's, after the rest
// have run.
const sharedBoxGate = `refuse() { echo "` + sharedBoxRefusedMarker + `$*"; exit 1; }
# step says how many seconds the step it names took, from the end of the one before it (#bc8nf0r).
stepStarted=$(date +%s)
step() { stepEnded=$(date +%s); echo "` + boxStepMarker + `$1 $((stepEnded - stepStarted))"; stepStarted=$stepEnded; }
` + sharedBoxToolchainCheck + `nodePath=""
if [ -e "$node/bin/node" ]; then
  nodeFound=$(sha256sum "$node/bin/node" | cut -d' ' -f1)
  [ "$nodeFound" = "$nodeDigest" ] || refuse "the node at $node/bin/node digests to $nodeFound, not the pinned $nodeDigest"
  nodePath="$node/bin:"
fi
unset GOFLAGS GOCACHE GOMODCACHE GOPATH GOWORK GOPROXY GONOSUMDB GONOSUMCHECK GOPRIVATE GOINSECURE GOSUMDB
export PATH="$goroot/bin:$nodePath` + sharedBoxSystemPath + `"
export GOROOT="$goroot" GOTOOLCHAIN=local GOENV=off GOFLAGS="-buildvcs=false -trimpath"
# A box that shares herself with other work gives the gate a budget of cores (macBoxCoreBudgets): two threads a test
# binary, and half the budget's binaries at once, so the gate's Go work holds that many threads. macOS caps no job's
# cores, so it is a budget, kept by Go's own settings, and the run says it held one.
if [ -n "${` + boxCoreBudgetVariable + `:-}" ]; then
  budgetPackages=$(( ` + boxCoreBudgetVariable + ` / 2 ))
  [ "$budgetPackages" -ge 1 ] || refuse "a core budget of ${` + boxCoreBudgetVariable + `} holds no test binary"
  # -p=N as one word: go splits GOFLAGS on spaces, so "-p 5" reached it as -p and a stray 5, and refused every run.
  export GOMAXPROCS=2 GOFLAGS="$GOFLAGS -p=$budgetPackages"
  echo "` + boxBudgetMarker + `${` + boxCoreBudgetVariable + `} cores, go test -p $budgetPackages on 2 threads each"
fi
gocache=$(mktemp -d "${TMPDIR:-/tmp}/cohere-gate-gocache.XXXXXX")
# The shards' outputs and the run's own temporary directory, when the script before this made one, go with the
# cache.
trap 'rm -rf "$gocache"; [ -z "${shards:-}" ] || rm -rf "$shards"; [ -z "${gatetemp:-}" ] || { chmod -R u+w "$gatetemp" 2>/dev/null; rm -rf "$gatetemp"; }' EXIT
export GOCACHE="$gocache"
echo "` + boxMarker + `$(git rev-parse HEAD) on $(go env GOOS)/$(go env GOARCH)"
step toolchain
downloaded=$(go mod download 2>&1) || refuse "go mod download failed: $(echo "$downloaded" | tail -n 1)"
verified=$(go mod verify 2>&1) || refuse "go mod verify failed: $(echo "$verified" | tail -n 1)"
step modules
# boxShardedPackage, when it is among the arguments, runs apart from the rest, in shards (sharedBoxShards).
sharded=""; others=""
for package in "$@"; do
  if [ "$package" = "` + boxShardedPackage + `" ]; then sharded="$package"; else others="$others $package"; fi
done
# A failing go test is remembered rather than ending the script under set -e, so the shards still run after a plain
# run that failed, as one go test of every package would have run them, and the run ends failed once both have.
testsFailed=0
if [ -z "$sharded" ]; then
  go test -count=1 -timeout ` + sharedBoxTestTimeout + ` -json "$@" | grep -vE '"Action":"(start|run|pause|cont)"' ||
    testsFailed=1
elif [ -n "$others" ]; then
  # Unquoted, so each package is its own argument: an import path, which holds no space or glob.
  go test -count=1 -timeout ` + sharedBoxTestTimeout + ` -json $others |
    grep -vE '"Action":"(start|run|pause|cont)"' || testsFailed=1
fi
step tests
if [ -n "$sharded" ]; then
` + sharedBoxShards + `step sharded
fi
[ "$testsFailed" = 0 ] || exit 1
if [ -n "$races" ]; then
  echo "` + boxRaceMarker + `"
  # Split on commas alone, never on a run's characters, and each run quoted, so nothing of it globs.
  IFS=, read -r -a raceTokens <<< "$races"
  plainRaces=""
  for race in "${raceTokens[@]}"; do
    case "$race" in *` + raceRunSeparator + `*) ;; *) plainRaces="$plainRaces $race" ;; esac
  done
  # A failing go test is remembered rather than ending the script under set -e, so every token still runs.
  raceFailed=0
  # Unquoted, so each plain token is its own argument: a directory of the module, which holds no space or glob.
  if [ -n "$plainRaces" ]; then
    go test -count=1 -timeout ` + sharedBoxTestTimeout + ` -json -race $plainRaces |
      grep -vE '"Action":"(start|run|pause|cont)"' || raceFailed=1
  fi
  step race
  for race in "${raceTokens[@]}"; do
    case "$race" in
    *` + raceRunSeparator + `*)
      go test -count=1 -timeout ` + sharedBoxTestTimeout + ` -json -race -run "${race#*` + raceRunSeparator + `}" \
        "${race%%` + raceRunSeparator + `*}" | grep -vE '"Action":"(start|run|pause|cont)"' || raceFailed=1 ;;
    esac
  done
  step race-runs
  [ "$raceFailed" = 0 ] || exit 1
fi
`
