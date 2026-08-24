// Per-file memoization of lowering, so the rules that share a function share the work.
package hir

import (
	"strconv"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

// ForFunction is Lower followed by Construct, computed once per function per file and shared by
// every rule that asks.
//
// # Why this exists
//
// Three react rules lower the same functions independently, each over the whole source file:
// set-state-in-render, static-components, and set-state-in-effect. A component holding a `useMemo`,
// a state setter, and a JSX tag was lowered three times and converted to single-assignment form
// three times. Measured with `verify --timing` before this existed: set-state-in-effect 829ms,
// set-state-in-render 535ms, static-components 494ms, against every syntactic react rule under
// 20ms. The author of the most expensive one named the cause exactly — "a property of lowering, not
// this rule" — and a shared per-file cache as the fix.
//
// # Why Construct is inside the cached value rather than left to the caller
//
// This is the part that makes a shared lowering safe, and getting it wrong would have been silent.
// `Construct` mutates in place: it renames identifiers and appends phis. It is NOT idempotent.
// Measured on `let y = 2; if (y > 1) { y = 1; } else { y = 2; } let x = y; return x;`, a second
// Construct over the same graph moved it from 1 phi and 5 named values to 2 phis and 6 named
// values.
//
// The dangerous part is that the doubly-constructed graph is still WELL-FORMED — `VerifySSA`
// reports zero violations on it. So a cache that handed out a raw lowering and let each of the
// three callers run its own Construct would have given the second and third caller a graph carrying
// phis that correspond to no branch in the source, with no verifier, no panic, and no test
// complaining. It would have surfaced later as a wrong finding in whichever rule happened to run
// second, and it would have looked like a bug in that rule rather than in this cache.
//
// So the unit stored here is the finished graph, lowered and constructed, and Construct runs
// exactly once per function. That also answers whether construction wants a cache of its own: it
// does not, because it is not separable from the lowering it mutates. Measured on the probe
// component in cache_test.go, construction is about 18% of the combined cost (23µs against 102µs
// for lowering), and all of it is now paid once rather than three times. Callers read it and must not mutate it; the three callers today only
// read, deriving their own maps (`UnconditionalBlocks` builds a fresh map, the taint passes build
// their own).
//
// # Why the function node alone is the key
//
// `Lower` takes a node and a type checker, so a key naming only the node is wrong if the checker
// can differ between two calls within one file. It cannot. `internal/program/walk.go` builds one
// `fileChecker` per file and closes it into every rule's Context alongside the one `FileCache`
// created on the line above, so the checker and this cache have exactly the same lifetime. A
// differing checker would mean a differing cache, and there is no path that produces one.
//
// # Why this is safe for the on-disk findings cache
//
// It is not run-scoped state and it reaches nothing outside the file. A lowering is derived purely
// from a function node in the file being linted plus that file's checker, and the cache is created
// and discarded per file by the walk. So no rule using this acquires the `ReadsProgram` property,
// and the hash-keyed findings cache stays correct: nothing here can make one file's result depend
// on another file's contents, which is the failure `ReadsProgram` exists to declare.
func ForFunction(ctx rule.Context, node *ast.Node) *Function {
	if node == nil {
		return nil
	}
	// Declining a checker-less file here rather than in each caller keeps the answer independent of
	// which rule asked first. `Lower`'s own comment is that a lowering built without a checker
	// resolves every reference as a global, making the result well-formed and meaningless, and two
	// of the three callers already refused to run in that state. Deciding it once means a cached
	// entry cannot depend on rule order.
	if ctx.TypeChecker == nil {
		return nil
	}
	return rule.Cached(ctx.FileCache, cacheKeyFor(node), func() *Function {
		lowered := Lower(node, ctx.TypeChecker)
		if lowered == nil {
			return nil
		}
		Construct(lowered)
		return lowered
	})
}

// cacheKeyFor names one function node within one file.
//
// The node's source position is its identity here. That is sufficient precisely because the cache
// is per-file: two function nodes in one file cannot start at the same offset, and a node in
// another file is another cache. The kind is included so the key stays unambiguous if a future
// caller ever asks for a different node that happens to share a position with this one.
//
// A pointer address would be the obvious alternative and is worse: it is not printable in a way
// `FillDurations` can report usefully, and it would silently key two runs of the same file
// differently.
func cacheKeyFor(node *ast.Node) string {
	return "hir.Function:" + strconv.Itoa(int(node.Kind)) + ":" + strconv.Itoa(node.Pos())
}
