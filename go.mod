module github.com/system-inc/cohere

go 1.27

replace (
	github.com/microsoft/TypeScript/tsc/shim/ast => ./TypeScript-shim/ast
	github.com/microsoft/TypeScript/tsc/shim/bundled => ./TypeScript-shim/bundled
	github.com/microsoft/TypeScript/tsc/shim/checker => ./TypeScript-shim/checker
	github.com/microsoft/TypeScript/tsc/shim/compiler => ./TypeScript-shim/compiler
	github.com/microsoft/TypeScript/tsc/shim/core => ./TypeScript-shim/core
	github.com/microsoft/TypeScript/tsc/shim/format => ./TypeScript-shim/format
	github.com/microsoft/TypeScript/tsc/shim/incremental => ./TypeScript-shim/incremental
	github.com/microsoft/TypeScript/tsc/shim/locale => ./TypeScript-shim/locale
	github.com/microsoft/TypeScript/tsc/shim/parser => ./TypeScript-shim/parser
	github.com/microsoft/TypeScript/tsc/shim/scanner => ./TypeScript-shim/scanner
	github.com/microsoft/TypeScript/tsc/shim/tsoptions => ./TypeScript-shim/tsoptions
	github.com/microsoft/TypeScript/tsc/shim/tspath => ./TypeScript-shim/tspath
	github.com/microsoft/TypeScript/tsc/shim/vfs => ./TypeScript-shim/vfs
	github.com/microsoft/TypeScript/tsc/shim/vfs/cachedvfs => ./TypeScript-shim/vfs/cachedvfs
	github.com/microsoft/TypeScript/tsc/shim/vfs/osvfs => ./TypeScript-shim/vfs/osvfs

	// The single static assignment form cohere's high-level IR and Adamic's flow graph share, a
	// module of its own so Adamic can import it through its cohere submodule (#ejcnkja).
	github.com/system-inc/cohere/static_single_assignment => ./static_single_assignment

	// React's mutation and aliasing model, the effect vocabulary and the mutable ranges inferred from
	// it, shared with Adamic the same way (#kr2yp54).
	github.com/system-inc/cohere/mutation_aliasing => ./mutation_aliasing

	// git's own arithmetic over a repository's tree: its objects' ids and the listings `git ls-tree -z` and
	// `git ls-files --stage -z` print, shared with Loom and Adamic the same way (#4mrck67).
	github.com/system-inc/cohere/git_tree => ./git_tree

	// The memory limit a process's cgroups hold it to, shared with Loom and Adamic the same way (#mf4x0hb).
	github.com/system-inc/cohere/memory_limit => ./memory_limit

	// Go's build cache layout and how to delete from it safely, shared with Loom the same way (#xwfdr3d).
	github.com/system-inc/cohere/go_cache => ./go_cache

	// staticcheck v0.8.1 as released, but for upstream's fa9ae6d1a5 in unused/: U1000 shares one object path
	// encoder per package, where the release walks the package afresh for every object it records. That walk was
	// most of U1000's CPU at cohere's root, 4.3 to 4.5 core-seconds, and with the fix it is 0.5 to 0.7, findings
	// the same (#j3bfnyt). The copy and this line are deleted at the first staticcheck release that carries the
	// commit (third_party/honnef.co/go/tools/README.cohere.md).
	honnef.co/go/tools => ./third_party/honnef.co/go/tools
)

require (
	github.com/microsoft/TypeScript/tsc/shim/ast v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/bundled v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/checker v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/compiler v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/core v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/format v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/incremental v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/locale v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/parser v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/scanner v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/tsoptions v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/tspath v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/vfs v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/vfs/cachedvfs v0.0.0
	github.com/microsoft/TypeScript/tsc/shim/vfs/osvfs v0.0.0
	github.com/system-inc/cohere/git_tree v0.0.0
	github.com/system-inc/cohere/go_cache v0.0.0
	github.com/system-inc/cohere/memory_limit v0.0.0
	github.com/system-inc/cohere/mutation_aliasing v0.0.0
	github.com/system-inc/cohere/static_single_assignment v0.0.0

	// A JavaScript interpreter, in a Go linter, on purpose. cohere replaces Prettier, and replacing
	// Prettier means matching it byte for byte on an already-Prettier-formatted tree. typescript-go's
	// own formatter cannot: FormatCodeSettings has no printWidth and no line-breaking engine, and
	// 21.8% of tracked files diverge from our fork after every settings-reachable fix. The only thing
	// that reproduces our formatter is our formatter, and goja is what runs it in-process.
	github.com/dop251/goja v0.0.0-20260822123354-58e940e0d230

	// A type parameter's terms, read as staticcheck reads them: SA4023's port asks it what the original
	// asks (internal/go_engine/rules/typed_nil_interface_comparison).
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678

	// cohere-dev reads go.work and each module's go.mod with the parser go itself uses, to gate every
	// module the repository builds (command/cohere-dev/workspace.go).
	golang.org/x/mod v0.41.0
	golang.org/x/sys v0.48.0

	// The Go engine's analysis framework: every Go rule is a go/analysis analyzer (internal/go_engine/rule),
	// so vet's own suite, analysistest and facts across packages work unchanged, and the engine loads with
	// go/packages and runs the rules with go/analysis/checker. Tests use it too: the registry's
	// option-decoding guard loads the rules tree with type information, because whether a decode is strict
	// depends on the type it decodes into, which a regex over the source cannot see.
	golang.org/x/tools v0.50.0

	// staticcheck's SA, S, ST and U1000 checks, which the Go engine registers as rules under their own codes
	// (internal/go_engine/rules). They are go/analysis analyzers already, so they run in the engine's one
	// load with no runner of staticcheck's own. Minimal version selection keeps our x/tools over the older
	// one staticcheck asks for, and the registry's tests prove its analyzers validate against ours.
	honnef.co/go/tools v0.8.1
)

require (
	// staticcheck's configuration reader parses staticcheck.conf with it, and the ST checks require that
	// reader for their options (dot import and HTTP status code allow lists).
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)
