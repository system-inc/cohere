// Package nextjs answers which Next.js file a path is, for the rules that gate on it.
//
// Eight rules in the `@next/next` family decide "is this the document file" and upstream answers it
// four different ways: a shared `is_document_page` that splits on `pages`, two byte-equivalent
// hand-rolled basename tests, a third basename test that also accepts a `_document/index.*`
// directory, and one rule with no gate at all.
//
// **That is drift upstream has accumulated rather than a specification to reproduce**, and a
// per-rule port would bake it in permanently: eight rules, four predicates, and no guard that could
// tell you they disagreed, because each passes its own fixtures.
//
// So this package is a deliberate divergence from the port target, ruled 2026-08-23. Fidelity is to
// what a rule decides rather than to how it obtains what it needs, and all eight are deciding the
// same thing.
package nextjs

import (
	"strings"
)

// IsDocumentFile reports whether a path is the Next.js custom document.
//
// The union of every spelling upstream uses, because they are four answers to one question:
//
//	pages/_document.tsx           every spelling agrees
//	src/pages/_document.tsx       every spelling agrees
//	pages/_document/index.tsx     only no_head_import_in_document accepts it
//	components/_document.tsx      the basename spellings accept it, is_document_page does not
//	pages/_documentation.tsx      nothing accepts it, and the trailing dot is why
//
// The trailing dot on `_document.` is load-bearing and is the one piece of upstream precision worth
// keeping: without it `_documentation.tsx` reads as the document, which is a real ESLint false
// positive that oxc fixed.
//
// The `pages` requirement is deliberately dropped. `is_document_page` requires it and three sibling
// rules do not, and requiring it would make this narrower than three of the eight rules it serves.
// A file named `_document.tsx` outside a pages directory is still a custom document by name, and a
// rule that ignored it would be silent on a real misplacement.
//
// So this predicate is wider than the shared helper on two paths and narrower on one, which is worth
// stating because a reader checking only one direction will conclude it drifts:
//
//	components/_document.tsx     wider   the basename spellings accept it, is_document_page does not
//	pages/_document/index.tsx    wider   only the widest spelling accepts it
//	pages/_documentation.tsx     NARROWER  is_document_page accepts it, this does not
//
// The last one is the trailing dot doing its work, and it is the direction that matters most: the
// shared helper tests `starts_with("/_document")` with no dot, so a file merely beginning with the
// word reads as the document. A research pass reported this comparison with the direction inverted,
// claiming we report that path and upstream exempts it. Checked at the source: the opposite is true,
// and the fixture asserting our answer was already correct.
func IsDocumentFile(filePath string) bool {
	baseName, parentName := splitPath(filePath)

	if strings.HasPrefix(baseName, "_document.") {
		return true
	}
	// The directory form, which only the widest upstream spelling accepts: `_document/index.tsx`
	// names the same thing as `_document.tsx` and a rule seeing one should see the other.
	return strings.HasPrefix(baseName, "index") && strings.HasPrefix(parentName, "_document")
}

// IsInApplicationDirectory reports whether a path sits under a Next.js app directory.
//
// A substring test rather than a segment test, matching upstream's `is_in_app_dir`. That is looser
// than it looks: a directory named `application/` contains `app/` as a substring and answers true.
// Reproduced rather than tightened because the rules gating on it are gating on a routing model, and
// narrowing it here would make them silent on trees upstream lints.
func IsInApplicationDirectory(filePath string) bool {
	return strings.Contains(filePath, "app/") || strings.Contains(filePath, "app\\")
}

// splitPath returns a path's base name and its parent directory's name.
//
// Both separators are handled because every upstream predicate handles both, and a path arriving in
// the Windows shape must answer the same as its POSIX twin. Splitting on the last separator of
// either kind is what makes that true without asking which platform produced the path.
func splitPath(filePath string) (baseName string, parentName string) {
	baseName = filePath
	if index := lastSeparator(filePath); index >= 0 {
		baseName = filePath[index+1:]
		parent := filePath[:index]
		parentName = parent
		if parentIndex := lastSeparator(parent); parentIndex >= 0 {
			parentName = parent[parentIndex+1:]
		}
	}
	return baseName, parentName
}

// lastSeparator returns the index of the final path separator of either kind, or -1.
func lastSeparator(filePath string) int {
	posix := strings.LastIndexByte(filePath, '/')
	windows := strings.LastIndexByte(filePath, '\\')
	if windows > posix {
		return windows
	}
	return posix
}
