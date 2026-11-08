#!/usr/bin/env bash
# Runs house_names.sh's passes over another repository one at a time and prints each pass's change as a patch, for a
# box that runs the passes where the repository's own rules keep its Go off a person's machine, as adamic's do.
#
# Usage, from the root of a cohere worktree:
#   tools/go_codemods/house_names_patches.sh <root> [--only <n>]
#
# <root> is a scratch checkout of the other repository, with a go.work, that this script may commit in: after each
# pass its changes are committed there, under a placeholder author, so the next pass's change is its own, and the
# commit's patch prints gzipped and then base64 between a `BEGIN pass-<n>.patch.gz` line and an `END pass-<n>.patch.gz`
# line, to decode (`base64 -d | gunzip`) and apply in order onto a branch of that repository, one commit per pass. A
# pass over a large tree writes tens of megabytes of patch, and gzip keeps it a fraction of that, small enough for a
# job relay that lets bytes go when a job writes faster than it reads. Each pass's lines that say whether it is sound (its
# accepted and rejected counts, "zero package errors", "0 mismatches") print as it runs. A pass that changes nothing
# says so. A pass that fails stops the run, and every pass before it stays printed. --only runs the nth pass alone, as
# house_names.sh's --only does, for a branch that already holds the passes before it.
#
# Only the root's own files are committed: every submodule of the root is skipped by every pass and left out of what
# is staged. It is bash, written for bash 3.2 and up, like house_names.sh.

if [[ ! -f go.work || ! -f tools/go_codemods/house_names.sh ]]; then
	echo "house_names_patches: run this from the root of a cohere worktree" >&2
	exit 2
fi
if (( $# != 1 && $# != 3 )) || { (( $# == 3 )) && [[ $2 != --only ]]; }; then
	echo "usage: house_names_patches.sh <root> [--only <n>]" >&2
	exit 2
fi
root=$(cd "$1" 2>/dev/null && pwd -P) || { echo "house_names_patches: no directory at $1" >&2; exit 2; }
cohere=$(pwd -P)
echo "house_names_patches: root $(git -C "$root" rev-parse HEAD), cohere $(git -C "$cohere" rev-parse HEAD)"
echo "house_names_patches: go $(go env GOVERSION) $(go env GOOS)/$(go env GOARCH), bash $BASH_VERSION"

# The number of passes, asked of the script that runs them, so the two never disagree.
count=$(bash tools/go_codemods/house_names.sh --count)
if [[ ! $count =~ ^[0-9]+$ ]]; then
	echo "house_names_patches: could not read the number of passes from house_names.sh" >&2
	exit 2
fi
first=1
last=$count
if (( $# == 3 )); then
	if [[ ! $3 =~ ^[0-9]+$ ]] || (( $3 < 1 || $3 > count )); then
		echo "house_names_patches: --only takes a pass number from 1 to $count, not $3" >&2
		exit 2
	fi
	first=$3
	last=$3
fi

# Pathspecs leaving out each submodule, so a pass's commit holds the root's own files only.
excludes=()
if [[ -f $root/.gitmodules ]]; then
	while read -r nested; do
		[[ -n $nested ]] && excludes+=(":(exclude)$nested")
	done < <(git -C "$root" config --file .gitmodules --get-regexp '\.path$' | awk '{print $2}')
fi

for (( number = first; number <= last; number++ )); do
	echo "=== PASS $number"
	bash tools/go_codemods/house_names.sh --root "$root" --only "$number"
	exitCode=$?
	echo "=== PASS $number exit $exitCode"
	if (( exitCode != 0 )); then
		echo "house_names_patches: stopping at pass $number; every pass before it is committed and printed above"
		exit "$exitCode"
	fi
	git -C "$root" add -A -- . "${excludes[@]}"
	if git -C "$root" diff --cached --quiet; then
		echo "=== PASS $number wrote nothing"
		continue
	fi
	git -C "$root" -c user.name=house_names -c user.email=house_names@invalid commit -q \
		-m "house_names pass $number"
	echo "=== PASS $number files $(git -C "$root" show --name-only --format= HEAD | wc -l | tr -d ' ')"
	echo "BEGIN pass-$number.patch.gz"
	git -C "$root" show --binary --format= HEAD | gzip -9 | base64
	echo "END pass-$number.patch.gz"
done
echo "house_names_patches: done"
