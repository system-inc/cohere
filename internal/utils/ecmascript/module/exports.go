// Package module answers questions about how a file exposes its declarations.
//
// It sits under `internal/utils/ecmascript/` because a default export is an ECMAScript module
// concept rather than a React or JSX one, and because that prefix is already on the leaf guard's
// allowlist.
package module

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// IsDefaultExported reports whether a declaration carries both the export and default modifiers.
//
// This is lifted on a stronger argument than caller count. Two implementations of it already existed
// in one package, byte-identical in logic under two names: `isDefaultExported` in
// `react_component_no_multiple_primary.go` and `hasDefaultModifier` in
// `next_require_page_default_export.go`. Neither file could see the other, so the second author
// wrote it again rather than found it.
//
// **Two copies that agree today are the state just before drift, not evidence that drift is not a
// risk.** They are free to diverge on the first case nobody has thought about yet, and nothing would
// report it: each rule's fixtures would keep passing against its own copy.
//
// A research pass on `no-page-custom-font` then found a third caller wanting the same predicate from
// `internal/rules/next/`, which is what turned a tidy-up into a lift.
//
// Both modifiers are required rather than either. `export function run() {}` is exported and not
// default; `export default function run() {}` is both.
func IsDefaultExported(node *ast.Node) bool {
	if node == nil {
		return false
	}
	return HasDefaultModifier(node.Modifiers())
}

// HasDefaultModifier is IsDefaultExported for a caller that already holds the modifier list.
//
// Both spellings survive the lift because both call shapes existed in the rules it came from, and
// collapsing them would push one call site into reaching for `node.Modifiers()` on a value it does
// not hold.
func HasDefaultModifier(modifiers *ast.ModifierList) bool {
	if modifiers == nil {
		return false
	}
	sawExport := false
	sawDefault := false
	for _, modifier := range modifiers.Nodes {
		switch modifier.Kind {
		case ast.KindExportKeyword:
			sawExport = true
		case ast.KindDefaultKeyword:
			sawDefault = true
		}
	}
	return sawExport && sawDefault
}

// IsExported reports whether a declaration carries the export keyword, default or not.
//
// Three copies of this existed under three names before the lift: `isExportedDeclaration` in
// `nexus/consistency_no_screaming_snake_case.go`, `isExportedStatement` in
// `structure/react_component_no_multiple_primary.go`, and a third shape below. Two packages, six
// call sites, and no file able to see another's.
//
// The two that agreed are collapsed here. The third is not, and the difference is the reason this
// file has two functions rather than one.
func IsExported(node *ast.Node) bool {
	if node == nil {
		return false
	}
	return HasExportModifier(node.Modifiers())
}

// HasExportModifier is IsExported for a caller holding the modifier list.
func HasExportModifier(modifiers *ast.ModifierList) bool {
	if modifiers == nil {
		return false
	}
	for _, modifier := range modifiers.Nodes {
		if modifier.Kind == ast.KindExportKeyword {
			return true
		}
	}
	return false
}

// IsExportedByName reports an export that is not a default export.
//
// **This is a different question from IsExported and the names are one word apart, which is exactly
// why both belong on a shelf rather than in three rule files.** `export default function Thing()`
// carries both keywords in one modifier list, so a rule asking about named exports has to exclude
// it, and a rule asking about exports at all must not.
//
// The shape it exists for: `react-component-require-named-export` flags a component exported only as
// a default, so reading `export default` as a named export would silence the rule on precisely the
// code it exists to catch.
func IsExportedByName(modifiers *ast.ModifierList) bool {
	if modifiers == nil {
		return false
	}
	hasExport := false
	hasDefault := false
	for _, modifier := range modifiers.Nodes {
		switch modifier.Kind {
		case ast.KindExportKeyword:
			hasExport = true
		case ast.KindDefaultKeyword:
			hasDefault = true
		}
	}
	return hasExport && !hasDefault
}
