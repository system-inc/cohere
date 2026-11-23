# golang.org/x/tools v0.50.0

This directory is `golang.org/x/tools` at `v0.50.0`, copied from the module cache by `go run ./tools/go_libraries` from cohere's root, which wrote every file here, this one too. Edit the command, not the copy, and run it again after every bump of the module in cohere's go.mod.

Every import of `go/types` in a copied Go file outside testdata names `github.com/system-inc/go-types/types` instead, cohere's fork, since the fork's types are not the standard library's and every package that hands types to cohere must check against the same ones. Nothing else in a Go file changed. Test files were left behind, and testdata is copied whole, byte for byte, for the tests of cohere's that read it.

go.mod has one change, this appended:

```
// cohere: the fork of go/types every copied import of go/types names instead, which cohere's go.mod
// replaces with libraries/go/go-types (tools/go_libraries).
require github.com/system-inc/go-types v0.0.0
```

The packages are those a Go file of cohere's imports, tests included, or a go.mod in go.work names as a tool, and every package of the three copied modules theirs import in turn, on every platform. A file built only under the tag `ignore`, a program run by hand, is copied with its package, but what it imports is not followed.

## Packages (84)

- `golang.org/x/tools/cmd/stringer`
- `golang.org/x/tools/go/analysis`
- `golang.org/x/tools/go/analysis/checker`
- `golang.org/x/tools/go/analysis/internal`
- `golang.org/x/tools/go/analysis/passes/appends`
- `golang.org/x/tools/go/analysis/passes/asmdecl`
- `golang.org/x/tools/go/analysis/passes/assign`
- `golang.org/x/tools/go/analysis/passes/atomic`
- `golang.org/x/tools/go/analysis/passes/bools`
- `golang.org/x/tools/go/analysis/passes/buildssa`
- `golang.org/x/tools/go/analysis/passes/buildtag`
- `golang.org/x/tools/go/analysis/passes/cgocall`
- `golang.org/x/tools/go/analysis/passes/composite`
- `golang.org/x/tools/go/analysis/passes/copylock`
- `golang.org/x/tools/go/analysis/passes/ctrlflow`
- `golang.org/x/tools/go/analysis/passes/defers`
- `golang.org/x/tools/go/analysis/passes/directive`
- `golang.org/x/tools/go/analysis/passes/errorsas`
- `golang.org/x/tools/go/analysis/passes/framepointer`
- `golang.org/x/tools/go/analysis/passes/hostport`
- `golang.org/x/tools/go/analysis/passes/httpresponse`
- `golang.org/x/tools/go/analysis/passes/ifaceassert`
- `golang.org/x/tools/go/analysis/passes/inspect`
- `golang.org/x/tools/go/analysis/passes/loopclosure`
- `golang.org/x/tools/go/analysis/passes/lostcancel`
- `golang.org/x/tools/go/analysis/passes/nilfunc`
- `golang.org/x/tools/go/analysis/passes/nilness`
- `golang.org/x/tools/go/analysis/passes/printf`
- `golang.org/x/tools/go/analysis/passes/scannererr`
- `golang.org/x/tools/go/analysis/passes/shift`
- `golang.org/x/tools/go/analysis/passes/sigchanyzer`
- `golang.org/x/tools/go/analysis/passes/slog`
- `golang.org/x/tools/go/analysis/passes/sqlrowserr`
- `golang.org/x/tools/go/analysis/passes/stdmethods`
- `golang.org/x/tools/go/analysis/passes/stdversion`
- `golang.org/x/tools/go/analysis/passes/stringintconv`
- `golang.org/x/tools/go/analysis/passes/structtag`
- `golang.org/x/tools/go/analysis/passes/testinggoroutine`
- `golang.org/x/tools/go/analysis/passes/tests`
- `golang.org/x/tools/go/analysis/passes/timeformat`
- `golang.org/x/tools/go/analysis/passes/unmarshal`
- `golang.org/x/tools/go/analysis/passes/unreachable`
- `golang.org/x/tools/go/analysis/passes/unsafeptr`
- `golang.org/x/tools/go/analysis/passes/unusedresult`
- `golang.org/x/tools/go/analysis/passes/unusedwrite`
- `golang.org/x/tools/go/analysis/passes/waitgroup`
- `golang.org/x/tools/go/analysis/suite/vet`
- `golang.org/x/tools/go/ast/astutil`
- `golang.org/x/tools/go/ast/edge`
- `golang.org/x/tools/go/ast/inspector`
- `golang.org/x/tools/go/cfg`
- `golang.org/x/tools/go/gcexportdata`
- `golang.org/x/tools/go/packages`
- `golang.org/x/tools/go/ssa`
- `golang.org/x/tools/go/types/objectpath`
- `golang.org/x/tools/go/types/typeutil`
- `golang.org/x/tools/internal/aliases`
- `golang.org/x/tools/internal/analysis/analyzerutil`
- `golang.org/x/tools/internal/analysis/driverutil`
- `golang.org/x/tools/internal/analysis/typeindex`
- `golang.org/x/tools/internal/astutil`
- `golang.org/x/tools/internal/astutil/free`
- `golang.org/x/tools/internal/diff`
- `golang.org/x/tools/internal/diff/lcs`
- `golang.org/x/tools/internal/event`
- `golang.org/x/tools/internal/event/core`
- `golang.org/x/tools/internal/event/keys`
- `golang.org/x/tools/internal/event/label`
- `golang.org/x/tools/internal/fmtstr`
- `golang.org/x/tools/internal/gcimporter`
- `golang.org/x/tools/internal/gocommand`
- `golang.org/x/tools/internal/moreiters`
- `golang.org/x/tools/internal/moremaps`
- `golang.org/x/tools/internal/packagepath`
- `golang.org/x/tools/internal/packagesinternal`
- `golang.org/x/tools/internal/pkgbits`
- `golang.org/x/tools/internal/refactor`
- `golang.org/x/tools/internal/stdlib`
- `golang.org/x/tools/internal/typeparams`
- `golang.org/x/tools/internal/typesinternal`
- `golang.org/x/tools/internal/typesinternal/typeindex`
- `golang.org/x/tools/internal/versions`
- `golang.org/x/tools/refactor/satisfy`
- `golang.org/x/tools/txtar`

## Rewrites (103)

- `cmd/stringer/stringer.go:87`
- `go/analysis/analysis.go:12`
- `go/analysis/checker/checker.go:35`
- `go/analysis/passes/appends/appends.go:12`
- `go/analysis/passes/asmdecl/asmdecl.go:15`
- `go/analysis/passes/assign/assign.go:14`
- `go/analysis/passes/bools/bools.go:12`
- `go/analysis/passes/buildssa/buildssa.go:13`
- `go/analysis/passes/cgocall/cgocall.go:15`
- `go/analysis/passes/cgocall/cgocall_go120.go:9`
- `go/analysis/passes/cgocall/cgocall_go121.go:9`
- `go/analysis/passes/composite/composite.go:12`
- `go/analysis/passes/copylock/copylock.go:14`
- `go/analysis/passes/ctrlflow/ctrlflow.go:13`
- `go/analysis/passes/errorsas/errorsas.go:12`
- `go/analysis/passes/hostport/hostport.go:14`
- `go/analysis/passes/httpresponse/httpresponse.go:11`
- `go/analysis/passes/ifaceassert/ifaceassert.go:10`
- `go/analysis/passes/loopclosure/loopclosure.go:10`
- `go/analysis/passes/lostcancel/lostcancel.go:11`
- `go/analysis/passes/nilfunc/nilfunc.go:13`
- `go/analysis/passes/nilness/nilness.go:12`
- `go/analysis/passes/printf/printf.go:13`
- `go/analysis/passes/printf/types.go:10`
- `go/analysis/passes/scannererr/scannererr.go:13`
- `go/analysis/passes/shift/dead.go:13`
- `go/analysis/passes/shift/shift.go:17`
- `go/analysis/passes/sigchanyzer/sigchanyzer.go:17`
- `go/analysis/passes/slog/slog.go:16`
- `go/analysis/passes/sqlrowserr/sqlrowserr.go:13`
- `go/analysis/passes/stdmethods/stdmethods.go:10`
- `go/analysis/passes/stdversion/stdversion.go:12`
- `go/analysis/passes/stringintconv/string.go:11`
- `go/analysis/passes/structtag/structtag.go:13`
- `go/analysis/passes/testinggoroutine/testinggoroutine.go:12`
- `go/analysis/passes/testinggoroutine/util.go:9`
- `go/analysis/passes/tests/tests.go:11`
- `go/analysis/passes/timeformat/timeformat.go:14`
- `go/analysis/passes/unmarshal/unmarshal.go:10`
- `go/analysis/passes/unsafeptr/unsafeptr.go:13`
- `go/analysis/passes/unusedresult/unusedresult.go:19`
- `go/analysis/passes/unusedwrite/unusedwrite.go:9`
- `go/gcexportdata/gcexportdata.go:51`
- `go/gcexportdata/importer.go:10`
- `go/gcexportdata/main.go:15`
- `go/packages/packages.go:18`
- `go/ssa/builder.go:80`
- `go/ssa/const.go:13`
- `go/ssa/create.go:14`
- `go/ssa/emit.go:13`
- `go/ssa/func.go:14`
- `go/ssa/instantiate.go:9`
- `go/ssa/lvalue.go:13`
- `go/ssa/methods.go:11`
- `go/ssa/print.go:13`
- `go/ssa/sanity.go:14`
- `go/ssa/source.go:16`
- `go/ssa/ssa.go:15`
- `go/ssa/subst.go:9`
- `go/ssa/typeset.go:8`
- `go/ssa/util.go:13`
- `go/ssa/wrappers.go:26`
- `go/types/objectpath/main.go:21`
- `go/types/objectpath/objectpath.go:29`
- `go/types/typeutil/callee.go:9`
- `go/types/typeutil/imports.go:7`
- `go/types/typeutil/map.go:12`
- `go/types/typeutil/methodsetcache.go:10`
- `go/types/typeutil/ui.go:10`
- `internal/aliases/aliases.go:9`
- `internal/analysis/driverutil/fix.go:20`
- `internal/gcimporter/bimport.go:13`
- `internal/gcimporter/gcimporter.go:28`
- `internal/gcimporter/iexport.go:235`
- `internal/gcimporter/iimport.go:16`
- `internal/gcimporter/main.go:18`
- `internal/gcimporter/predeclared.go:8`
- `internal/gcimporter/ureader.go:12`
- `internal/refactor/delete.go:13`
- `internal/refactor/imports.go:12`
- `internal/refactor/refactor.go:14`
- `internal/stdlib/generate.go:28`
- `internal/typeparams/common.go:18`
- `internal/typeparams/coretype.go:9`
- `internal/typeparams/free.go:8`
- `internal/typeparams/normalize.go:10`
- `internal/typeparams/termlist.go:13`
- `internal/typeparams/typeterm.go:12`
- `internal/typesinternal/assignedaddress.go:10`
- `internal/typesinternal/classify_call.go:10`
- `internal/typesinternal/element.go:9`
- `internal/typesinternal/fx.go:10`
- `internal/typesinternal/isnamed.go:8`
- `internal/typesinternal/qualifier.go:9`
- `internal/typesinternal/recv.go:8`
- `internal/typesinternal/toonew.go:8`
- `internal/typesinternal/typeindex/typeindex.go:14`
- `internal/typesinternal/types.go:24`
- `internal/typesinternal/varkind.go:9`
- `internal/typesinternal/varkind_go124.go:9`
- `internal/typesinternal/zerovalue.go:11`
- `internal/versions/types.go:9`
- `refactor/satisfy/find.go:47`
