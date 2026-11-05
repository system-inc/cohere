package program

import (
	"crypto/sha256"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/bundled"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// LibraryParses is one parse of each bundled lib file, shared read-only by every program built with it
// (Options.LibraryParses).
//
// Every program parses the lib files its target pulls in: 58 of them for ES2022, about 3.25ms and 2.5 MB
// retained, against 0.1ms for the one-file fixture a rule test builds around them. A test binary builds
// thousands of those programs, so it parsed the same embedded files thousands of times and kept every copy
// (#7nx3z1n, #0rj7wvg). The files are compiled into the binary and cannot change while it runs, so one
// parse of each can serve every program, the way typescript-go's language server shares parses across its
// projects (internal/project/parsecache.go).
//
// # Why sharing a parsed file is safe
//
// A parse is a function of its key: the parse options (file name, path, and how the file decides whether
// it is a module), the script kind, and the text. That is upstream's own parse cache key, and a lib file's
// target or lib setting enters none of it: a different target loads a different set of lib files, not a
// different parse of the same one. The text is keyed by its hash rather than assumed, so a stand-in lib
// directory can never be answered with the embedded parse.
//
// After the parse, nothing a program does writes to the file. Binding is once per file and the same for
// every program (SourceFile.BindOnce), and every type a checker computes lives in that checker. The
// compiler's loader writes to a file only for content-mapped ones, which a lib file never is.
//
// Only bundled files are shared. A file on disk can change between builds, and the run cache and the
// content pack exist to say when it has; the embedded libs are the one set that provably cannot.
type LibraryParses struct {
	mutex   sync.Mutex
	entries map[libraryParseKey]*libraryParse

	// hashes is each bundled file's content hash, by name, computed once: the embedded text cannot change,
	// and hashing all of it on every build would cost what the cache saves.
	hashes map[string][sha256.Size]byte
}

// libraryParseKey is upstream's parse cache key, with a content hash in place of its file handle's.
type libraryParseKey struct {
	options    ast.SourceFileParseOptions
	scriptKind core.ScriptKind
	hash       [sha256.Size]byte
}

// libraryParse is one key's parse, made once by whichever program asks first.
type libraryParse struct {
	once sync.Once
	file *ast.SourceFile
}

// NewLibraryParses returns an empty cache. One per process is the intended use: every program built with
// it shares its parses for as long as it lives.
func NewLibraryParses() *LibraryParses {
	return &LibraryParses{entries: map[libraryParseKey]*libraryParse{}, hashes: map[string][sha256.Size]byte{}}
}

// Len is how many distinct parses the cache holds.
func (parses *LibraryParses) Len() int {
	parses.mutex.Lock()
	defer parses.mutex.Unlock()
	return len(parses.entries)
}

// sourceFile answers a bundled file from the cache, parsing it on first use. It reports false for a file
// that is not bundled or cannot be read, which the caller loads the ordinary way.
func (parses *LibraryParses) sourceFile(
	options ast.SourceFileParseOptions,
	read func(string) (string, bool),
) (*ast.SourceFile, bool) {
	if !bundled.IsBundled(options.FileName.AsString()) {
		return nil, false
	}
	parses.mutex.Lock()
	hash, hashed := parses.hashes[options.FileName.AsString()]
	parses.mutex.Unlock()
	var text string
	if !hashed {
		contents, ok := read(options.FileName.AsString())
		if !ok {
			return nil, false
		}
		text, hash = contents, sha256.Sum256([]byte(contents))
		parses.mutex.Lock()
		parses.hashes[options.FileName.AsString()] = hash
		parses.mutex.Unlock()
	}

	key := libraryParseKey{
		options:    options,
		scriptKind: core.EnsureScriptKindFromFileName(options.FileName),
		hash:       hash,
	}
	parses.mutex.Lock()
	entry, found := parses.entries[key]
	if !found {
		entry = &libraryParse{}
		parses.entries[key] = entry
	}
	parses.mutex.Unlock()

	entry.once.Do(func() {
		if text == "" {
			contents, ok := read(options.FileName.AsString())
			if !ok {
				return
			}
			text = contents
		}
		entry.file = parser.ParseSourceFile(options, text, key.scriptKind)
	})
	return entry.file, entry.file != nil
}

// libraryParsesHost serves bundled lib files from a shared cache and loads everything else as the host
// beneath it does.
type libraryParsesHost struct {
	compiler.CompilerHost
	parses *LibraryParses
}

func (host *libraryParsesHost) GetSourceFile(options ast.SourceFileParseOptions) *ast.SourceFile {
	if file, shared := host.parses.sourceFile(
		options,
		func(path string) (string, bool) { return host.FS().ReadFile(tspath.RootedFilePath(path)) },
	); shared {
		return file
	}
	return host.CompilerHost.GetSourceFile(options)
}
