package program

import (
	"sort"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/microsoft/TypeScript/tsc/shim/tsoptions"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// MissingInputKind is why an input counts as missing, which decides what its line says and advises.
type MissingInputKind int

const (
	// MissingGenerated is generated output nobody has written yet: an import or a `/// <reference path>`
	// whose target does not exist and git ignores (#wxsqavy).
	MissingGenerated MissingInputKind = iota
	// UnresolvedImport is an import, relative or a package, that the compiler resolved to nothing and no
	// ambient module declares (#4jxsbzr).
	UnresolvedImport
	// UnresolvedReference is a `/// <reference path>` to a file that does not exist and is not generated.
	UnresolvedReference
	// UnresolvedTypes is a compilerOptions.types entry or a `/// <reference types>` that resolves to no type
	// library (#4jxsbzr).
	UnresolvedTypes
)

// MissingInput is an input the program was told to load and could not: an import, a `/// <reference>` or
// a compilerOptions.types entry whose target it could not find.
type MissingInput struct {
	Kind MissingInputKind
	// Importer is the file that asks for it, the tsconfig for a types entry, and Line and Column (one-based)
	// where.
	Importer string
	Line     int
	Column   int
	// Specifier is the name as the importer wrote it, and Target the absolute path it names, or the name
	// itself when it names a package or a type library, which has no path until it resolves.
	Specifier string
	Target    string
}

// MissingInputs is every input the project's files and its tsconfig ask for that the program could not
// load, each with its kind. Ordered by importer, then position, then name.
//
// Next.js is the case that asked first (#wxsqavy): next-env.d.ts imports .next/types/routes.d.ts, which
// holds the route globals (PageProps, LayoutProps, RouteContext), and a fresh checkout has no .next until
// `next typegen` writes it. Every file that names one of those globals then fails, and the run reads as
// dozens of separate type errors with one cause. generated keeps that kind to output a build writes (a
// gitignored path), since its advice is to run the build.
//
// Loom's wire project asked for the rest (#4jxsbzr): with no node_modules, its tsconfig's types entries
// and its package imports resolved to nothing, the checker reported 2,363 TS2304, and a fix still renamed
// identifiers in two files, reading names that never loaded. A package, a type library or a source file
// that does not load is the same hazard as a generated file that was never written.
//
// The program decides it, never a list of names: an import is missing when the compiler could not resolve
// it and no ambient module declares it, a reference path when no file is at its path, and a type reference
// when the compiler resolved it to nothing. A side-effect import the checker leaves unchecked
// (noUncheckedSideEffectImports set to false) asks the compiler for nothing, so it counts only when it is
// generated, where the type errors alone could never say what is missing.
func (graph *Graph) MissingInputs(generated func(target string) bool) []MissingInput {
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
	add := func(sourceFile *ast.SourceFile, position int, kind MissingInputKind, specifier string, target string) {
		line, character := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, position)
		missing = append(missing, MissingInput{
			Kind:      kind,
			Importer:  sourceFile.FileName().AsString(),
			Line:      line + 1,
			Column:    character + 1,
			Specifier: specifier,
			Target:    target,
		})
	}

	declared := graph.ambientModuleDeclarations()
	checksSideEffects := graph.Program.Options().NoUncheckedSideEffectImports.IsTrueOrUnknown()
	for _, sourceFile := range graph.ProjectFiles() {
		directory := tspath.RootedDirectoryPath(tspath.GetDirectoryPath(sourceFile.FileName().AsString()))
		for _, specifier := range sourceFile.Imports() {
			// A source phase import is never resolved as a module, so the program asked for nothing here.
			if ast.IsSourcePhaseImport(specifier.Parent) {
				continue
			}
			name := specifier.Text()
			if resolved := graph.Program.GetResolvedModuleFromModuleSpecifier(
				sourceFile,
				specifier,
			); resolved != nil && resolved.IsResolved() {
				continue
			}
			position := scanner.SkipTrivia(sourceFile.Text(), specifier.Pos())
			relative := tspath.IsExternalModuleNameRelative(name)
			target := name
			if relative {
				target = tspath.GetNormalizedAbsolutePath(name, directory)
				if !exists(target) && generated(target) {
					add(sourceFile, position, MissingGenerated, name, target)
					continue
				}
			}
			sideEffect := ast.IsImportDeclaration(specifier.Parent) && specifier.Parent.ImportClause() == nil
			if (sideEffect && !checksSideEffects) || declared.answers(name, relative) {
				continue
			}
			add(sourceFile, position, UnresolvedImport, name, target)
		}
		for _, reference := range sourceFile.ReferencedFiles {
			target := tspath.GetNormalizedAbsolutePath(reference.FileName, directory)
			// A reference path may leave its extension off, as the compiler's own lookup does.
			if exists(target, ".ts", ".tsx", ".d.ts") {
				continue
			}
			kind := UnresolvedReference
			if generated(target) {
				kind = MissingGenerated
			}
			add(sourceFile, reference.Pos(), kind, reference.FileName, target)
		}
		for _, reference := range sourceFile.TypeReferenceDirectives {
			if resolved := graph.Program.GetResolvedTypeReferenceDirectiveFromTypeReferenceDirective(
				reference,
				sourceFile,
			); resolved != nil && resolved.IsResolved() {
				continue
			}
			add(sourceFile, reference.Pos(), UnresolvedTypes, reference.FileName, reference.FileName)
		}
	}

	// The compiler resolves the tsconfig's types entries as references from a file of its own beside the
	// tsconfig, one the program never holds, so that file is the one key here with no source file behind it.
	// Every other key is a file the program loaded and holds.
	configurationFile := graph.Program.CommandLine().ConfigFile
	if configurationFile != nil && configurationFile.SourceFile != nil {
		for path, resolutions := range graph.Program.GetResolvedTypeReferenceDirectives() {
			if graph.Program.GetSourceFileByPath(path) != nil {
				continue
			}
			for key, resolved := range resolutions {
				if resolved != nil && resolved.IsResolved() {
					continue
				}
				add(
					configurationFile.SourceFile,
					typesEntryPosition(configurationFile.SourceFile, key.Name),
					UnresolvedTypes,
					key.Name,
					key.Name,
				)
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
		if missing[left].Column != missing[right].Column {
			return missing[left].Column < missing[right].Column
		}
		return missing[left].Specifier < missing[right].Specifier
	})
	return missing
}

// ambientModuleDeclarations is every ambient module the program declares, from every file it holds, since
// one in node_modules (`declare module "node:fs"` in @types/node) answers an import as well as one here.
func (graph *Graph) ambientModuleDeclarations() ambientModules {
	declared := ambientModules{names: map[string]bool{}}
	for _, sourceFile := range graph.Program.GetSourceFiles() {
		for _, name := range sourceFile.AmbientModuleNames {
			pattern := core.TryParsePattern(name)
			switch {
			case !pattern.IsValid():
			case pattern.StarIndex >= 0:
				declared.patterns = append(declared.patterns, pattern)
			default:
				declared.names[name] = true
			}
		}
	}
	return declared
}

// typesEntryPosition is where the tsconfig names a types entry: the entry's string in its compilerOptions,
// or the file's start when a tsconfig it extends names it, since only the tsconfig itself is parsed here.
func typesEntryPosition(configurationFile *ast.SourceFile, name string) int {
	if configurationFile == nil || configurationFile.Statements == nil || len(configurationFile.Statements.Nodes) == 0 {
		return 0
	}
	root := configurationFile.Statements.Nodes[0].Expression()
	if root == nil || !ast.IsObjectLiteralExpression(root) {
		return 0
	}
	for _, property := range root.AsObjectLiteralExpression().Properties.Nodes {
		if !ast.IsPropertyAssignment(property) || !ast.IsObjectLiteralExpression(property.Initializer()) {
			continue
		}
		if propertyName, named := ast.TryGetTextOfPropertyName(property.Name()); !named ||
			propertyName != "compilerOptions" {
			continue
		}
		if entry := tsoptions.GetOptionsSyntaxByArrayElementValue(
			property.Initializer().AsObjectLiteralExpression(),
			"types",
			name,
		); entry != nil {
			return scanner.SkipTrivia(configurationFile.Text(), entry.Pos())
		}
	}
	return 0
}
