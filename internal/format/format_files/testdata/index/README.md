# The subproject walk's recorded indexes

`subproject.index` is the `.git/index` real git (2.54.0) wrote for the repository that
`enumerate_subproject_test.go` lays out, and `subproject-split.index` is the same index after
`git update-index --split-index`, whose `link` extension the reader refuses. The test writes the tree and places
one of these as its `.git/index`, so it runs no git. The index tracks exactly these paths:

```
.gitignore
CohereSettings.json
app/.gitignore
app/CohereSettings.json
app/index.ts
app/kept.generated.ts
app/scratch.ts
nexus/NexusCohereSettings.json
```

`app/kept.generated.ts` and `app/scratch.ts` were added by force past the `.gitignore` patterns that match them;
`app/other.generated.ts` and `app/stray/scratch.ts` are untracked.

They were made with this script, run with `env -i PATH=/usr/bin:/bin sh make-fixtures.sh <directory>`, so git
read no configuration of the machine's:

```sh
set -eu
out=$1
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 HOME="$out"
git init -q -b main "$out/repository"
cd "$out/repository"
mkdir -p nexus app/stray
printf '*.generated.ts\n' > .gitignore
printf 'scratch.ts\n' > app/.gitignore
printf 'export const index = 1;\n' > app/index.ts
printf 'export const kept = 1;\n' > app/kept.generated.ts
printf 'export const other = 1;\n' > app/other.generated.ts
printf 'export const scratch = 1;\n' > app/scratch.ts
printf 'export const stray = 1;\n' > app/stray/scratch.ts
printf '{"extends": "../nexus/NexusCohereSettings.json", "rules": {}}' > app/CohereSettings.json
printf '{"rules": {}, "format": {"tabWidth": 4, "ignore": []}}' > nexus/NexusCohereSettings.json
printf '{"extends": "./nexus/NexusCohereSettings.json", "rules": {}, "ignorePatterns": []}' > CohereSettings.json
git add -A
git add -f app/kept.generated.ts app/scratch.ts
cp .git/index "$out/subproject.index"
git update-index --split-index
cp .git/index "$out/subproject-split.index"
```

The stat data in each entry is the machine's that wrote it and means nothing here; the reader takes only the mode,
the flags and the path.
