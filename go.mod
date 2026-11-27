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
	github.com/system-inc/cohere/modules/static_single_assignment => ./modules/static_single_assignment

	// React's mutation and aliasing model, the effect vocabulary and the mutable ranges inferred from
	// it, shared with Adamic the same way (#kr2yp54).
	github.com/system-inc/cohere/modules/mutation_aliasing => ./modules/mutation_aliasing

	// The memory limit a process's cgroups hold it to, shared with Loom and Adamic the same way (#mf4x0hb).
	github.com/system-inc/cohere/modules/memory_limit => ./modules/memory_limit

	// Go's build cache layout and how to delete from it safely, shared with Loom the same way (#xwfdr3d).
	github.com/system-inc/cohere/modules/go_cache => ./modules/go_cache

	// What `go list -json` says of a build's packages and the files it names, shared with Loom and Adamic the same
	// way (#qqg66c7).
	github.com/system-inc/cohere/modules/go_listing => ./modules/go_listing

	// The ECMAScript Unicode character property tables cohere's regexp parser reads, shared with Adamic the same way
	// (#gcdfx7v).
	github.com/system-inc/cohere/modules/unicode_properties => ./modules/unicode_properties
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
	github.com/system-inc/cohere/modules/go_cache v0.0.0
	github.com/system-inc/cohere/modules/go_listing v0.0.0
	github.com/system-inc/cohere/modules/memory_limit v0.0.0
	github.com/system-inc/cohere/modules/mutation_aliasing v0.0.0
	github.com/system-inc/cohere/modules/static_single_assignment v0.0.0
	github.com/system-inc/cohere/modules/unicode_properties v0.0.0

	// The CPU profile's reader, in a test only: a profiled run's files are each parsed as `go tool pprof` reads them,
	// so a missing, empty or truncated profile fails (command/cohere/contract_profile_test.go, #r33n96f).
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904

	// A backtracking regular expression engine with an ECMAScript mode: the rules handed JavaScript regexps
	// compile them with it (internal/lint/ecmascript/regexp), since Go's RE2 has no lookaround or backreferences.
	github.com/dlclark/regexp2/v2 v2.5.2

	// A type parameter's terms, read as staticcheck reads them: SA4023's port asks it what the original
	// asks (internal/go_language/rules/typed_nil_interface_comparison).
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678

	// cohere-dev reads go.work and each module's go.mod with the parser go itself uses, to gate every
	// module the repository builds (command/cohere-dev/workspace.go).
	golang.org/x/mod v0.41.0
	golang.org/x/sys v0.48.0

	// cohere-go's analysis framework: every Go rule is a go/analysis analyzer (internal/go_language/rule),
	// so vet's own suite, analysistest and facts across packages work unchanged, and cohere-go loads with
	// go/packages and runs the rules with go/analysis/checker. Tests use it too: the registry's
	// option-decoding guard loads the rules tree with type information, because whether a decode is strict
	// depends on the type it decodes into, which a regex over the source cannot see.
	golang.org/x/tools v0.50.0

	// staticcheck as released. The binary links only its helper packages (type utilities, edits, the
	// generated-file facts); every check it registers is cohere's own port, and the original checks run
	// only in the tests, as the live oracles each port is held to (internal/go_language/check/parity). Move
	// to the first release that carries upstream's fa9ae6d1a5, one object path encoder per package, which
	// makes the original U1000 cheaper in its parity tests, once x/tools is at v0.51, which it requires.
	honnef.co/go/tools v0.8.1
)

require (
	// staticcheck's configuration reader parses staticcheck.conf with it, and the ST checks require that
	// reader for their options (dot import and HTTP status code allow lists).
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)
