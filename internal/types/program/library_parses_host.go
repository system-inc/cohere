package program

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

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
