package structure

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utilities/ecmascript/imports"
	"github.com/system-inc/verify/internal/utilities/react"
)

const forwardRefReasoning = "React 19 passes ref as an ordinary property to a function component, " +
	"so forwardRef wraps a component in a layer that no longer does anything. The cost is not the " +
	"wrapper itself: a wrapped component's properties type is written against the wrapper rather " +
	"than the component, so the shape a reader sees at the call site is one indirection away from " +
	"the shape the component actually declares. Accept ref as a regular property instead."

var messageNoForwardRefCall = rule.Message{
	Id:          "noForwardRefCall",
	Description: "This uses forwardRef. " + forwardRefReasoning,
}

var messageNoForwardRefImport = rule.Message{
	Id: "noForwardRefImport",
	Description: "This imports forwardRef from react. " + forwardRefReasoning +
		" The import is reported separately from a call because a leftover import outlives the " +
		"last call site and is what makes the next one easy to write.",
}

// forwardRefTypeNames are the React types that only exist to describe a forwardRef wrapper.
//
// Flagged because they are how the wrapper survives a refactor: someone removes the forwardRef call
// and leaves the annotation, and the type keeps the old shape alive in every consumer's editor.
var forwardRefTypeNames = map[string]bool{
	"ForwardRefExoticComponent": true,
	"ForwardedRef":              true,
}

// ReactComponentNoForwardRef flags forwardRef in each of the three shapes it appears in.
//
//	valid:   function Field(properties: { ref?: Ref<HTMLInputElement> }) { ... }
//	invalid: import { forwardRef } from 'react'
//	invalid: React.forwardRef(function Field() { ... })
//	invalid: forwardRef(function Field() { ... })
//	invalid: const Field: React.ForwardRefExoticComponent<Properties> = ...
//
// Three shapes rather than one, because removing forwardRef from a codebase means removing all
// three and each survives the others. A call can go while the import stays; both can go while a
// type annotation keeps the wrapper's shape in every consumer's editor.
//
// The bare call is matched on the name alone, with no check that it came from react. That is the
// original's choice and its known limit: a local function named forwardRef would be reported. It is
// the right direction here, since the alternative needs the binding resolved and would go quiet on
// the aliased import (`import { forwardRef as fr }`) that the import arm exists to catch anyway.
var ReactComponentNoForwardRef = rule.Rule{
	Name: "react-component-no-forward-ref",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindImportDeclaration: func(node *ast.Node) {
				reportForwardRefImport(ctx, node)
			},

			ast.KindCallExpression: func(node *ast.Node) {
				callee := ast.SkipParentheses(node.AsCallExpression().Expression)
				if callee == nil {
					return
				}

				switch callee.Kind {
				case ast.KindIdentifier:
					if callee.Text() == "forwardRef" {
						ctx.ReportNode(callee, messageNoForwardRefCall)
					}
				case ast.KindPropertyAccessExpression:
					if react.IsNamespacedMember(callee, func(name string) bool { return name == "forwardRef" }) {
						ctx.ReportNode(callee, messageNoForwardRefCall)
					}
				}
			},

			// A qualified type name, which is where the wrapper hides after the call is gone.
			ast.KindQualifiedName: func(node *ast.Node) {
				qualified := node.AsQualifiedName()
				left, right := qualified.Left, qualified.Right
				if left == nil || right == nil {
					return
				}
				if left.Kind == ast.KindIdentifier && left.Text() == "React" &&
					right.Kind == ast.KindIdentifier && forwardRefTypeNames[right.Text()] {
					ctx.ReportNode(node, messageNoForwardRefCall)
				}
			},
		}
	},
}

// reportForwardRefImport flags a named import of forwardRef from react, aliased or not.
//
// The alias is deliberately not what is tested. `import { forwardRef as wrap }` imports the same
// function under another name, so the imported name is what decides and the local name is what the
// call arm would have to have resolved. Reporting the specifier rather than the whole statement
// keeps the finding on the one name at fault when several are imported together.
func reportForwardRefImport(ctx rule.Context, node *ast.Node) {
	declaration := node.AsImportDeclaration()
	if declaration.ModuleSpecifier == nil || !ast.IsStringLiteralLike(declaration.ModuleSpecifier) {
		return
	}
	if declaration.ModuleSpecifier.Text() != "react" {
		return
	}
	if declaration.ImportClause == nil {
		return
	}

	// A default or namespace import brings in no name to test here. `import React from 'react'` is
	// how React.forwardRef is reached, and that is the call arm's business. `BindingsOf` separates
	// the three shapes, so reading `Named` alone is that distinction rather than an omission.
	for _, element := range imports.BindingsOf(node).Named {
		if imports.ImportedNameOf(element) == "forwardRef" {
			// The specifier node rather than the local name, so an aliased import blames the whole
			// `forwardRef as forward` rather than just the alias.
			ctx.ReportNode(element, messageNoForwardRefImport)
		}
	}
}
