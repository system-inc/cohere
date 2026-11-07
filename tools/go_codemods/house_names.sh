#!/bin/zsh
# Brings a cohere worktree's Go up to the house names main already holds, by running each codemod pass, in the
# order the passes landed, over the whole tree. Every pass is deterministic: it renames only through the rename
# engine (a rename is accepted only if every identifier still resolves to the same object), reloads the tree, and
# fails unless it type-checks with every identifier matching. Running it on a tree that already has the names
# changes nothing.
#
# Usage, from the root of a cohere worktree:
#   tools/go_codemods/house_names.sh
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
	'word_names -global -tag-encoded-fields -rename-owned-spellings -rename-library-exports static_single_assignment,mutation_aliasing')

if [[ ! -f go.work || ! -d tools/go_codemods/command ]]; then
	print -u2 "house_names: run this from the root of a cohere worktree (no go.work or tools/go_codemods here)"
	exit 2
fi
root=$PWD

# The builds live in a directory of their own, named so loadRunning can know a pass by its executable's path.
binaries=$(mktemp -d)/go_codemods_bin
mkdir -p $binaries
trap 'rm -f $binaries/*(N); rmdir $binaries ${binaries:h} 2>/dev/null' EXIT
for command in ${(u)${passes%% *}} line_wrap; do
	if ! go run ./command/cohere-dev build -o $binaries/$command ./tools/go_codemods/command/$command; then
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

for specification in $passes; do
	pass=${specification%% *}
	flags=(${(s: :)${specification#$pass}})
	# One whole-workspace load at a time on this machine: the loads are several gigabytes each.
	while loadRunning; do
		print -u2 "house_names: waiting for another load to finish before $pass"
		sleep 30
	done
	print "house_names: $pass $flags"
	go run ./command/cohere-dev exec -- $binaries/$pass -root $root $flags 2>&1 \
		| grep -E '^(proposals|candidates|accepted|rejected|wrote|reloaded|re-verification)|package errors|mismatch'
	exitCode=${pipestatus[1]}
	if (( exitCode != 0 )); then
		print -u2 "house_names: $pass exited $exitCode; stopping. Read its output by running it alone:"
		print -u2 "  go run ./command/cohere-dev exec -- <a build of tools/go_codemods/command/$pass> -root $root $flags"
		exit $exitCode
	fi
done

# A renamed name is often longer, so the files the passes changed are rewrapped to 120 columns: each file is proven
# before it is written (the same syntax tree, the same comment words). The tree held only committed work when this
# started, so what git calls changed is what the passes wrote.
changed=(${(f)"$(git diff --name-only -- '*.go')"})
for file in $changed; do
	$binaries/line_wrap -w $file || { print -u2 "house_names: line_wrap refused $file; stopping"; exit 4; }
done
print "house_names: wrapped ${#changed} changed files"
print "house_names: done (${(j:, :)passes})"
