package rule

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tsoptions"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// IsSourceFileDefaultLibrary checks declaration-file kind and the program's exact default-library
// identity, including configured-library path fallback. It reads no source text or declaration syntax.
func IsSourceFileDefaultLibrary(program Program, file *ast.SourceFile) bool {
	if !file.IsDeclarationFile {
		return false
	}

	if program.IsSourceFileDefaultLibrary(file.PathKey()) {
		return true
	}

	options := program.Options()

	if options.NoLib.IsTrue() {
		return false
	}

	// copied from program.go
	var libs []string
	if options.Lib == nil {
		name := tsoptions.GetDefaultLibFileName(options)
		libs = append(libs, tspath.CombinePaths(program.DefaultLibraryPath(), name))
	} else {
		for _, lib := range options.Lib {
			name, ok := tsoptions.GetLibFileName(lib)
			if ok {
				libs = append(libs, tspath.CombinePaths(program.DefaultLibraryPath(), name))
			}
			// !!! error on unknown name
		}
	}

	sensitivity := tspath.CaseInsensitive
	if program.UseCaseSensitiveFileNames() {
		sensitivity = tspath.CaseSensitive
	}
	for _, lib := range libs {
		if tspath.ComparePaths(file.FileName().AsString(), lib, sensitivity) == 0 {
			return true
		}
	}
	return false
}

// IsSymbolFromDefaultLibrary reports whether any declaration belongs to a default library.
// It exposes only provenance, never syntax.
func IsSymbolFromDefaultLibrary(
	program Program,
	symbol *ast.Symbol,
) bool {
	if symbol == nil {
		return false
	}

	for _, declaration := range symbol.Declarations {
		sourceFile := ast.GetSourceFileOfNode(declaration)
		if IsSourceFileDefaultLibrary(program, sourceFile) {
			return true
		}
	}

	return false
}

// IsDeclaredOnlyInDefaultLibraries reports whether every declaration with a source file is in a
// default library. Missing files, an empty symbol, and nil are accepted, preserving the built-in-array
// exemption's vacuous-all query rather than the any query above. It hands back no syntax.
func IsDeclaredOnlyInDefaultLibraries(program Program, symbol *ast.Symbol) bool {
	if symbol == nil {
		return true
	}
	for _, declaration := range symbol.Declarations {
		file := ast.GetSourceFileOfNode(declaration)
		if file != nil && !IsSourceFileDefaultLibrary(program, file) {
			return false
		}
	}
	return true
}
