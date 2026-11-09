# git index fixtures, one per version

Each `version-N.index` is a `.git/index` written by real git (2.54.0) for one small repository, and each
`version-N.paths` beside it lists, one per line in the index's byte order, exactly the tracked paths a reader
must yield from it: every path once, whatever its stage or flags. The object format is SHA-1. Both cohere's
Go reader (`internal/gitignore/tracked_index.go`) and the Swift engine's read these same bytes, so neither may be
regenerated for one reader alone.

The repository holds a nested directory, a path with a space, and four paths under
`shared/prefix/with/a/long/directory/`, whose long shared prefix is what version 4's path compression
removes. `conflicted.txt` is written at stages 1, 2 and 3, as a merge in conflict leaves it, and yields one
path. Version 2 has no room for extended flags, so its index stops there. Versions 3 and 4 add an
intent-to-add entry (`intended.txt`) and mark `nested/directory/file.txt` skip-worktree, which is what makes
git write version 3 at all: asked for 3 with no extended flag set, it writes 2.

They were made with this script, run with `sh make-fixtures.sh <directory>`:

```sh
set -eu
out=$1
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
git init -q -b main "$out/repository"
cd "$out/repository"
long=shared/prefix/with/a/long/directory/name
mkdir -p nested/directory "$long"
for path in top.txt "path with space.txt" nested/directory/file.txt "$long/alpha.txt" "$long/beta.txt" \
	"$long/gamma.txt" shared/prefix/with/a/long/directory/other.txt; do
	printf '%s\n' "$path" > "$path"
done
git add .
blob=$(git hash-object -w top.txt)
printf '100644 %s 1\tconflicted.txt\n100644 %s 2\tconflicted.txt\n100644 %s 3\tconflicted.txt\n' \
	"$blob" "$blob" "$blob" | git update-index --index-info
git update-index --index-version 2
cp .git/index "$out/version-2.index"
git ls-files --stage -z | tr '\0' '\n' | cut -f2- | uniq > "$out/version-2.paths"
printf 'intended\n' > intended.txt
git add -N intended.txt
git update-index --skip-worktree nested/directory/file.txt
git update-index --index-version 3
cp .git/index "$out/version-3.index"
git ls-files --stage -z | tr '\0' '\n' | cut -f2- | uniq > "$out/version-3.paths"
git update-index --index-version 4
cp .git/index "$out/version-4.index"
git ls-files --stage -z | tr '\0' '\n' | cut -f2- | uniq > "$out/version-4.paths"
```

The stat data in each entry (times, device, inode, owner) is the machine's that wrote it and means nothing
here; a reader takes only the mode, the flags and the path.
