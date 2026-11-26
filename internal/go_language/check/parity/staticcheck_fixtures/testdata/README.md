# staticcheck's check fixtures, vendored for tests

These files are copied from staticcheck's module, `honnef.co/go/tools` at `v0.8.1`, the version cohere's go.mod
requires: every `testdata` folder under its `staticcheck/`, `simple/`, `stylecheck/` and `quickfix/` checks (161
folders, 368 files), with the directory structure kept. staticcheck is MIT licensed. Its LICENSE is kept beside them
in LICENSE.

The contents are unchanged. The names are not, in one way: each `.go` file is kept as `.go.fixture`, since
internal/testing/guard holds every `.go` file in the repository to gofmt, testdata included, and some of these are not
gofmt-clean on purpose. `staticcheck_fixtures.Directory` copies a folder into a test's own directory with each Go file
under its own name again. Nothing here is built into cohere's binary; it is test input for the ports that are held to
staticcheck's own wants.

To refresh to another release, after go.mod moves, copy the same folders byte for byte from that release's module
directory, renaming each `.go` file the same way, and update the version above, LICENSE, and the counts the
staticcheck_wants tests hold.
