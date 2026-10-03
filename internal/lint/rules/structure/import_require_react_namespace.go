package structure

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/imports"
	"github.com/system-inc/cohere/internal/lint/ecmascript/react"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var messageNoNamedImport = rule.Message{
	Id: "noNamedImport",
	Description: "This imports a name out of react directly. Reach it through the React namespace " +
		"instead. A bare `useState` or `Ref` at a call site says nothing about where it came from, " +
		"so a reader has to go to the import block to find out, and a local binding with the same " +
		"name shadows it silently. React.useState carries its provenance with it.",
}

var messageNoDestructuringFromReact = rule.Message{
	Id: "noDestructuringFromReact",
	Description: "This import takes names out of react without importing React itself, so there " +
		"is no namespace to reach through and every use in the file has to be a bare name. Import " +
		"React as the default and use React.* instead.",
}

var messageNoCallWithoutPrefix = rule.Message{
	Id: "noCallWithoutPrefix",
	Description: "This calls a hook that was destructured out of react. Call it as React.<hook> so " +
		"the call site says where the hook comes from.",
}

var messageNoTypeReferenceWithoutPrefix = rule.Message{
	Id: "noTypeReferenceWithoutPrefix",
	Description: "This uses a type that was destructured out of react. Write it as React.<Type> so " +
		"the annotation says where the type comes from.",
}

// ImportRequireReactNamespace flags names taken out of react rather than reached through React.
//
//	valid:   import React from 'react'; React.useState(0)
//	invalid: import { useState } from 'react'
//	invalid: import { useState } from 'react'; useState(0)
//	invalid: import type { Ref } from 'react'; function f(r: Ref<HTMLElement>) {}
//
// Four message ids, and they are not four alternatives: one import produces the import finding and
// then a further finding at every use. That is deliberate rather than noisy. The import is one edit
// and each use is another, so a reader fixing this needs to see all of them, and the counts are
// what tell them how large the change is before they start.
//
// The rule carries a tracking set for the same reason displayName does: whether a bare `useState`
// is a violation depends on whether this file imported it, which is an earlier node. A bare name
// with no import from react is somebody's own function and none of this rule's business.
//
// One asymmetry is preserved from the original and is worth naming, because it looks like a bug. The
// call arm tests react.IsHookName ("use" plus an uppercase letter) while the type arm tests only a "use"
// prefix. They disagree on a name like `used`: called, it is not treated as a hook; used as a type,
// it is not treated as a type either. Both arms therefore stay silent on it, which is the
// conservative direction, and reproducing the two spellings keeps our findings equal to the gate's
// rather than adding one it does not have.
var ImportRequireReactNamespace = rule.Rule{
	Name: "structure/import-require-react-namespace",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		// Local names brought in by a named import from react, which is what a later bare use has
		// to be checked against.
		importedFromReact := map[string]bool{}

		return rule.Listeners{
			// This rule registers three listeners and only this one is an import-shape concern.
			// A commit declining to convert it to the shared import visitor said it "watches
			// KindImportDeclaration alone", which is checkable and false: KindCallExpression at
			// :110 reads call sites of destructured hooks and KindTypeReference at :125 reads type
			// positions. Neither treats a call as an import, and the rule never reaches for
			// IsImportCall, IsRequireCall or CallExpressionSource, so the decision was right for
			// the reason it gave.
			//
			// Recorded here because a true conclusion resting on a checkable falsehood fails
			// inspection rather than failing in production: the next reader counts the listeners,
			// finds three, and concludes the analysis was wrong when it was right.
			ast.KindImportDeclaration: func(node *ast.Node) {
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

				// `BindingsOf` separates the three shapes one statement can carry, which this rule
				// needs all of: the default decides whether the namespace is available, the named
				// ones are the violation, and the namespace arm is deliberately left alone below.
				bindings := imports.BindingsOf(node)

				// The default import has to be named React specifically. `import Reakt from 'react'`
				// provides a namespace nobody will write, so it does not satisfy the rule.
				hasReactDefault := bindings.Default != nil && bindings.Default.Text() == "React"

				namedCount := 0
				for _, element := range bindings.Named {
					// The local name is what later uses are matched against, and it is the alias
					// when there is one, so this reads `Name` where the sibling rules that ask
					// which export was bound read `ImportedNameOf`.
					if localName := element.Name(); localName != nil {
						importedFromReact[localName.Text()] = true
					}

					namedCount++
					ctx.ReportNode(element, messageNoNamedImport)
				}

				// A namespace import (`import * as React from 'react'`) provides the namespace and
				// destructures nothing, so it is left alone.
				if namedCount > 0 && !hasReactDefault {
					ctx.ReportNode(node, messageNoDestructuringFromReact)
				}
			},

			ast.KindCallExpression: func(node *ast.Node) {
				callee := ast.SkipParentheses(node.AsCallExpression().Expression)
				if callee == nil || callee.Kind != ast.KindIdentifier {
					return
				}
				name := callee.Text()

				// Only hooks. A destructured type or event-handler name is not something anyone
				// calls, so testing every imported name here would report on nothing real while
				// risking a finding on an unrelated call.
				if importedFromReact[name] && react.IsHookName(name) {
					ctx.ReportNode(node, messageNoCallWithoutPrefix)
				}
			},

			ast.KindTypeReference: func(node *ast.Node) {
				typeName := node.AsTypeReferenceNode().TypeName
				if typeName == nil || typeName.Kind != ast.KindIdentifier {
					// A qualified name is already namespaced, which is what the rule is asking for.
					return
				}
				name := typeName.Text()

				// The "use" prefix rather than react.IsHookName, matching the original. See the rule
				// comment: the two arms deliberately spell the hook test differently.
				if importedFromReact[name] && !strings.HasPrefix(name, "use") {
					ctx.ReportNode(node, messageNoTypeReferenceWithoutPrefix)
				}
			},
		}
	},
}
