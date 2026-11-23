# golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678

This directory is `golang.org/x/exp/typeparams` at `v0.0.0-20231108232855-2478ac86f678`, copied from the module cache by `go run ./tools/go_libraries` from cohere's root, which wrote every file here, this one too. Edit the command, not the copy, and run it again after every bump of the module in cohere's go.mod.

Every import of `go/types` in a copied Go file outside testdata names `github.com/system-inc/go-types/types` instead, cohere's fork, since the fork's types are not the standard library's and every package that hands types to cohere must check against the same ones. Nothing else in a Go file changed. Test files were left behind, and testdata is copied whole, byte for byte, for the tests of cohere's that read it.

go.mod has one change, this appended:

```
// cohere: the fork of go/types every copied import of go/types names instead, which cohere's go.mod
// replaces with libraries/go/go-types (tools/go_libraries).
require github.com/system-inc/go-types v0.0.0
```

The packages are those a Go file of cohere's imports, tests included, or a go.mod in go.work names as a tool, and every package of the three copied modules theirs import in turn, on every platform. A file built only under the tag `ignore`, a program run by hand, is copied with its package, but what it imports is not followed.

## Packages (1)

- `golang.org/x/exp/typeparams`

## Rewrites (6)

- `common.go:26`
- `normalize.go:10`
- `termlist.go:11`
- `typeparams_go117.go:12`
- `typeparams_go118.go:11`
- `typeterm.go:9`
