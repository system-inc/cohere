#!/usr/bin/env bash
# cohere's canary for a Loom job image (#zntvd7v, on Jobs' rule on #et36r38): before an image takes cohere's proofs,
# it runs what cohere's minds run most and heaviest through the door, so a broken image meets this first and not a
# mind's proof. Run it with no arguments from the root of a cohere tree laid at a commit, its TypeScript submodule laid
# with it, after the job's opening; a sealed tree has no .git, and nothing here needs one.
#
# Each step prints one line: PASS, FAIL, or GAP for a need the box can't meet (a tool, a module, a mirror), named,
# never a quiet skip. It exits 0 only when every step passed, 1 when one failed, and 3 when none failed but one met a
# gap, as Loom's lay kit does. The steps:
#   toolchain  the go on the path, as the image's opening sets it
#   modules    every module the engine's tests and cohere-pairs build from, read from the module cache offline
#   engine     the Go engine's heaviest ordinary tests, cold: the root package (it runs git, so a box without git is a
#              gap for it alone), load, and six of their kin; about 4.5 minutes on Pupa, 8 cores (Nov 16)
#   pairs      cohere-pairs built, then one round of a pair of one commit against itself, the smallest honest one,
#              when the box holds its four sources at their pins; a box without them is a gap, named
set -u

status=0
pass() { echo "PASS $1"; }
fail() {
  echo "FAIL $1: $2"
  status=1
}
gap() {
  echo "GAP $1: $2"
  [ "$status" -ne 0 ] || status=3
}
# lastLine is a log's last line that says something, for a step's one line.
lastLine() { grep -v '^[[:space:]]*$' "$1" | tail -n 1; }

if [ ! -f go.mod ] || ! grep -q '^module github.com/system-inc/cohere$' go.mod; then
  echo "FAIL tree: $(pwd) is no cohere tree; run this from its root"
  exit 1
fi
if [ ! -f TypeScript/tsc/go.mod ]; then
  echo "GAP tree: the TypeScript submodule isn't laid in $(pwd)/TypeScript"
  exit 3
fi

if [ -f /opt/loom-proof/environment.sh ]; then
  # shellcheck source=/dev/null
  . /opt/loom-proof/environment.sh
fi
work=$(mktemp -d "${TMPDIR:-/tmp}/cohere-canary.XXXXXX")
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT
# Offline, on a cache of its own, with no stamp from a .git the tree may not have.
export GOCACHE="$work/gocache" GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-buildvcs=false

if ! command -v go > /dev/null; then
  gap toolchain "there is no go on the path"
  exit "$status"
fi
pass "toolchain: $(go version)"

if go list -deps -test ./internal/go_engine/... ./command/cohere-pairs > /dev/null 2> "$work/modules.log"; then
  pass modules
else
  gap modules "the module cache can't build them offline: $(lastLine "$work/modules.log")"
  exit "$status"
fi

packages="./internal/go_engine/load ./internal/go_engine/intermediate_representation"
packages="$packages ./internal/go_engine/check/checker ./internal/go_engine/rules/staticcheck_code"
packages="$packages ./internal/go_engine/build_dependence ./internal/go_engine/check/parity ./internal/go_engine/cache"
if command -v git > /dev/null; then
  packages="./internal/go_engine $packages"
else
  gap engine-root "there is no git on the path, and the root package's tests build repositories with it"
fi
started=$(date +%s)
# shellcheck disable=SC2086 # the packages are words on purpose
if go test -count=1 -timeout 9m $packages > "$work/engine.log" 2>&1; then
  pass "engine: $(echo $packages | wc -w | tr -d ' ') packages in $(($(date +%s) - started))s"
else
  failed=$(grep -E -- '^(--- FAIL|FAIL|panic)' "$work/engine.log" | head -n 5 | tr '\n' ' ')
  fail engine "${failed:-$(lastLine "$work/engine.log")}"
  grep -E -- '--- FAIL|_test.go:[0-9]+:' "$work/engine.log" | head -n 30
fi

if GOWORK=off go build -o "$work/cohere-pairs" ./command/cohere-pairs > "$work/pairs-build.log" 2>&1; then
  pass pairs-build
else
  fail pairs-build "$(lastLine "$work/pairs-build.log")"
  exit "$status"
fi
# The four sources a pair reads, each a repository the box keeps: Loom's mirrors on a keeper box, or the image's on a
# rented one. A variable names one elsewhere.
mirror() {
  for candidate in "$HOME/loom-mirrors/$1.git" "/var/lib/loom/mirrors/$1.git" "/opt/loom-mirrors/$1.git"; do
    if [ -d "$candidate" ]; then
      echo "$candidate"
      return
    fi
  done
}
cohereSource="${COHERE_CANARY_COHERE:-$(mirror cohere)}"
typescriptSource="${COHERE_CANARY_TYPESCRIPT:-$(mirror TypeScript)}"
adamicSource="${COHERE_CANARY_ADAMIC:-$(mirror adamic)}"
loomSource="${COHERE_CANARY_LOOM:-$(mirror loom)}"
[ -n "$loomSource" ] || [ ! -d "$HOME/loom-suite.git" ] || loomSource="$HOME/loom-suite.git"
lacking=""
[ -n "$cohereSource" ] || lacking="$lacking cohere"
[ -n "$typescriptSource" ] || lacking="$lacking TypeScript"
[ -n "$adamicSource" ] || lacking="$lacking adamic"
[ -n "$loomSource" ] || lacking="$lacking loom"
# The commit measured against itself: the job's, which a sealed tree can't say, so a variable names it there.
commit="${COHERE_CANARY_COMMIT:-$(git rev-parse --verify --quiet HEAD 2> /dev/null)}"
if [ -n "$lacking" ]; then
  gap pairs "this box keeps no repository of$lacking, which a pair reads; name each with COHERE_CANARY_<NAME>"
elif [ -z "$commit" ]; then
  gap pairs "the tree has no .git to name its commit; set COHERE_CANARY_COMMIT"
else
  started=$(date +%s)
  if "$work/cohere-pairs" --base "$commit" --branch "$commit" --rounds 1 --out "$work/pairs" \
    --cohere-repository "$cohereSource" --typescript-repository "$typescriptSource" \
    --adamic-repository "$adamicSource" --loom-repository "$loomSource" > "$work/pairs.log" 2>&1; then
    pass "pairs: one round in $(($(date +%s) - started))s"
  else
    code=$?
    # cohere-pairs exits 2 when it refuses before measuring anything, saying why: its preflight names what the box lacks.
    if [ "$code" -eq 2 ]; then
      gap pairs "cohere-pairs refused: $(lastLine "$work/pairs.log")"
    else
      fail pairs "exit $code: $(lastLine "$work/pairs.log")"
    fi
  fi
fi
exit "$status"
