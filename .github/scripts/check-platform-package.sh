#!/bin/bash
# Proves a staged platform package checks, fixes and formats a TypeScript project the way a user runs it.
#
#     check-platform-package.sh <platform>
#     check-platform-package.sh win32-x64
#
# Written for Windows, the platform nobody had run (#0kfa508), and taking the platform as an argument so
# the same script runs on a Mac against darwin-arm64, which is how it was tested before Windows ran it.
# Run from the directory holding ./dist, as release.yml leaves it.
#
# Both packages are packed and installed with npm into a project of their own, so the run goes through
# what a user's install gives them: npm's shim in node_modules/.bin, the Node launcher, and the platform
# binary it finds. Every case runs in a fresh copy of the project, so no case passes on what an earlier
# one wrote. The full `go test` suite is not run here: many of its tests use /bin/sh, symlinks and
# `/`-absolute fixtures, and that is stated rather than hidden (#0kfa508).
set -euo pipefail

platform=$1
gate=$(mktemp -d)
logs="$gate/logs"
mkdir -p "$logs" "$gate/packs"

npm pack --silent --pack-destination "$gate/packs" "./dist/cohere" > /dev/null
npm pack --silent --pack-destination "$gate/packs" "./dist/cohere-$platform" > /dev/null

# The project every case starts from: a clean file, the house tier and a config that turns one rule on.
template="$gate/template"
mkdir -p "$template"
printf '{ "compilerOptions": { "strict": true, "noEmit": true, "target": "ES2022", "module": "esnext", "moduleResolution": "bundler" }, "include": ["**/*.ts"], "exclude": ["node_modules", "**/dist"] }\n' > "$template/tsconfig.json"
printf '{ "format": {} }\n' > "$template/NexusCohereSettings.json"
printf '{ "extends": "./NexusCohereSettings.json", "rules": { "no-debugger": "error" } }\n' > "$template/CohereSettings.json"
printf 'node_modules\n.cache/\ndist\n' > "$template/.gitignore"
printf 'export const clean = 1;\n' > "$template/Clean.ts"
printf '{ "name": "gate", "private": true }\n' > "$template/package.json"
(cd "$template" && npm install --silent --no-audit --no-fund "$gate"/packs/*.tgz)

failures=0
fail() {
    echo "FAIL $1"
    failures=$((failures + 1))
}

# project <name> copies the template and prints its path.
project() {
    cp -R "$template" "$gate/$1"
    echo "$gate/$1"
}

# run <directory> <log> <arguments...> runs the installed cohere there and prints its exit code.
run() {
    local directory=$1 log=$2
    shift 2
    local status=0
    (cd "$directory" && npx --no-install cohere "$@") > "$logs/$log" 2>&1 || status=$?
    echo "$status"
}

# expect <case> <wanted exit> <got exit> <log> [<text the log must hold>]
expect() {
    local name=$1 wanted=$2 got=$3 log=$4 holds=${5:-}
    if [ "$got" != "$wanted" ]; then
        fail "$name: exit $got, want $wanted"
        cat "$logs/$log"
        return
    fi
    if [ -n "$holds" ] && ! grep -Fq -- "$holds" "$logs/$log"; then
        fail "$name: the output does not hold \"$holds\""
        cat "$logs/$log"
        return
    fi
    echo "ok   $name"
}

# cksum rather than sha256sum, which macOS lacks: the question is only whether the bytes moved.
checksum() {
    cksum < "$1"
}

# Milliseconds from Node, which the install already needs, since macOS's date has no %N.
milliseconds() {
    node -e 'console.log(Date.now())'
}

# The binary runs and names its platform.
directory=$(project version)
status=$(run "$directory" version.log --version)
expect "--version runs and names the platform" 0 "$status" version.log "platform:"

# A clean tree passes, and the same run again replays from the cache.
directory=$(project clean)
status=$(run "$directory" clean.log --no-fix)
expect "a clean tree passes" 0 "$status" clean.log
start=$(milliseconds)
status=$(run "$directory" replay.log --no-fix)
finish=$(milliseconds)
expect "an unchanged tree replays" 0 "$status" replay.log "cached: "
echo "     replay took $((finish - start))ms through npx, the launcher and the binary"

# A finding fails the run and names its file.
directory=$(project findings)
printf 'export function value(): number {\n    debugger;\n    return 1;\n}\n' > "$directory/Debugger.ts"
status=$(run "$directory" findings.log --no-fix)
expect "a finding fails the run" 1 "$status" findings.log "Debugger.ts"

# A type error fails the run and names its file.
directory=$(project types)
printf 'export const broken: number = "text";\n' > "$directory/Broken.ts"
status=$(run "$directory" types.log --no-fix)
expect "a type error fails the run" 1 "$status" types.log "Broken.ts"

# --fix --format settles a tree in one run, and leaves alone what .gitignore names at any depth.
directory=$(project format)
printf 'export const ugly   =   1\n' > "$directory/Ugly.ts"
mkdir -p "$directory/packages/web/dist"
printf 'export const built   =   1\n' > "$directory/packages/web/dist/out.ts"
before=$(checksum "$directory/packages/web/dist/out.ts")
status=$(run "$directory" fix.log --fix --format)
expect "--fix --format runs" 0 "$status" fix.log
status=$(run "$directory" settled.log --no-fix --format)
expect "one --fix --format settles the tree" 0 "$status" settled.log
if [ "$(checksum "$directory/packages/web/dist/out.ts")" != "$before" ]; then
    fail "a nested dist that .gitignore names was written"
else
    echo "ok   a nested dist that .gitignore names is left alone"
fi
if grep -Fq 'ugly   =' "$directory/Ugly.ts"; then
    fail "Ugly.ts was not formatted"
else
    echo "ok   Ugly.ts was formatted"
fi

# A CRLF file is named with the .gitattributes fix.
directory=$(project crlf)
printf 'export const crlf = 1;\r\n' > "$directory/Crlf.ts"
status=$(run "$directory" crlf.log --no-fix --format)
expect "a CRLF file is reported, with the fix" 1 "$status" crlf.log "text=auto eol=lf"

if [ "$failures" -gt 0 ]; then
    echo "$failures checks failed on $platform; logs are in $logs"
    exit 1
fi
echo "every check passed on $platform"
