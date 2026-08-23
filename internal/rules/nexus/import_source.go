package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

// A specifier can arrive in three shapes, and a rule that guards a boundary has to see all three or
// it guards nothing: the shape it misses is the one that gets used to route around it.
//
//	import Thing from '@project/Thing'   static
//	await import('@project/Thing')       dynamic
//	require('@project/Thing')            CommonJS
//
// The TypeScript originals learned this the same way, which is why several of them share one
// visitor factory rather than each writing the static case and forgetting the other two.

// importSourceVisitors builds the listeners that call report for every string specifier in a file,
// whichever of the three shapes it arrives in.
//
// report is handed the specifier and the node to blame. The node differs by shape on purpose: a
// static import blames the whole declaration, while a dynamic or require call blames the call, since
// that is the expression a reader has to change.
func importSourceVisitors(report func(source string, node *ast.Node)) rule.Listeners {
	return rule.Listeners{
		ast.KindImportDeclaration: func(node *ast.Node) {
			declaration := node.AsImportDeclaration()
			if declaration == nil || declaration.ModuleSpecifier == nil {
				return
			}
			if !ast.IsStringLiteralLike(declaration.ModuleSpecifier) {
				return
			}
			report(declaration.ModuleSpecifier.Text(), node)
		},
		ast.KindCallExpression: func(node *ast.Node) {
			source, isImport := callExpressionSource(node)
			if !isImport {
				return
			}
			report(source, node)
		},
	}
}

// callExpressionSource returns the string specifier of a dynamic import or a require call.
//
// Both are call expressions, so one listener serves both rather than two listeners on the same kind,
// which the listener map could not hold anyway.
func callExpressionSource(node *ast.Node) (string, bool) {
	isDynamicImport := ast.IsImportCall(node)
	isRequire := ast.IsRequireCall(node, true)
	if !isDynamicImport && !isRequire {
		return "", false
	}

	call := node.AsCallExpression()
	if call == nil || call.Arguments == nil || len(call.Arguments.Nodes) == 0 {
		return "", false
	}

	// IsRequireCall already checked this, but IsImportCall does not look at arguments at all, so a
	// computed `import(someVariable)` reaches here and has no specifier to read.
	firstArgument := call.Arguments.Nodes[0]
	if !ast.IsStringLiteralLike(firstArgument) {
		return "", false
	}
	return firstArgument.Text(), true
}

// normalizedFileName returns a path with forward slashes, so a rule matching a path fragment finds
// it on Windows too.
func normalizedFileName(sourceFile *ast.SourceFile) string {
	if sourceFile == nil {
		return ""
	}
	return strings.ReplaceAll(sourceFile.FileName(), "\\", "/")
}

// importSpecifierNode returns the string-literal specifier inside an import-shaped node, falling
// back to the node itself when there is none to find.
//
// This exists because of where a finding lands rather than what it says. A node's Pos() includes its
// leading trivia, so reporting an import declaration anchors the finding at the first comment above
// it. In the Next wrapper files that put findings on line 1 while the author's
// `eslint-disable-next-line` sat on line 3 covering line 4, and a `-next-line` directive can only
// match the line after itself. The finding was unreachable by any suppression that could be written,
// and it read as a real finding in every count.
//
// A rule reporting through importSourceVisitors receives the declaration or call, which is the right
// node to reason about and the wrong node to point at. This turns one into the other.
func importSpecifierNode(node *ast.Node) *ast.Node {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case ast.KindImportDeclaration:
		if declaration := node.AsImportDeclaration(); declaration != nil && declaration.ModuleSpecifier != nil {
			return declaration.ModuleSpecifier
		}

	case ast.KindCallExpression:
		if call := node.AsCallExpression(); call != nil && call.Arguments != nil &&
			len(call.Arguments.Nodes) > 0 {
			return call.Arguments.Nodes[0]
		}
	}
	return node
}
