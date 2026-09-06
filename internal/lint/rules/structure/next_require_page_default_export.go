package structure

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/module"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var messagePageRequireDefaultExport = rule.Message{
	Id: "pageRequireDefaultExport",
	Description: "A Next.js page file has no default export, so the route it defines does not " +
		"exist. Next resolves a route by this export and by nothing else, so the failure is a " +
		"missing page at runtime rather than anything the compiler reports: every other export in " +
		"the file type-checks fine. Add a default-exported function ending in PageRoute.",
}

var messagePageDefaultExportInline = rule.Message{
	Id: "pageDefaultExportInline",
	Description: "This page's default export is a separate statement rather than being on the " +
		"function itself. Writing `export default function NameOfPageRoute()` puts the route's " +
		"identity on the declaration, where a reader opening the file sees it first, instead of " +
		"in a line at the bottom that a refactor can leave pointing at the wrong function.",
}

var messagePageDefaultExportNameSuffix = rule.Message{
	Id: "pageDefaultExportNameSuffix",
	Description: "A page's default export must be a named function ending in PageRoute. Every " +
		"page in the app is a file called page.tsx, so the function name is the only thing that " +
		"distinguishes one in a stack trace, a profiler flame graph, or the React tree.",
}

// NextRequirePageDefaultExport flags a Next.js page whose default export is missing or misshapen.
//
//	valid:   export default function AccountSettingsPageRoute() { ... }
//	invalid: (a page.tsx with no default export at all)
//	invalid: function AccountSettingsPageRoute() { ... } export default AccountSettingsPageRoute
//	invalid: export default function AccountSettings() { ... }
//
// Three message ids for three repairs: adding an export, moving it onto the declaration, and
// renaming the function.
//
// Only page.tsx and page.jsx. The convention is about the file Next resolves a route from, and a
// layout or an error boundary has its own contract that this one would misdescribe.
//
// The rule applies to any page file, including one with a default export that is not a function at
// all, which is why the missing-export check runs at the file rather than per export.
var NextRequirePageDefaultExport = rule.Rule{
	Name: "structure/next-require-page-default-export",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		fileContext := FileContextFor(ctx.SourceFile.FileName())
		if !fileContext.IsReactFile || !fileContext.IsPageFile {
			return nil
		}

		sourceFile := ctx.SourceFile.AsNode()
		hasDefaultExport := false

		// The whole file is walked once here rather than through a listener, because the
		// missing-export finding is a claim about the file and needs every statement seen before it
		// can be made. A listener would have to report at the last node and guess which that is.
		sourceFile.ForEachChild(func(statement *ast.Node) bool {
			exportedName, isInline, ok := defaultExportedName(statement)
			if !ok {
				return false
			}
			hasDefaultExport = true

			if !isInline {
				ctx.ReportNode(statement, messagePageDefaultExportInline)
			}
			if exportedName != "" && !strings.HasSuffix(exportedName, "PageRoute") {
				ctx.ReportNode(statement, messagePageDefaultExportNameSuffix)
			}
			return false
		})

		if !hasDefaultExport {
			ctx.ReportNode(sourceFile, messagePageRequireDefaultExport)
		}

		return nil
	},
}

// defaultExportedName reads a statement's default export, if it is one.
//
// Returns the exported name, whether the export sits on the declaration itself, and whether this
// statement is a default export at all. An anonymous default export reports an empty name, which
// the caller treats as having no suffix to check rather than as absent.
func defaultExportedName(statement *ast.Node) (name string, isInline bool, ok bool) {
	switch statement.Kind {
	case ast.KindFunctionDeclaration:
		declaration := statement.AsFunctionDeclaration()
		if !module.HasDefaultModifier(declaration.Modifiers()) {
			return "", false, false
		}
		if declared := declaration.Name(); declared != nil {
			return declared.Text(), true, true
		}
		return "", true, true

	case ast.KindClassDeclaration:
		declaration := statement.AsClassDeclaration()
		if !module.HasDefaultModifier(declaration.Modifiers()) {
			return "", false, false
		}
		if declared := declaration.Name(); declared != nil {
			return declared.Text(), true, true
		}
		return "", true, true

	case ast.KindExportAssignment:
		// `export default <expression>`, which is the separate-statement form. An identifier here
		// names the function being exported; anything else is an expression with no name to check.
		assignment := statement.AsExportAssignment()
		if assignment.IsExportEquals {
			// `export = x` is the CommonJS form and is not a default export.
			return "", false, false
		}
		expression := ast.SkipParentheses(assignment.Expression)
		if expression != nil && expression.Kind == ast.KindIdentifier {
			return expression.Text(), false, true
		}
		return "", false, true
	}

	return "", false, false
}
