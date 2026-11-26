# git's answers over the package's fixtures

Every file here is one git command's output, byte for byte, as real git (2.54.0, Apple Git-157) printed
it. The package's tests compare their arithmetic against these answers and start no git of their own.
They were made with the script below, run as
`env -i PATH=/usr/bin:/bin HOME=<an empty directory> bash make-recordings.sh <directory>`; it runs git
with no configuration of the machine's and a fixed author, committer and date, so a second run writes the
same bytes.

## `sha1/` and `sha256/`, one per object format

The fixture is the tree `fixture` in `git_tree_test.go` lays, the script laying the same bytes: an empty
file, a 1 MiB file, a name with a tab and a newline, two links (one dangling), siblings that sort on either
side of the directory `a`, a gitlink `sub` with no checkout pinned at a commit of fives, a checked-out
submodule `module`, and a file at each of eight permissions. The submodule is added by
`git submodule add` and its url then set to `./module-source`, so `.gitmodules` holds no temporary path;
the fixture lays that `.gitmodules` and the `.git` file git leaves in `module/` with the same bytes.

| File | git's answer |
| --- | --- |
| `ls-files-stage.z` | `git ls-files --stage -z` of the fixture's index |
| `ls-files-stage` | `git ls-files --stage`, the same index without -z, its odd name quoted |
| `write-tree` | `git write-tree` of that index |
| `empty-tree` | `git hash-object -t tree --stdin </dev/null`, the empty tree |
| `hash-object-no-filters.z` | `git hash-object --no-filters -- <path>` of each regular file, one record per path: the id, a tab, the path and a zero byte |
| `rev-parse-head` | `git rev-parse HEAD` once the fixture is committed with message `fixture` |
| `rev-parse-head-tree` | `git rev-parse HEAD^{tree}` of that commit |
| `cat-file-commit-head` | `git cat-file commit HEAD` of that commit |
| `ls-tree-r-z-full-tree-head` | `git ls-tree -r -z --full-tree HEAD` of that commit |
| `ls-tree-r-t-z-full-tree-head` | `git ls-tree -r -t -z --full-tree HEAD` of that commit |
| `later/rev-parse-head` | `git rev-parse HEAD` once `hello` is rewritten and committed with message `later` |
| `later/cat-file-commit-head` | `git cat-file commit HEAD` of that later commit |
| `diff-files-name-only.z` | `git diff-files --name-only -z` over the fixture committed and changed as `TestDiffersFindsWhatGitDiffFilesFinds` changes it, after `git update-index -q --refresh` |
| `merge/rev-parse-head` | `git rev-parse HEAD` of the merge's first commit, whose length the merge's pins take |
| `merge/ls-files-stage.z` | `git ls-files --stage -z` of a merge left in conflict, as `TestParseIndexListingReadsAMergeLeftInConflict` describes it |
| `merge/ls-files-stage` | the same index without -z |

## `golden/`

The answers `manifest_test.go`'s golden record holds, from Loom's manifest fixture in SHA-1: `top/` the
tree's own repository, `sub/` its submodule, recorded in `top` by `git update-index` rather than
`git submodule add`. Each holds `rev-parse-head` (`git rev-parse HEAD`), `cat-file-commit-head`
(`git cat-file commit HEAD`) and `ls-tree-r-z-full-tree-head` (`git ls-tree -r -z --full-tree HEAD`).

## The script

```sh
set -eu
out=$1
work=$(mktemp -d)
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid GIT_AUTHOR_DATE='1700000000 +0000'
export GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid GIT_COMMITTER_DATE='1700000000 +0000'
g() {
	git -c core.fileMode=true -c core.symlinks=true -c core.autocrlf=false -c commit.gpgsign=false \
		-c init.defaultBranch=main -c protocol.file.allow=always "$@"
}
write() { # write <path> <permissions> <content>
	mkdir -p "$(dirname "$1")"
	printf '%s' "$3" > "$1"
	chmod "$2" "$1"
}
odd=$(printf 'tab\tand\nnewline')
regular_files() {
	printf '%s\n' .gitmodules empty hello large a/inner a/deeper/most a-b a.b a0 a_b sub-x/file sub.y \
		permission/0644 permission/0654 permission/0645 permission/0611 permission/0411 \
		permission/0744 permission/0700 permission/0755
}

# fixture <format> <directory>: the tree git_tree_test.go's fixture lays, added to the index, its
# submodule's url rewritten to ./module-source so no temporary path is recorded.
fixture() {
	format=$1 directory=$2
	g init -q --object-format="$format" "$directory"
	cd "$directory"
	write empty 644 ''
	write hello 644 'hello
'
	awk 'BEGIN { for (i = 0; i < 65536; i++) printf "0123456789abcdef" }' > large
	chmod 644 large
	write "$odd" 644 'an odd name
'
	ln -s hello link
	ln -s nowhere/at/all dangling
	write a/inner 644 'inside a
'
	write a/deeper/most 644 'deepest
'
	for sibling in a-b a.b a0 a_b; do
		write "$sibling" 644 "$sibling
"
	done
	write sub-x/file 644 'beside the gitlink
'
	write sub.y 644 'beside the gitlink
'
	for pair in 0644:-rw-r--r-- 0654:-rw-r-xr-- 0645:-rw-r--r-x 0611:-rw---x--x 0411:-r----x--x \
		0744:-rwxr--r-- 0700:-rwx------ 0755:-rwxr-xr-x; do
		write "permission/${pair%%:*}" "${pair%%:*}" "permission ${pair#*:}
"
	done
	g submodule add -q "$work/$format-module-source" module
	g config -f .gitmodules submodule.module.url ./module-source
	g add -A
	mkdir sub
	test "$(cat module/.git)" = 'gitdir: ../.git/modules/module'
	pin=$(g ls-files --stage module | cut -d' ' -f2 | tr '0-9a-f' '5')
	g update-index --add --cacheinfo "160000,$pin,sub"
}

for format in sha1 sha256; do
	mkdir -p "$out/$format/later" "$out/$format/merge"
	g init -q --object-format="$format" "$work/$format-module-source"
	cd "$work/$format-module-source"
	write module.txt 644 "a submodule's file
"
	g add -A
	g commit -q -m module

	fixture "$format" "$work/$format"
	g ls-files --stage -z > "$out/$format/ls-files-stage.z"
	g ls-files --stage > "$out/$format/ls-files-stage"
	g write-tree > "$out/$format/write-tree"
	g hash-object -t tree --stdin < /dev/null > "$out/$format/empty-tree"
	regular_files | while IFS= read -r path; do
		printf '%s\t%s\0' "$(g hash-object --no-filters -- "$path")" "$path"
	done > "$out/$format/hash-object-no-filters.z"
	printf '%s\t%s\0' "$(g hash-object --no-filters -- "$odd")" "$odd" >> "$out/$format/hash-object-no-filters.z"
	# The tests' commit adds the work tree first, as `git add -A` would; it changes nothing.
	g add -A
	test "$(g write-tree)" = "$(cat "$out/$format/write-tree")"
	g ls-files --stage -z | cmp - "$out/$format/ls-files-stage.z"
	g commit -q --allow-empty -m fixture
	g rev-parse HEAD > "$out/$format/rev-parse-head"
	g rev-parse 'HEAD^{tree}' > "$out/$format/rev-parse-head-tree"
	g cat-file commit HEAD > "$out/$format/cat-file-commit-head"
	g ls-tree -r -z --full-tree HEAD > "$out/$format/ls-tree-r-z-full-tree-head"
	g ls-tree -r -t -z --full-tree HEAD > "$out/$format/ls-tree-r-t-z-full-tree-head"

	write hello 644 'hello again
'
	g add -A
	g commit -q --allow-empty -m later
	g rev-parse HEAD > "$out/$format/later/rev-parse-head"
	g cat-file commit HEAD > "$out/$format/later/cat-file-commit-head"

	# TestDiffersFindsWhatGitDiffFilesFinds' changes, over the fixture committed again.
	fixture "$format" "$work/$format-changed"
	g add -A
	g commit -q --allow-empty -m fixture
	test "$(g rev-parse HEAD)" = "$(cat "$out/$format/rev-parse-head")"
	for move in 0644:0654 0654:0645 0645:0744 0744:0700 0700:0611 0611:0411 0411:0100 0755:0644; do
		chmod "${move#*:}" "permission/${move%%:*}"
	done
	write hello 644 'jello
'
	rm link
	write link 644 hello
	rm empty
	rm a/deeper/most
	rmdir a/deeper
	write a/deeper 644 'a file now
'
	g update-index -q --refresh
	g diff-files --name-only -z > "$out/$format/diff-files-name-only.z"

	# The merge left in conflict, as TestParseIndexListingReadsAMergeLeftInConflict describes it.
	g init -q --object-format="$format" "$work/$format-merge"
	cd "$work/$format-merge"
	write merged 644 'merged
'
	write conflicted 644 'base
'
	g add -A
	g commit -q -m files
	g rev-parse HEAD > "$out/$format/merge/rev-parse-head"
	digits=$(tr -d '\n' < "$out/$format/merge/rev-parse-head" | wc -c | tr -d ' ')
	gitlink() { # gitlink <digit> <path>
		g update-index --add --cacheinfo "160000,$(head -c "$digits" < /dev/zero | tr '\0' "$1"),$2"
	}
	gitlink 1 module
	gitlink 1 removed
	g commit -q -m base
	g checkout -q -b theirs
	write conflicted 644 'theirs
'
	g add conflicted
	gitlink 3 module
	gitlink 3 removed
	g commit -q -m theirs
	g checkout -q main
	write conflicted 644 'ours
'
	g add conflicted
	gitlink 2 module
	g update-index --force-remove removed
	g commit -q -m ours
	if g merge -q theirs > /dev/null 2>&1; then
		echo "the merge was meant to conflict" >&2
		exit 1
	fi
	g ls-files --stage -z > "$out/$format/merge/ls-files-stage.z"
	g ls-files --stage > "$out/$format/merge/ls-files-stage"
done

# manifest_test.go's golden record: Loom's manifest fixture, a submodule recorded by update-index.
mkdir -p "$out/golden/top" "$out/golden/sub"
g init -q "$work/golden-sub"
cd "$work/golden-sub"
write sub.txt 644 'sub
'
write x/y.txt 644 'y
'
g add -A
g commit -q -m tree
g init -q "$work/golden-top"
cd "$work/golden-top"
write top.txt 644 'top
'
write a/b.txt 644 'b
'
write z.txt 644 'z
'
ln -s top.txt link
g add -A
g update-index --add --cacheinfo "160000,$(g -C "$work/golden-sub" rev-parse HEAD),sub"
g commit -q -m tree
for repository in top sub; do
	cd "$work/golden-$repository"
	g rev-parse HEAD > "$out/golden/$repository/rev-parse-head"
	g cat-file commit HEAD > "$out/golden/$repository/cat-file-commit-head"
	g ls-tree -r -z --full-tree HEAD > "$out/golden/$repository/ls-tree-r-z-full-tree-head"
done
```
