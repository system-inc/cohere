package main

// A shared box is one where every job runs as one account and writes one Go cache, Home and Loom's scratch boxes
// until #3e0jmys. There a candidate's tests could leave a test result, a module or a toolchain file that a later
// landing's gate trusts, and Go's test cache would answer "ok" for a test that landing never ran. So a gate there
// trusts nothing the box kept (#adbkt5t, on Loom Security's terms on #1v2shq7):
//
//   - The Go toolchain is the one pinned here, by a digest of its whole tree, read before anything of it runs. A
//     swapped `go`, `compile` or standard-library source differs from the Go project's own archive.
//   - The account's Go settings are never read: no env.sh, no go env file, no toolchain switch, the path set here.
//   - `go mod verify` passes before any test runs, so the module cache matches go.sum.
//   - Every test runs with -count=1 and a Go cache made empty for this job and removed after it.
//
// It is the exception to the house's rule against -count=1 in a gate. That rule holds because Go's test cache is
// keyed honestly, and on a cache any job can write it isn't. The Mac's own runs keep the cache.

// sharedBoxTrust is what the land line says of a verdict from a shared box beside it: it holds against our own
// mistakes, not against a candidate written to forge one, since every job there runs as one account (#1v2shq7).
const sharedBoxTrust = "scratch box, shared uid"

// sharedBoxRefusedMarker opens the line a shared box's gate prints when it refuses to run the tests at all, for a
// toolchain that isn't the pinned one or a module cache that doesn't match go.sum. The Mac reads it as a refusal of
// the landing, never as a box that gave no verdict, which would run the tests on the Mac instead.
const sharedBoxRefusedMarker = "cohere-dev-home: refused "

// sharedBoxGoroot is where Home and the scratch boxes keep the Go toolchain, ~/adamic-tools/go (#p3rd3vn).
const sharedBoxGoroot = "$HOME/adamic-tools/go"

// sharedBoxToolchainDigest is the digest of go1.27.1's linux/amd64 tree as the Go project ships it: every regular file,
// by its path from the root in byte order, through sha256sum, and that list through sha256sum again. It was read from
// go1.27.1.linux-amd64.tar.gz, whose sha256 63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445 matches
// go.dev's published one, holding 15,639 files and no links (Nov 13). A box on another Go refuses until this moves.
const sharedBoxToolchainDigest = "aa737bb2de5f152e1bdaad9902816611d1cdbe3298238a2207c71e875df8ceeb"

// sharedBoxGate is the gate's run inside a tree checked out at the commit being landed, on a shared box. It takes
// goroot, digest and races (the raceGated packages, comma-separated) as variables, and the module's test patterns as
// its arguments. It prints homeMarker before the tests, homeRaceMarker before the -race run once the module passes,
// and sharedBoxRefusedMarker, ending the run, when it won't trust the box.
const sharedBoxGate = `refuse() { echo "` + sharedBoxRefusedMarker + `$*"; exit 1; }
[ -x "$goroot/bin/go" ] || refuse "there is no go at $goroot"
link=$(find -P "$goroot" -type l | head -n 1)
[ -z "$link" ] || refuse "the Go toolchain at $goroot holds a link, $link, which its digest can't vouch for"
found=$(cd "$goroot" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1)
[ "$found" = "$digest" ] || refuse "the Go toolchain at $goroot digests to $found, not the pinned $digest"
unset GOFLAGS GOCACHE GOMODCACHE GOPATH GOWORK GOPROXY GONOSUMDB GONOSUMCHECK GOPRIVATE GOINSECURE GOSUMDB
export PATH="$goroot/bin:/usr/bin:/bin" GOROOT="$goroot" GOTOOLCHAIN=local GOENV=off GOFLAGS="-buildvcs=false -trimpath"
gocache=$(mktemp -d "${TMPDIR:-/tmp}/cohere-gate-gocache.XXXXXX")
trap 'rm -rf "$gocache"' EXIT
export GOCACHE="$gocache"
echo "` + homeMarker + `$(git rev-parse HEAD) on $(go env GOOS)/$(go env GOARCH)"
verified=$(go mod verify 2>&1) || refuse "go mod verify failed: $(echo "$verified" | tail -n 1)"
go test -count=1 -json "$@" | grep -vE '"Action":"(start|run|pause|cont)"'
if [ -n "$races" ]; then
  echo "` + homeRaceMarker + `"
  go test -count=1 -json -race $(echo "$races" | tr , ' ') | grep -vE '"Action":"(start|run|pause|cont)"'
fi
`
