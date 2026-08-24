#!/bin/sh
# v runs verify without booting Node.
#
# `s v` reaches verify through `pnpm tsx` evaluating a TypeScript CLI, which costs 0.58s of a 2.67s
# run: measured five warm runs at each layer, raw binary 1.84s, plus dispatcher 2.09s, plus tsx
# 2.67s. Nothing in the resolution needs TypeScript. It reads one environment variable and stats two
# paths, which is what this file does.
#
# This mirrors `resolveVerifyBinary` in Structure.ts deliberately rather than improving on it. Two
# resolvers that disagree about which binary to run is worse than one slow resolver, so when that
# function changes, this changes with it.
#
# The failure behavior is the part that must not drift. An unresolvable binary exits non-zero naming
# everywhere it looked, and never falls through to a bare command name: `s c` printed green over
# zero files for days because a resolver did exactly that, the spawn failed, and an empty file list
# is indistinguishable from a clean tree.

set -eu

installed="${PWD}/node_modules/.bin/verify"
module="${HOME}/Projects/system/verify"
dispatcher="${module}/verify-dispatch"

# An override is fatal when broken rather than a fallback, even with a good install available.
# Someone who sets it has stated which binary they want, and quietly running a different one hands
# them results they would read as their own build's.
if [ -n "${VERIFY_BINARY:-}" ]; then
    if [ -x "${VERIFY_BINARY}" ]; then
        exec "${VERIFY_BINARY}" "$@"
    fi
    echo "verify: VERIFY_BINARY is set to a path that is not executable, so nothing was checked." >&2
    echo "  ${VERIFY_BINARY}" >&2
    exit 1
fi

# The installed package outranks the checkout so a machine with both runs the version it installed.
[ -x "${installed}" ] && exec "${installed}" "$@"
[ -x "${dispatcher}" ] && exec "${dispatcher}" "$@"

echo "verify: no binary found, so nothing was checked." >&2
echo "  Looked, in order:" >&2
echo "    \$VERIFY_BINARY (unset)" >&2
echo "    ${installed}" >&2
echo "    ${dispatcher}" >&2
echo "  Build it with: go build -o ${dispatcher} ./cmd/verify-dispatch" >&2
exit 1
