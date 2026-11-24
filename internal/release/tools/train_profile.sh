#!/usr/bin/env bash
# cohere's release profile (#r6sqd2c): the CPU profile command/cohere/default.pgo that a release's Go is compiled
# with, made here from fixed training trees and never by hand. A profile from a tree the benchmarks measure fits the
# binary to the benchmark, so it trains on three others, named with their commits below, and never on cohere's root
# or a tree bench/ measures (ahra, and cohere at its head). #b2t9xmf measured a profile trained this way, B2, against
# one fitted to the root's own bar row: the held-out one took as much off the root, 0.85 user-s of 38.3 (p < 0.001).
#
# Run it from the root of a cohere checkout, on a box with the three trees laid; Adamic's Go never builds or runs on
# Kirk's Mac:
#
#   internal/release/tools/train_profile.sh --deeafbf4 <dir> --adamic <dir> --loom <dir> [--commit <sha>]
#       [--work <new directory>]
#
# Each tree is a checkout at its commit below, and each submodule in it a checkout at the commit its tree records. A
# tree at another commit, with a submodule elsewhere or with a tracked file changed, is refused. The trees are only
# read: each is cloned into the work directory, sharing its objects, and the runs read the clones.
#
# What it does:
#   build   cohere at --commit (this checkout's HEAD unless it says), from a clone, as a release builds it but with
#           -pgo=off, so the profile describes a binary no profile shaped
#   recipe  B2's, on each tree: an unprofiled --no-cache run that fills the system store, then two profiled --no-cache
#           runs over every build (the bar row), then one profiled run over an empty project store (a cold run). The
#           Go engine runs as the front door starts it, `cohere go-engine`, checking without fixing, and each profiled
#           run writes the engine process's own CPU profile with --profile
#   merge   `go tool pprof -proto` over the nine profiles into command/cohere/default.pgo in this checkout, then what
#           it merged: each profile's samples, and the result's sha256, whose prefix `cohere --version` prints
#
# The profile lands in a commit of its own that names the commit trained and quotes what this printed. Anything that
# fails refuses, saying what, and leaves the checkout's profile as it was.
set -u

fail() {
  echo "train_profile: $*" >&2
  exit 1
}

# The training trees. deeafbf4 is cohere itself on Nov 5, the corpus the Go engine's work has been measured on since;
# Adamic and Loom are the commits cohere-pairs measures every pair on (command/cohere-pairs/corpus_pins.go), so a box
# laid for a pair holds them. A pair builds both of its sides with the same profile, so it compares like with like; the
# bar row and bench/ time absolute numbers, which is why their trees are never trained on. Moving a pin is a decision
# of its own, made here and said in the commit that makes it, since it changes every profile made after it.
deeafbf4Commit=deeafbf43e5dfb39c7bacdcdc3f165b571b97276
adamicCommit=217863c10426a4a9e03f74cefb9d789721d45d3f
loomCommit=9510c6baddda7b04f1ac4fa5f927d627f9a01cb3

commit=HEAD
deeafbf4=
adamic=
loom=
work=
while [ $# -gt 0 ]; do
  [ $# -ge 2 ] || fail "$1 needs a value"
  case $1 in
    --commit) commit=$2 ;;
    --deeafbf4) deeafbf4=$2 ;;
    --adamic) adamic=$2 ;;
    --loom) loom=$2 ;;
    --work) work=$2 ;;
    *) fail "$1 is not an argument this takes; the top of $0 says what it takes" ;;
  esac
  shift 2
done
[ -n "$deeafbf4" ] && [ -n "$adamic" ] && [ -n "$loom" ] || fail "it needs --deeafbf4, --adamic and --loom, each a tree"

checkout=$(pwd -P)
[ "$(git rev-parse --show-toplevel 2>/dev/null)" = "$checkout" ] && [ -d command/cohere ] ||
  fail "run it from the root of a cohere checkout, which $checkout is not"
trained=$(git rev-parse --verify --quiet "$commit^{commit}") || fail "this checkout holds no commit $commit"
[ "$trained" != "$deeafbf4Commit" ] || fail "$commit is deeafbf4, a training tree, so the profile would fit the root"

# requireAt refuses unless the directory $1 is a checkout of its own at the commit $2, with no tracked file changed,
# and each submodule in it a checkout at the commit its tree records, all the way down. $3 names it in a refusal.
requireAt() {
  [ -d "$1" ] || fail "$3: $1 is not a directory"
  requireAtTop=$(git -C "$1" rev-parse --show-toplevel 2>/dev/null) || fail "$3: $1 is not a checkout"
  [ "$requireAtTop" = "$(cd "$1" && pwd -P)" ] || fail "$3: $1 is inside the checkout at $requireAtTop"
  [ "$requireAtTop" != "$checkout" ] || fail "$3: $1 is this checkout, the root, which is never trained on"
  requireAtHead=$(git -C "$1" rev-parse HEAD) || fail "$3: $1 has no HEAD"
  [ "$requireAtHead" = "$2" ] || fail "$3: $1 is at $requireAtHead, and the profile trains on $2; lay it there"
  requireAtChanged=$(git -C "$1" status --porcelain --untracked-files=no --ignore-submodules=all) ||
    fail "$3: git can't read the status of $1"
  [ -z "$requireAtChanged" ] || fail "$3: $1 has tracked changes: $(echo "$requireAtChanged" | head -n 3 | tr '\n' ' ')"
  git -C "$1" ls-tree -r HEAD | awk '$2 == "commit" { print $3 "\t" $4 }' | while IFS=$'\t' read -r pin path; do
    requireAt "$1/$path" "$pin" "$3's $path"
  done || exit 1
}
requireAt "$deeafbf4" "$deeafbf4Commit" deeafbf4
requireAt "$adamic" "$adamicCommit" Adamic
requireAt "$loom" "$loomCommit" Loom

if [ -n "$work" ]; then
  mkdir -p "$work" || fail "making $work"
  [ -z "$(ls -A "$work")" ] || fail "$work is not empty, and everything this writes goes in a new directory"
else
  work=$(mktemp -d "${TMPDIR:-/tmp}/cohere-profile.XXXXXX") || fail "making a work directory"
fi
work=$(cd "$work" && pwd -P)
mkdir -p "$work/trees" "$work/runs" "$work/profiles" "$work/system" || fail "laying out $work"

# lay clones the checkout $1 into $2 at the commit $3, sharing its objects, and each submodule from $1's own checkout
# of it, at the commit $3's tree records. A checkout that lacks the commit refuses.
lay() {
  git -C "$1" cat-file -e "$3^{commit}" 2>/dev/null || fail "$1 doesn't hold $3, so $2 can't be laid"
  git clone -q --shared --no-checkout "$1" "$2" && git -C "$2" checkout -q --detach "$3" || fail "laying $2 at $3"
  git -C "$2" ls-tree -r HEAD | awk '$2 == "commit" { print $3 "\t" $4 }' | while IFS=$'\t' read -r pin path; do
    lay "$1/$path" "$2/$path" "$pin"
  done || exit 1
}

echo "on $(hostname), $(sysctl -n hw.ncpu 2>/dev/null || nproc) cores, $(go version), load $(uptime | sed 's/.*: //')"
echo "training cohere $trained in $work"
lay "$checkout" "$work/source" "$trained"
lay "$deeafbf4" "$work/trees/deeafbf4" "$deeafbf4Commit"
lay "$adamic" "$work/trees/adamic" "$adamicCommit"
lay "$loom" "$work/trees/loom" "$loomCommit"

# Every go command and run reads none of the caller's Go or cohere settings that change what is compiled or checked.
clean=(env -u GOFLAGS -u GOGC -u GOMEMLIMIT -u GOARM64 -u GOWORK -u COHERE_SYSTEM_CACHE -u COHERE_PROJECT_ENGINE
  GOTOOLCHAIN=local)

(cd "$work/source" && "${clean[@]}" CGO_ENABLED=0 go build -trimpath -buildvcs=false -pgo=off -ldflags='-s -w' \
  -o "$work/cohere" ./command/cohere) > "$work/build.log" 2>&1 ||
  fail "cohere didn't build at $trained: $(tail -n 3 "$work/build.log" | tr '\n' ' ')"
echo "built cohere at $trained with no profile"

# The engine's own profile, since the work is the engine's: the front door's --profile profiles the front door's
# process alone, and the engine is a child of it. The engine takes --profile itself, or this stops here.
probe=$("${clean[@]}" COHERE_GO_ENGINE_CHILD=1 "$work/cohere" go-engine --contract 3 --root "$work/trees/loom" \
  --version --profile "$work/probe.pprof" 2>&1) ||
  fail "the Go engine at $trained takes no --profile, so this can't write the engine's CPU profile; it said: $(
    echo "$probe" | tail -n 1)"

# The modules each tree's workspace needs, fetched as the box's Go settings allow and verified, before any run; the
# runs then go offline. A tree whose modules are missing stops at type errors and trains on a load that never ran.
for tree in deeafbf4 adamic loom; do
  downloaded=$(cd "$work/trees/$tree" && "${clean[@]}" go mod download 2>&1) ||
    fail "go mod download failed at $tree: $(echo "$downloaded" | tail -n 1)"
  verified=$(cd "$work/trees/$tree" && "${clean[@]}" go mod verify 2>&1) ||
    fail "go mod verify failed at $tree: $(echo "$verified" | tail -n 1)"
done

# run runs the engine once at the tree $1 as the run named $2, writing its CPU profile to $3 unless that is empty, with
# the flags after. A run that exits 2 or more checked nothing, and one with no lint record never reached the lint, so
# either refuses: a profile of that is a profile of a run nobody makes.
run() {
  runTree=$1
  runName=$2
  runProfile=$3
  shift 3
  runWhere=$work/trees/$runTree
  runOut=$work/runs/$runTree-$runName
  (cd "$runWhere" && "${clean[@]}" GOPROXY=off PWD="$runWhere" COHERE_SYSTEM_CACHE="$work/system" \
    COHERE_GO_ENGINE_CHILD=1 "$work/cohere" go-engine --contract 3 --root "$runWhere" --no-fix "$@" \
    ${runProfile:+--profile "$runProfile"} > "$runOut.jsonl" 2> "$runOut.err")
  runCode=$?
  [ "$runCode" -le 1 ] || fail "the $runName run at $runTree exited $runCode: $(tail -n 1 "$runOut.err")"
  runWalked=$(grep -m 1 '"kind":"lint"' "$runOut.jsonl" | grep -o '"filesWalked":[0-9]*' | cut -d : -f 2)
  [ -n "$runWalked" ] || fail "the $runName run at $runTree printed no lint record: $(tail -n 1 "$runOut.err")"
  runFindings=$(grep -c '"kind":"finding"' "$runOut.jsonl")
  if [ -n "$runProfile" ]; then
    [ -s "$runProfile" ] || fail "the $runName run at $runTree wrote no profile at $runProfile"
  fi
  echo "ran $runTree $runName ($*): exit $runCode, $runWalked files walked, $runFindings findings"
}

for tree in deeafbf4 adamic loom; do
  run "$tree" fill "" --no-cache
  run "$tree" bar1 "$work/profiles/$tree-bar1.pprof" --no-cache
  run "$tree" bar2 "$work/profiles/$tree-bar2.pprof" --no-cache
  run "$tree" cold "$work/profiles/$tree-cold.pprof"
done

# samples is a profile's line naming its duration and the CPU it sampled, as pprof prints it.
samples() {
  "${clean[@]}" go tool pprof -top -nodecount=1 "$1" 2>/dev/null | grep -m 1 'Total samples' | sed 's/^ *//'
}
set -- "$work"/profiles/*.pprof
"${clean[@]}" go tool pprof -proto "$@" > "$work/default.pgo" 2> "$work/merge.err" && [ -s "$work/default.pgo" ] ||
  fail "merging the profiles: $(tail -n 1 "$work/merge.err")"
for profile in "$@"; do
  echo "merged $(basename "$profile"): $(samples "$profile")"
done
cp "$work/default.pgo" "$checkout/command/cohere/default.pgo" || fail "writing command/cohere/default.pgo"
if command -v shasum > /dev/null; then
  digest=$(shasum -a 256 < "$checkout/command/cohere/default.pgo" | cut -c 1-64)
else
  digest=$(sha256sum < "$checkout/command/cohere/default.pgo" | cut -c 1-64)
fi
echo "wrote command/cohere/default.pgo from $# profiles: $(samples "$checkout/command/cohere/default.pgo")"
echo "its sha256 is $digest, which a release built with it names as sha256 $(echo "$digest" | cut -c 1-12)"
