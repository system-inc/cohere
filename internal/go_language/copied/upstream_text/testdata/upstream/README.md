# The upstream text cohere's copies are of, recorded for tests

Each Go file in this module that declares itself a copy (`// cohere:copied <module>@<version> <path>`, read by
`internal/go_language/copied`) is of one upstream file, recorded here byte for byte at `<module>@<version>/<path>`:
`golang.org/x/tools@v0.50.0/` from the module cache, and `std@go1.27.0/` from the Go release's own `src`.
`SHA256SUMS` holds the sha256 of every file here, in sha256sum's form, so `shasum -a 256 -c SHA256SUMS` checks them
from this directory.

The Go project and golang.org/x/tools are BSD licensed. Each source's LICENSE is kept beside its files, in
`<module>@<version>/LICENSE`.

Nothing here is built into cohere's binary or judged as cohere's own: Go and cohere's walks never enter testdata.
It is test input for TestEveryCopyMatchesUpstreamOutsideItsMarkedChanges in `../../../every_copy` and
TestEveryRecordedFileMatchesItsDigest in `../../upstream_text_test.go`, which read only the repository, never the
module cache or a toolchain.

The std files were recorded from go1.27.4's source, whose three files are go1.27.0's byte for byte: their digests
are those `internal/go_language/load/build`'s copiedSources pins for go1.27.0.

To record a copy added or bumped, run the generator from the module root, naming the module cache and the root of a
Go distribution of the release a std copy names (one the module cache's `golang.org/toolchain` holds has its LICENSE
and VERSION where the generator reads them), then delete any file here no copy names any more, which the test names:

    go run ./internal/go_language/copied/upstream_text/tools/generate -module-cache "$(go env GOMODCACHE)" \
        -toolchain-root <distribution root>

Recorded text is never edited by hand: a copy's changes are marked in the copy, and the test fails on a file here
whose digest is not the one `SHA256SUMS` holds.
