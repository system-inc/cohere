#!/bin/zsh
# Brings a cohere worktree's Go up to the house names main already holds, by running each codemod pass, in the
# order the passes landed, over the whole tree. Every pass is deterministic: it renames only through the rename
# engine (a rename is accepted only if every identifier still resolves to the same object), reloads the tree, and
# fails unless it type-checks with every identifier matching. Running it on a tree that already has the names
# changes nothing.
#
# Usage, from the root of a cohere worktree:
#   tools/go_codemods/house_names.sh [--root <path>] [--only <n>] [--rename-library-exports <package,package>]
#
# With no --root the passes run over the cohere worktree itself. With --root they are built from the cohere worktree
# the script runs in and run over the Go workspace at that path, another repository with a go.work of its own: every
# nested repository it holds (a git submodule, such as a vendored cohere) is skipped by every pass, so no pass edits
# a file another repository owns, and git there says which files the passes changed. --only runs the nth pass alone,
# counting from 1, and wraps what it changed, so a caller can commit each pass apart. The last pass renames the
# exports of library packages only where the packages are named: cohere's own two by default over cohere, none over
# another repository unless --rename-library-exports names them, since an importable package's consumers outside the
# workspace cannot be renamed with it.
#
# Each pass is built from tools/go_codemods in the tree it runs over, so the version of a pass that runs is the one
# that tree holds: a pass is landed exactly when its source is on main, and no list of approved builds is needed.
# It writes files in place and commits nothing. The tree must type-check before it starts (a pass refuses to rename
# over package errors), so run it before a merge, not in the middle of one.
#
# A branch brings main in like this, so it never meets old and new names mixed in one function:
#   1. commit your work
#   2. run this script, then `go run ./command/cohere-dev test <your packages>`, then commit the renames
#   3. git merge main
#      Both sides now spell the renamed names the same way. Sort each conflicting hunk by what each side changed
#      since the merge base: keep the side with the substance, and let step 4 re-derive the names and the wrap on
#      it. A side that changed only by these passes or the wrap holds no substance. Where both sides hold
#      substance, resolve it by hand.
#   4. run this script again (it must print "zero package errors" and "0 mismatches" for every pass), then test,
#      then commit the merge.

# The passes, in landing order. A pass is its command's folder under tools/go_codemods/command, or that and its
# flags separated by spaces and quoted as one word, so a flag's value may hold a comma.
passes=(test_names error_names context_names receiver_names word_names loop_names 'initialisms -tag-encoded-fields'
	'word_names -global -tag-encoded-fields -rename-owned-spellings')

if [[ ! -f go.work || ! -d tools/go_codemods/command ]]; then
	print -u2 "house_names: run this from the root of a cohere worktree (no go.work or tools/go_codemods here)"
	exit 2
fi
cohere=$PWD
root=$cohere
only=0
libraryExports=static_single_assignment,mutation_aliasing
libraryExportsGiven=0
usage="usage: house_names.sh [--root <path>] [--only <n>] [--rename-library-exports <package,package>]"
while (( $# > 0 )); do
	case $1 in
		--root) root=${2:A}; shift 2 ;;
		--only) only=$2; shift 2 ;;
		--rename-library-exports) libraryExports=$2; libraryExportsGiven=1; shift 2 ;;
		*) print -u2 "house_names: unknown argument $1; $usage"; exit 2 ;;
	esac
done
if [[ ! -f $root/go.work ]]; then
	print -u2 "house_names: $root holds no go.work, which every pass reads its packages from"
	exit 2
fi
if [[ $only != <-> ]] || (( only > ${#passes} )); then
	print -u2 "house_names: --only takes a pass number from 1 to ${#passes}, not $only"
	exit 2
fi
# cohere's own library list names nothing in another repository, so over one only a list given for it applies.
if [[ $root != $cohere && $libraryExportsGiven == 0 ]]; then
	libraryExports=
fi
if [[ -n $libraryExports ]]; then
	passes[-1]="${passes[-1]} -rename-library-exports $libraryExports"
fi
# A nested repository's files are another repository's: every pass skips each submodule of the root, by its path.
skips=()
if [[ -f $root/.gitmodules ]]; then
	for nested in ${(f)"$(git -C $root config --file .gitmodules --get-regexp '\.path$' | awk '{print $2}')"}; do
		skips+=(-skip $nested)
	done
fi
selected=($passes)
if (( only > 0 )); then
	selected=("${passes[$only]}")
fi

# The builds live in a directory of their own, named so loadRunning can know a pass by its executable's path.
binaries=$(mktemp -d)/go_codemods_bin
mkdir -p $binaries
trap 'rm -f $binaries/*(N); rmdir $binaries ${binaries:h} 2>/dev/null' EXIT
# A pass needs no version stamp, and stamping fails in a worktree whose TypeScript is a link or in a cohere checked out
# as another repository's submodule, so the builds leave it out.
for command in ${(u)${passes%% *}} line_wrap; do
	if ! go run ./command/cohere-dev build -buildvcs=false -o $binaries/$command ./tools/go_codemods/command/$command; then
		print -u2 "house_names: building $command failed; stopping"
		exit 3
	fi
done

# loadRunning reports whether a whole-workspace load is running: a pass, or a cohere binary running its go-engine
# child. It matches by the executable a process runs (ps's comm), then for cohere by its first argument, never by
# text anywhere on a command line, since a session's prompt can carry these words and would hold every run waiting.
loadRunning() {
	ps -axo comm=,args= | awk '
		$1 ~ /\/go_codemods_bin\// { found = 1 }
		$1 ~ /(^|\/)cohere$/ && $3 == "go-engine" { found = 1 }
		END { exit found ? 0 : 1 }
	'
}

for specification in $selected; do
	pass=${specification%% *}
	flags=(${(s: :)${specification#$pass}})
	# One whole-workspace load at a time on this machine: the loads are several gigabytes each.
	while loadRunning; do
		print -u2 "house_names: waiting for another load to finish before $pass"
		sleep 30
	done
	print "house_names: $pass $flags"
	go run ./command/cohere-dev exec -- $binaries/$pass -root $root $flags $skips 2>&1 \
		| grep -E '^(proposals|candidates|accepted|rejected|wrote|reloaded|re-verification)|package errors|mismatch'
	exitCode=${pipestatus[1]}
	if (( exitCode != 0 )); then
		print -u2 "house_names: $pass exited $exitCode; stopping. Read its output by running it alone:"
		print -u2 "  go run ./command/cohere-dev exec -- <a build of tools/go_codemods/command/$pass> -root $root $flags $skips"
		exit $exitCode
	fi
done

# A renamed name is often longer, so the files the passes changed are rewrapped to 120 columns: each file is proven
# before it is written (the same syntax tree, the same comment words). The tree held only committed work when this
# started, so what git calls changed is what the passes wrote.
changed=(${(f)"$(git -C $root diff --name-only -- '*.go')"})
for file in $changed; do
	$binaries/line_wrap -w $root/$file || { print -u2 "house_names: line_wrap refused $file; stopping"; exit 4; }
done
print "house_names: wrapped ${#changed} changed files"
print "house_names: done (${(j:, :)selected})"
