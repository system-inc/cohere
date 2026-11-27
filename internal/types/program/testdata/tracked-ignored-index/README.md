# git indexes for the tracked-ignored run cache

Each `.index` here is a `.git/index` written by real git (2.54.0, Apple Git-157) for one small repository, in
the three states `run_cache_tracked_ignored_test.go` lays into a temporary tree so that it starts no git
itself. The repository holds `.gitignore` (`*.log`), `main.go`, `other.go` and an untracked `debug.log`, which
the ignore file excludes.

- `committed.index` is the index after the first commit tracks `.gitignore`, `main.go` and `other.go`.
- `status-rewritten.index` is the index after `other.go`'s modification time moved two seconds and a
  `git status` refreshed its stat data: the same entries at the same modes, in other bytes.
- `tracks-ignored.index` is the index after `git add -f debug.log` newly tracks the ignored file.

The worktree test lays the first two as a worktree's index too, since a worktree checked out from that
commit tracks the same paths at the same modes. The object format is SHA-1.

They were made with this script, run with `sh make-indexes.sh <directory>`:

```sh
set -eu
out=$1
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null HOME="$out/repository"
git() { command git -c user.name=cohere -c user.email=cohere@localhost -c core.excludesFile=/dev/null "$@"; }
mkdir "$out/repository"
cd "$out/repository"
git init -q -b main
printf '*.log\n' > .gitignore
printf 'package main\n' > main.go
printf 'package main\n' > other.go
printf 'a log\n' > debug.log
git add .gitignore main.go other.go
git commit -q -m first
cp .git/index "$out/committed.index"
touch -A 02 other.go
git status --porcelain
cp .git/index "$out/status-rewritten.index"
git add -f debug.log
cp .git/index "$out/tracks-ignored.index"
```

The stat data in each entry (times, device, inode, owner) is the machine's that wrote it and means nothing
here: the matcher takes only each entry's mode and path, and never compares stat data with the files on disk.
