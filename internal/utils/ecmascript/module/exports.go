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
