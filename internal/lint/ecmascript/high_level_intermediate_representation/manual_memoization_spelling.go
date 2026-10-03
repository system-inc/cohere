// Whether a function's source can spell a `useMemo` or `useCallback` call the erasure recognises.
package high_level_intermediate_representation

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// MayNameManualMemoization reports whether a function could contain a `useMemo` or `useCallback`
// call that `DropManualMemoization` would recognise. False is a proof that the erasure finds nothing
// in it; true is only a maybe. text is the function's own source range.
//
// Two callers decide on it whether to lower at all: `ForFunctionWithoutManualMemoization`, which
// shares the memo-intact lowering when the erasure could change nothing, and
// `react/preserve-manual-memoization`, whose findings all live inside memo blocks. Both are wrong
// in the same silent direction if this answers false for a function the erasure would have
// changed, so they share one answer rather than each keeping a copy that can drift.
//
// # Why false proves the erasure finds nothing
//
// `recogniseManualMemoCall` matches a temporary holding either a `LoadGlobal` whose `Name` is
// `useMemo` or `useCallback`, or a `Primitive` string of one of those names, the `React.useMemo`
// property or a computed `React['useMemo']`. Both come from `node.Text()` of an identifier,
// property name, or string literal inside the lowered function, so the name is spelled in that
// function's source range. An import alias does not escape this: `LoadGlobal.Name` is the local
// spelling, so `import { useMemo as memo }` is not recognised by the pipeline either.
//
// # Why a backslash falls back to the syntax tree
//
// `node.Text()` is the cooked value, and an escape can spell the name without its letters appearing
// in a row: `useMemo` as an identifier, `'use\x4Demo'` or a line continuation in a string.
// So a range with a backslash and no plain spelling is answered by reading the cooked text of every
// identifier and string-like literal under the function, which are exactly the nodes the two
// recognised instructions are lowered from. A substring search alone missed these: measured before
// this was shared, five escaped spellings of `useCallback` wrapping a setter left
// `react-hooks/set-state-in-effect` silent where the plain spelling reported, because the cache
// handed it the memo-intact graph. The walk costs a fraction of lowering, and it matters: measured on
// ahra, about 600 of 3,785 files carry a backslash, and lowering every function in them on a "maybe"
// was half of what preserve-manual-memoization still cost after the plain-text check.
func MayNameManualMemoization(functionNode *ast.Node, text string) bool {
	if SpellsManualMemoization(text) {
		return true
	}
	if strings.IndexByte(text, '\\') < 0 {
		return false
	}
	return cooksToManualMemoization(functionNode)
}

// SpellsManualMemoization reports whether text spells either name plainly.
func SpellsManualMemoization(text string) bool {
	return strings.Contains(text, "useMemo") || strings.Contains(text, "useCallback")
}

// cooksToManualMemoization reports whether any identifier or string-like literal under node has the
// cooked text `useMemo` or `useCallback`.
//
// These are the node kinds whose `Text()` lowering copies into a `LoadGlobal.Name` (an identifier
// read) or a `Primitive` string (a string literal, a template without substitutions, or the property
// name of a method call). Every other kind lowers to an instruction the recogniser does not match, so
// a kind missing here would be a missed finding and a kind added here only costs a comparison.
func cooksToManualMemoization(node *ast.Node) bool {
	found := false
	var visit func(child *ast.Node) bool
	visit = func(child *ast.Node) bool {
		switch child.Kind {
		case ast.KindIdentifier, ast.KindPrivateIdentifier, ast.KindStringLiteral,
			ast.KindNoSubstitutionTemplateLiteral:
			if name := child.Text(); name == "useMemo" || name == "useCallback" {
				found = true
				return true
			}
		}
		return child.ForEachChild(visit)
	}
	visit(node)
	return found
}
