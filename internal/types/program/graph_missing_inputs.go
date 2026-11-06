package program

import (
	"sort"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// MissingInput is a file the program asked for and could not find: an import or a `/// <reference path>`
// in one of the project's own files whose target does not exist.
type MissingInput struct {
	// Importer is the file that asks for it, and Line and Column (one-based) where.
	Importer string
	Line     int
	Column   int
	// Specifier is the path as the importer wrote it, and Target the absolute path it names.
	Specifier string
	Target    string
}

// MissingGeneratedInputs is every import and reference in the project's files whose target does not exist
// and is generated output, as generated decides from the absolute path. Ordered by importer, then position.
//
// Next.js is the case that asked (#wxsqavy): next-env.d.ts imports .next/types/routes.d.ts, which holds
// the route globals (PageProps, LayoutProps, RouteContext), and a fresh checkout has no .next until `next
// typegen` writes it. Every file that names one of those globals then fails, and the run reads as dozens of
// separate type errors with one cause.
//
// The program decides it, not a name: an import is missing when the compiler could not resolve it, and a
// reference when no file is at its path. A side-effect import is never reported by the checker unless
// noUncheckedSideEffectImports is on, so the type errors alone cannot say a generated file is missing.
// generated keeps the answer to output a build writes (a gitignored path), since a missing source file is a
// mistake in the source, which its own type error already names.
func (graph *Graph) MissingGeneratedInputs(generated func(target string) bool) []MissingInput {
	fileSystem := graph.Program.Host().FS()
	exists := func(target string, extensions ...string) bool {
		for _, extension := range append([]string{""}, extensions...) {
			if fileSystem.FileExists(tspath.RootedFilePath(target + extension)) {
				return true
			}
		}
		return false
	}

	var missing []MissingInput
	add := func(sourceFile *ast.SourceFile, position int, specifier string, target string) {
		line, character := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, position)
		missing = append(missing, MissingInput{
			Importer:  sourceFile.FileName().AsString(),
			Line:      line + 1,
			Column:    character + 1,
			Specifier: specifier,
			Target:    target,
		})
	}

	for _, sourceFile := range graph.ProjectFiles() {
		directory := tspath.RootedDirectoryPath(tspath.GetDirectoryPath(sourceFile.FileName().AsString()))
		for _, specifier := range sourceFile.Imports() {
			name := specifier.Text()
			if !tspath.IsExternalModuleNameRelative(name) {
				continue
			}
			if resolved := graph.Program.GetResolvedModuleFromModuleSpecifier(
				sourceFile,
				specifier,
			); resolved != nil && resolved.IsResolved() {
				continue
			}
			target := tspath.GetNormalizedAbsolutePath(name, directory)
			if !exists(target) && generated(target) {
				add(sourceFile, scanner.SkipTrivia(sourceFile.Text(), specifier.Pos()), name, target)
			}
		}
		for _, reference := range sourceFile.ReferencedFiles {
			target := tspath.GetNormalizedAbsolutePath(reference.FileName, directory)
			// A reference path may leave its extension off, as the compiler's own lookup does.
			if !exists(target, ".ts", ".tsx", ".d.ts") && generated(target) {
				add(sourceFile, reference.Pos(), reference.FileName, target)
			}
		}
	}

	sort.SliceStable(missing, func(left, right int) bool {
		if missing[left].Importer != missing[right].Importer {
			return missing[left].Importer < missing[right].Importer
		}
		if missing[left].Line != missing[right].Line {
			return missing[left].Line < missing[right].Line
		}
		return missing[left].Column < missing[right].Column
	})
	return missing
}
