package nextjs

import "strings"

// IsRoutingFolder reports whether a folder's name is App Router syntax: a name whose shape Next.js reads, rather
// than words a reader reads. Each is told by its punctuation, the way Next.js itself tells them, never by a list of
// names:
//
//   - `[id]`, `[...slug]` and `[[...slug]]`, a dynamic segment, whose inside is the key `params` carries, an
//     identifier the code reads as `params.id`, so its spelling is the identifier rules' to judge.
//   - `(os-layout)`, a route group, and `(.)photo`, `(..)photo` and `(...)photo`, an intercepting route, which all
//     open with a parenthesis.
//   - `@modal`, a parallel route's slot, whose name is the prop its layout receives.
//
// A private folder, `_components`, is not among them: the underscore opts it out of routing, and the name after it
// is the project's own, judged as any folder's is.
func IsRoutingFolder(folder string) bool {
	switch {
	case strings.HasPrefix(folder, "[") && strings.HasSuffix(folder, "]"):
		return true
	case strings.HasPrefix(folder, "("):
		return true
	case strings.HasPrefix(folder, "@"):
		return true
	}
	return false
}
