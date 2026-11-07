package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/system-inc/cohere/internal/go_rename/rename"

	"golang.org/x/tools/go/packages"
)

// counts are what a plan changes, by kind.
type counts struct {
	PackageClauses int
	PackageDocs    int
	ImportPaths    int
	AliasesDropped int
	AliasesKept    int
	Qualifiers     int
	LocalsRenamed  int
	SyntacticFiles int
}

// plan is everything the moves change in Go source, before anything is written.
type plan struct {
	edits []rename.Edit
	// names are the identifiers an edit gives a new name, by the file as loaded and offset: package clauses,
	// qualifiers and renamed locals.
	names map[rename.EditKey]string
	// renamedObjects are the locals renamed out of a new package name's way, with their new names.
	renamedObjects map[types.Object]string
	// droppedAliases are the alias identifiers an edit removes, by the file as loaded and offset.
	droppedAliases map[rename.EditKey]bool
	counts         counts
	refusals       []string
	notes          []string
}

func newPlan() *plan {
	return &plan{
		names:          map[rename.EditKey]string{},
		renamedObjects: map[types.Object]string{},
		droppedAliases: map[rename.EditKey]bool{},
	}
}

// typedFile is one file of a type-checked package, with what the package's types say about it.
type typedFile struct {
	goPackage *packages.Package
	file      *ast.File
	filename  string
	// relativeFolder is the file's folder relative to the root.
	relativeFolder string
}

// planTypedFile adds the edits one type-checked file needs: its package clause and package doc when its folder is
// moved, every import of a moved folder's path, and every qualifier of an import whose package takes a new name,
// with any local standing in that name's way renamed. It returns the locals to rename, which the caller checks
// with the rest of the package's.
func (plan *plan) planTypedFile(mapping *mapping, sources fileSource, typed typedFile) []rename.Proposal {
	contents, readError := sources.read(typed.filename)
	if readError != nil {
		plan.refusals = append(plan.refusals, fmt.Sprintf("%s: %v", typed.filename, readError))
		return nil
	}
	fileSet := typed.goPackage.Fset
	offset := func(position token.Pos) int { return fileSet.PositionFor(position, false).Offset }
	plan.planClause(mapping, typed.relativeFolder, typed.filename, typed.file, offset)

	var proposals []rename.Proposal
	for _, spec := range typed.file.Imports {
		importPath, unquoteError := strconv.Unquote(spec.Path.Value)
		if unquoteError != nil {
			continue
		}
		newPath := mapping.NewImportPath(importPath)
		if newPath == importPath {
			continue
		}
		plan.add(typed.filename, offset(spec.Path.Pos()), spec.Path.Value, strconv.Quote(newPath))
		plan.counts.ImportPaths++

		packageName := importedName(typed.goPackage.TypesInfo, spec)
		if packageName == nil {
			plan.refusals = append(
				plan.refusals,
				fmt.Sprintf("%s: the import of %s declares no name the types know", typed.filename, importPath),
			)
			continue
		}
		oldName := packageName.Imported().Name()
		newName := mapping.ImportedPackageName(importPath, oldName)
		if newName == oldName || spec.Name != nil && (spec.Name.Name == "_" || spec.Name.Name == ".") {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name != oldName {
				plan.counts.AliasesKept++
				continue
			}
			plan.add(
				typed.filename,
				offset(spec.Name.Pos()),
				string(contents[offset(spec.Name.Pos()):offset(spec.Path.Pos())]),
				"",
			)
			plan.droppedAliases[rename.EditKey{Filename: typed.filename, Offset: offset(spec.Name.Pos())}] = true
			plan.counts.AliasesDropped++
		}
		proposals = append(proposals, plan.planQualifiers(typed, packageName, newName, offset)...)
	}
	return proposals
}

// planClause renames the package clause, and the package doc's name for it, of a file in a moved folder of its
// own.
func (plan *plan) planClause(
	mapping *mapping,
	relativeFolder string,
	filename string,
	file *ast.File,
	offset func(token.Pos) int,
) {
	newName, nameError := mapping.NewPackageName(relativeFolder, file.Name.Name)
	if nameError != nil {
		plan.refusals = append(plan.refusals, fmt.Sprintf("%s: %v", filename, nameError))
		return
	}
	if newName == file.Name.Name {
		return
	}
	plan.add(filename, offset(file.Name.Pos()), file.Name.Name, newName)
	plan.names[rename.EditKey{Filename: filename, Offset: offset(file.Name.Pos())}] = newName
	plan.counts.PackageClauses++
	if file.Doc == nil {
		return
	}
	first := file.Doc.List[0]
	text, isLineComment := strings.CutPrefix(first.Text, "// Package ")
	if !isLineComment || !strings.HasPrefix(text, file.Name.Name) {
		return
	}
	rest := text[len(file.Name.Name):]
	if rest != "" && (unicode.IsLetter(rune(rest[0])) || unicode.IsDigit(rune(rest[0])) || rest[0] == '_') {
		return
	}
	plan.add(filename, offset(first.Pos())+len("// Package "), file.Name.Name, newName)
	plan.counts.PackageDocs++
}

// planQualifiers renames every qualifier of packageName in the file to newName, and proposes renaming any local
// that would capture one of them, to the new name's words in camel case. Anything else standing in the way is
// refused.
func (plan *plan) planQualifiers(
	typed typedFile,
	packageName *types.PkgName,
	newName string,
	offset func(token.Pos) int,
) []rename.Proposal {
	info := typed.goPackage.TypesInfo
	var proposals []rename.Proposal
	proposed := map[types.Object]bool{}
	if existing := typed.goPackage.Types.Scope().Lookup(newName); existing != nil {
		plan.refusals = append(
			plan.refusals,
			fmt.Sprintf("%s: %s is declared at package level, in the way of the import's new name",
				typed.goPackage.Fset.Position(existing.Pos()), newName),
		)
	}
	ast.Inspect(typed.file, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier || info.Uses[identifier] != packageName {
			return true
		}
		plan.add(typed.filename, offset(identifier.Pos()), identifier.Name, newName)
		plan.names[rename.EditKey{Filename: typed.filename, Offset: offset(identifier.Pos())}] = newName
		plan.counts.Qualifiers++
		scope := typed.goPackage.Types.Scope().Innermost(identifier.Pos())
		if scope == nil {
			plan.refusals = append(
				plan.refusals,
				fmt.Sprintf("%s: no scope holds this qualifier", typed.goPackage.Fset.Position(identifier.Pos())),
			)
			return true
		}
		_, standing := scope.LookupParent(newName, identifier.Pos())
		if standing == nil || proposed[standing] {
			return true
		}
		if !rename.IsLocal(standing, typed.goPackage.Types) {
			if standing.Parent() != typed.goPackage.Types.Scope() {
				plan.refusals = append(plan.refusals, fmt.Sprintf(
					"%s: %s is declared there and is not a local variable, so it cannot be renamed out of the import's way",
					typed.goPackage.Fset.Position(standing.Pos()),
					newName,
				))
			}
			return true
		}
		proposed[standing] = true
		proposals = append(proposals, rename.Proposal{Object: standing, NewName: camelCase(newName)})
		return true
	})
	return proposals
}

// importedName is the name an import spec declares in its file.
func importedName(info *types.Info, spec *ast.ImportSpec) *types.PkgName {
	if spec.Name != nil {
		packageName, _ := info.Defs[spec.Name].(*types.PkgName)
		return packageName
	}
	packageName, _ := info.Implicits[spec].(*types.PkgName)
	return packageName
}

// camelCase joins a package name's underscore-separated words as a local's name: go_engine reads goEngine.
func camelCase(name string) string {
	words := strings.Split(name, "_")
	for index := 1; index < len(words); index++ {
		if words[index] != "" {
			words[index] = strings.ToUpper(words[index][:1]) + words[index][1:]
		}
	}
	return strings.Join(words, "")
}

// add records one edit.
func (plan *plan) add(filename string, offset int, oldText string, newText string) {
	plan.edits = append(plan.edits, rename.Edit{Filename: filename, Offset: offset, OldText: oldText, NewText: newText})
}

// relativeFolderOf is filename's folder relative to root, and whether it is under it.
func relativeFolderOf(root string, filename string) (string, bool) {
	relative, relativeError := filepath.Rel(root, filepath.Dir(filename))
	if relativeError != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", false
	}
	return filepath.ToSlash(relative), true
}
