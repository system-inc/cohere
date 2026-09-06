package structure

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/react"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var messageNoSeparateNamedExport = rule.Message{
	Id: "noSeparateNamedExport",
	Description: "This exports a component declared in the same file from a separate statement. " +
		"Put the export on the declaration instead. A reader opening the file to find what it " +
		"offers should see that on the function, not have to hold the whole file in mind and check " +
		"a list at the bottom, and the list is a second place a rename has to reach: renaming the " +
		"function and forgetting the export is a build error, renaming it in both and forgetting a " +
		"consumer is not.",
}

// ReactComponentNoSeparateNamedExport flags a bare export list naming a component the file declares.
//
//	valid:   export function Button() { ... }
//	valid:   export { Button } from './Button'          a re-export, not this file's declaration
//	invalid: function Button() { ... } export { Button }
//
// A re-export is deliberately exempt and it is the shape this codebase actually uses: an index file
// gathering components from elsewhere has nothing to move the export onto, since the declaration is
// in another file.
//
// Only components. A file exporting a type or a constant from a list at the bottom is a different
// convention question that this rule has no opinion on, and reporting it would make the rule about
// export style in general rather than about where a component announces itself.
//
// Fixable by deleting the statement, which is safe precisely because the components it names are
// declared in this file: removing the list without adding `export` to the declarations would break
// the build loudly rather than silently, and the fixer's own convergence check catches that.
var ReactComponentNoSeparateNamedExport = rule.Rule{
	Name: "structure/react-component-no-separate-named-export",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if !FileContextFor(ctx.SourceFile.FileName()).IsReactFile {
			return nil
		}

		declaredComponents := componentNamesDeclaredIn(ctx.SourceFile.AsNode())
		if len(declaredComponents) == 0 {
			return nil
		}

		return rule.Listeners{
			ast.KindExportDeclaration: func(node *ast.Node) {
				declaration := node.AsExportDeclaration()

				// A re-export has somewhere else to be, so there is nothing to move the export
				// onto. This is the shape an index file uses and it must stay silent.
				if declaration.ModuleSpecifier != nil {
					return
				}
				// The kind test cannot fail in practice and is kept for shape. The only clause that
				// is not a named-exports list is a namespace one (`export * as X from '...'`), and
				// every namespace clause carries a module specifier, which the re-export guard
				// above has already returned on. So no fixture can kill a mutant that drops it.
				//
				// Kept rather than reduced to the nil check, because the reason it is unreachable
				// lives four lines away and a reader would have to reconstruct it. Recorded so a
				// future sweep does not go looking for a fixture that cannot exist.
				if declaration.ExportClause == nil || declaration.ExportClause.Kind != ast.KindNamedExports {
					return
				}

				for _, element := range declaration.ExportClause.AsNamedExports().Elements.Nodes {
					name := element.AsExportSpecifier().Name()
					if name == nil || name.Kind != ast.KindIdentifier {
						continue
					}
					if declaredComponents[name.Text()] {
						// Reported once per statement, not per specifier. The repair is to delete
						// the statement, which is one edit however many names it holds.
						ctx.ReportNodeWithFixes(node, messageNoSeparateNamedExport, ctx.RemoveNode(node))
						return
					}
				}
			},
		}
	},
}

// componentNamesDeclaredIn collects the component-shaped declarations a file makes.
//
// Only top-level declarations, since a component nested inside another function is not part of the
// file's exported surface and could not be named in an export list anyway.
func componentNamesDeclaredIn(sourceFile *ast.Node) map[string]bool {
	declared := map[string]bool{}

	sourceFile.ForEachChild(func(statement *ast.Node) bool {
		switch statement.Kind {
		case ast.KindFunctionDeclaration:
			name := statement.AsFunctionDeclaration().Name()
			if name != nil && react.IsLikelyComponentName(name.Text()) && HasJsxOrReactHookCalls(statement) {
				declared[name.Text()] = true
			}

		case ast.KindVariableStatement:
			declarationList := statement.AsVariableStatement().DeclarationList
			if declarationList == nil {
				return false
			}
			for _, declarationNode := range declarationList.AsVariableDeclarationList().Declarations.Nodes {
				declaration := declarationNode.AsVariableDeclaration()
				name := declaration.Name()
				if name == nil || name.Kind != ast.KindIdentifier || !react.IsLikelyComponentName(name.Text()) {
					continue
				}
				// The initializer is what has to look like a component, not the statement: a
				// capitalized constant holding a string is not a component.
				if declaration.Initializer != nil && HasJsxOrReactHookCalls(declaration.Initializer) {
					declared[name.Text()] = true
				}
			}
		}
		return false
	})

	return declared
}
