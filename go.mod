module github.com/system-inc/verify

go 1.27

replace (
	github.com/microsoft/TypeScript/tsc/shim/ast => ./shim/ast
	github.com/microsoft/TypeScript/tsc/shim/bundled => ./shim/bundled
	github.com/microsoft/TypeScript/tsc/shim/checker => ./shim/checker
	github.com/microsoft/TypeScript/tsc/shim/compiler => ./shim/compiler
	github.com/microsoft/TypeScript/tsc/shim/core => ./shim/core
	github.com/microsoft/TypeScript/tsc/shim/format => ./shim/format
	github.com/microsoft/TypeScript/tsc/shim/locale => ./shim/locale
	github.com/microsoft/TypeScript/tsc/shim/parser => ./shim/parser
	github.com/microsoft/TypeScript/tsc/shim/scanner => ./shim/scanner
	github.com/microsoft/TypeScript/tsc/shim/tsoptions => ./shim/tsoptions
	github.com/microsoft/TypeScript/tsc/shim/tspath => ./shim/tspath
	github.com/microsoft/TypeScript/tsc/shim/vfs => ./shim/vfs
	github.com/microsoft/TypeScript/tsc/shim/vfs/cachedvfs => ./shim/vfs/cachedvfs
	github.com/microsoft/TypeScript/tsc/shim/vfs/osvfs => ./shim/vfs/osvfs
)

require (
	github.com/microsoft/TypeScript/tsc/shim/ast v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/bundled v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/checker v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/compiler v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/core v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/format v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/locale v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/parser v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/scanner v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/tsoptions v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/tspath v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/vfs v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/vfs/cachedvfs v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/vfs/osvfs v0.0.0

	// A JavaScript interpreter, in a Go linter, on purpose. verify replaces Prettier, and replacing
	// Prettier means matching it byte for byte on an already-Prettier-formatted tree. typescript-go's
	// own formatter cannot: FormatCodeSettings has no printWidth and no line-breaking engine, and
	// 21.8% of tracked files diverge from our fork after every settings-reachable fix. The only thing
	// that reproduces our formatter is our formatter, and goja is what runs it in-process.
	github.com/dop251/goja v0.0.0-20260822123354-58e940e0d230
)

require (
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
)
