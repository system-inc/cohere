// Package consistency_no_redundant_import_alias is the Go engine's rule that an import is not renamed to the name it
// already has.
//
// It is the Go form of ESLint core's no-useless-rename. Go has no destructuring, so the rename Go writes is the
// import alias, and `import fmt "fmt"` binds exactly the name `import "fmt"` binds. The alias reads as a decision,
// so a reader stops to look for the clash it avoids and finds none.
//
// The name an import binds is the package clause of the package it imports, which the rule reads from types and never
// guesses from the path. An alias that equals that name is redundant to the compiler, but it still tells a reader
// something when the path's last element does not spell the name: `yaml "gopkg.in/yaml.v3"` and `pgx
// "github.com/jackc/pgx/v5"` say which name the path hides, as goimports writes them. So the rule flags only an alias
// that both the package's name and the path's last element already say, where dropping it loses nothing.
package consistency_no_redundant_import_alias

import (
	"go/ast"
	"go/types"
	"path"
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-redundant-import-alias"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_redundant_import_alias",
	Doc: "Flags an import alias equal to the imported package's own name when the import path's last element " +
		"spells that name too, as in import fmt \"fmt\", and offers to drop it, because the alias renames nothing. " +
		"Blank and dot imports are not judged, nor is an alias that names a package its path does not spell.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads type information for the name in the imported
// package's own package clause.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var redundantImportAliasMessage = policy.MessageOf(Name, "redundantImportAlias")

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		for _, specification := range file.Imports {
			if isRedundantAlias(pass, specification) {
				report(pass, file, specification)
			}
		}
	}
	return nil, nil
}

// isRedundantAlias is whether specification renames its package to the name the package already has, spelled by its
// path's last element as well, so the alias adds nothing a reader of the path would not already know.
func isRedundantAlias(pass *analysis.Pass, specification *ast.ImportSpec) bool {
	// No alias binds the package's own name, and a blank or dot import is no rename: it binds nothing, or every
	// exported name.
	if specification.Name == nil || specification.Name.Name == "_" || specification.Name.Name == "." {
		return false
	}
	// The checker defines a renamed import's alias as a *types.PkgName, whose Imported is the package the path
	// resolved to, with the name its package clause declares.
	packageName, isPackageName := pass.TypesInfo.Defs[specification.Name].(*types.PkgName)
	if !isPackageName || specification.Name.Name != packageName.Imported().Name() {
		return false
	}
	importPath, unquoteError := strconv.Unquote(specification.Path.Value)
	if unquoteError != nil {
		// The parser accepts only a valid string literal as a path, so this is never reached; skipping it is quieter
		// than guessing.
		return false
	}
	// The path as written, not the package's resolved path, since the written one is what the reader sees. Import
	// paths always use slashes, so path, not filepath, finds the last element.
	return path.Base(importPath) == specification.Name.Name
}

// report flags the alias and offers the import without it. The fix removes the alias and the space up to the path, or
// up to a comment written between them, so the comment survives where the writer put it.
func report(pass *analysis.Pass, file *ast.File, specification *ast.ImportSpec) {
	end := specification.Path.Pos()
	for _, group := range file.Comments {
		if group.Pos() > specification.Name.End() && group.Pos() < end {
			end = group.Pos()
		}
	}
	pass.Report(analysis.Diagnostic{
		Pos:      specification.Name.Pos(),
		End:      specification.Name.End(),
		Category: redundantImportAliasMessage.Id,
		Message: redundantImportAliasMessage.Render(map[string]string{
			"alias":      specification.Name.Name,
			"importPath": specification.Path.Value,
		}),
		SuggestedFixes: []analysis.SuggestedFix{{
			Message: "Drop the alias " + specification.Name.Name,
			TextEdits: []analysis.TextEdit{{
				Pos:     specification.Name.Pos(),
				End:     end,
				NewText: []byte{},
			}},
		}},
	})
}
