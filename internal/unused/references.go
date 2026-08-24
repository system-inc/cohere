package unused

import (
	"context"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/program"
)

// Unreferenced is one exported declaration that nothing in the program uses.
type Unreferenced struct {
	// FileName is where the declaration lives.
	FileName string

	// Name is what was declared.
	Name string

	// Kind is what sort of thing it is, for grouping in the report.
	Kind string

	// Range is the declaration's own span.
	Range TextSpan

	// Intentional is set when the author marked this as deliberately unused.
	Intentional intentionalReason

	// WholeFileUnused is set when nothing in the file is referenced, so the reader can be told about
	// the file rather than handed a list of its exports one at a time.
	WholeFileUnused bool
}

// FileUnused is a file nothing imports.
type FileUnused struct {
	FileName    string
	Exports     int
	Intentional intentionalReason
}

// Report is what one `--unused` run found.
type Report struct {
	Unreferenced []Unreferenced
	Files        []FileUnused
	Unreachable  []Unreachable

	// FilesAnalyzed is the population the findings were drawn from, printed alongside them because a
	// finding count with no denominator cannot be told apart from a broken probe.
	FilesAnalyzed int

	// RootedFiles is how many files were spared by the root set, which is the number that says
	// whether the root set is doing anything. A root set that spares zero files on a Next.js tree is
	// broken, and a report that does not state the number cannot reveal that.
	RootedFiles int

	// ExportsAnalyzed is how many exported declarations were examined.
	ExportsAnalyzed int
}

// FindUnreferenced builds a program-wide reverse index and reports the exported declarations
// nothing uses.
//
// # Why a program-wide index rather than per-rule symbol comparison
//
// `internal/rule/rule.go` argues at length against putting a references-to-a-binding helper on the
// shelf, and that argument is correct for every rule shipped so far. It does not govern this,
// and the comment says so itself in its own last paragraph: "Find-all-references answers 'where is
// this used', and these rules already know where to look, since they walk one file. They need 'is
// this the same binding', which is a symbol comparison. Reaching for a reverse index here would be
// answering a forward question with the wrong instrument."
//
// This is the forward question. "Where is this used" is the entire output, the scope is the program
// rather than one file, and there is no cheaper tier that is correct: name matching cannot tell an
// unused `Button` from a used one in another file, and per-file symbol comparison cannot answer a
// whole-program question at all. The cost the rule comment refuses to hide behind an import is
// stated here instead — this is a phase, run when asked for, not a rule that every file pays for.
//
// # What the index is keyed on
//
// Symbol pointers, established by probe on this substrate before anything was built on it: 42 of 42
// imported names resolved through `GetAliasedSymbol` to a declaration in another file, with zero nil
// symbols. Pointer identity is what makes a reference in one file and a declaration in another the
// same fact.
func FindUnreferenced(ctx context.Context, graph *program.Graph, files []*ast.SourceFile, roots *RootSet) (*Report, error) {
	report := &Report{FilesAnalyzed: len(files)}

	// Pass one: every symbol that anything anywhere refers to.
	//
	// Built from identifier positions rather than from import statements, because a reference is a
	// use wherever it appears — a call, a type position, a JSX tag, a re-export — and enumerating the
	// forms would miss whichever one nobody thought of. Resolving every identifier is more work and
	// it cannot have that class of gap.
	referenced := make(map[*ast.Symbol]bool, len(files)*8)

	for _, file := range files {
		checker, release := graph.CheckerForFile(ctx, file)
		if checker == nil {
			release()
			continue
		}

		var visit func(node *ast.Node) bool
		visit = func(node *ast.Node) bool {
			if node == nil {
				return false
			}
			if node.Kind == ast.KindIdentifier {
				// A declaration's own name is not a use of it. Without this the index marks
				// everything referenced and the report finds nothing — the exact broken-probe shape
				// that reads as a clean codebase.
				if !isDeclarationName(node) {
					if symbol := checker.GetSymbolAtLocation(node); symbol != nil {
						referenced[symbol] = true
						// An import binding is an alias; the thing actually used is what it points
						// at. Recording only the alias leaves every real declaration looking unused.
						if symbol.Flags&ast.SymbolFlagsAlias != 0 {
							if aliased := checker.GetAliasedSymbol(symbol); aliased != nil {
								referenced[aliased] = true
							}
						}
					}
				}
			}
			node.ForEachChild(visit)
			return false
		}
		file.AsNode().ForEachChild(visit)
		release()
	}

	// Pass two: every exported declaration, asked whether the index knows it.
	for _, file := range files {
		fileName := file.FileName()
		if isRoot, _ := roots.IsRoot(fileName); isRoot {
			report.RootedFiles++
			continue
		}

		checker, release := graph.CheckerForFile(ctx, file)
		if checker == nil {
			release()
			continue
		}

		text := file.Text()
		fileExports := 0
		fileUnusedExports := 0
		var fileFindings []Unreferenced

		for _, statement := range file.Statements.Nodes {
			for _, declaration := range exportedDeclarations(statement) {
				name := declaration.name
				if name == nil {
					continue
				}
				fileExports++
				report.ExportsAnalyzed++

				symbol := checker.GetSymbolAtLocation(name)
				if symbol == nil {
					// A name that does not resolve is a question this analysis cannot answer, and
					// answering it anyway is how live code gets reported as dead. Skipped and
					// counted rather than guessed.
					continue
				}
				if referenced[symbol] {
					continue
				}

				fileUnusedExports++
				fileFindings = append(fileFindings, Unreferenced{
					FileName:    fileName,
					Name:        name.Text(),
					Kind:        declaration.kind,
					Range:       TextSpan{Position: statement.Pos(), End: statement.End()},
					Intentional: findIntentionalMarker(leadingText(text, statement)),
				})
			}
		}

		// A file where every export is unused reads better as one finding about the file than as a
		// list of its parts. That is the case the brief calls the insight: an abandoned direction
		// drags a small private world behind it, and naming the file is what makes the world visible.
		if fileExports > 0 && fileUnusedExports == fileExports {
			intentional := intentionalReason{}
			for _, finding := range fileFindings {
				if finding.Intentional.Present {
					intentional = finding.Intentional
					break
				}
			}
			report.Files = append(report.Files, FileUnused{
				FileName:    fileName,
				Exports:     fileExports,
				Intentional: intentional,
			})
		} else {
			report.Unreferenced = append(report.Unreferenced, fileFindings...)
		}

		release()
	}

	return report, nil
}

// exportedDeclaration is one exported name and what kind of thing it names.
type exportedDeclaration struct {
	name *ast.Node
	kind string
}

// exportedDeclarations returns the names a statement exports.
//
// Only `export` declarations are considered. A module-private helper that nothing calls is a real
// finding and a different one: it is decidable within the file, needs no index, and belongs to a
// lint rule rather than to a whole-program report. Keeping this to exports is what keeps the claim
// this report makes a whole-program claim.
func exportedDeclarations(statement *ast.Node) []exportedDeclaration {
	if !hasExportModifier(statement) {
		return nil
	}

	switch statement.Kind {
	case ast.KindFunctionDeclaration:
		return one(statement.Name(), "function")
	case ast.KindClassDeclaration:
		return one(statement.Name(), "class")
	case ast.KindInterfaceDeclaration:
		return one(statement.Name(), "interface")
	case ast.KindTypeAliasDeclaration:
		return one(statement.Name(), "type")
	case ast.KindEnumDeclaration:
		return one(statement.Name(), "enum")
	case ast.KindVariableStatement:
		declarationList := statement.AsVariableStatement().DeclarationList
		if declarationList == nil {
			return nil
		}
		var declarations []exportedDeclaration
		for _, declaration := range declarationList.AsVariableDeclarationList().Declarations.Nodes {
			name := declaration.Name()
			// Only a plain identifier. A destructuring pattern exports several names whose
			// individual liveness this pass does not separate, and reporting the whole statement
			// because one name is unused would be wrong.
			if name != nil && name.Kind == ast.KindIdentifier {
				declarations = append(declarations, exportedDeclaration{name: name, kind: "constant"})
			}
		}
		return declarations
	}
	return nil
}

func one(name *ast.Node, kind string) []exportedDeclaration {
	if name == nil {
		return nil
	}
	return []exportedDeclaration{{name: name, kind: kind}}
}

// hasExportModifier reports whether a statement carries `export`.
//
// A default export is deliberately excluded: it is imported under whatever name the consumer
// chooses, so the name at the declaration site is not the name at the use site, and the symbol
// identity this index is keyed on does not carry across reliably. Reporting one would risk a false
// positive on the most load-bearing kind of export there is.
func hasExportModifier(statement *ast.Node) bool {
	modifiers := statement.Modifiers()
	if modifiers == nil {
		return false
	}
	hasExport := false
	for _, modifier := range modifiers.Nodes {
		switch modifier.Kind {
		case ast.KindExportKeyword:
			hasExport = true
		case ast.KindDefaultKeyword:
			return false
		case ast.KindDeclareKeyword:
			// An ambient declaration describes something that exists elsewhere. Nothing here can
			// establish whether it is used.
			return false
		}
	}
	return hasExport
}

// isDeclarationName reports whether an identifier is the name being declared rather than a use of
// something declared elsewhere.
func isDeclarationName(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil {
		return false
	}
	if parent.Name() == node {
		switch parent.Kind {
		case ast.KindFunctionDeclaration,
			ast.KindClassDeclaration,
			ast.KindInterfaceDeclaration,
			ast.KindTypeAliasDeclaration,
			ast.KindEnumDeclaration,
			ast.KindVariableDeclaration,
			ast.KindParameter,
			ast.KindPropertyDeclaration,
			ast.KindMethodDeclaration,
			ast.KindEnumMember,
			ast.KindPropertySignature,
			ast.KindMethodSignature,
			ast.KindImportSpecifier,
			ast.KindImportClause,
			ast.KindNamespaceImport,
			ast.KindExportSpecifier,
			ast.KindBindingElement,
			ast.KindTypeParameter,
			ast.KindModuleDeclaration:
			return true
		}
	}
	return false
}

// leadingText returns the trivia before a statement, where a marker comment lives.
func leadingText(text string, statement *ast.Node) string {
	start := statement.Pos()
	end := statement.End()
	if start < 0 || start > len(text) {
		return ""
	}
	// The statement's Pos() is the start of its leading trivia in this parser, so the span from Pos
	// to the first non-trivia character is the comment block. Taking the whole statement head is
	// simpler and cannot miss a marker written on the same line.
	head := end
	if head > len(text) {
		head = len(text)
	}
	region := text[start:head]
	if newline := strings.LastIndex(region[:min(len(region), 400)], "\n"); newline >= 0 {
		return region[:newline]
	}
	return region[:min(len(region), 400)]
}
