// Package structure holds the rules for this project's own conventions, as opposed to the language
// rules in core and the cross-cutting ones in nexus.
package structure

import "strings"

// FileContext classifies a filename, which is enough for several rules to decide whether they apply
// before a single node is visited.
//
// The TypeScript original computes eighteen flags and memoizes them in a module-level map. Only the
// three below have a consumer here, and the rest are ported when a rule needs them: a flag nobody
// reads is a claim nobody checks, and this file is the wrong place to keep a shadow copy of the
// original's whole surface.
type FileContext struct {
	// IsReactFile is a .tsx or .jsx extension. JSX is what makes a component file, so a rule about
	// components declines everything else.
	IsReactFile bool

	// IsSpecialNextJsFile marks the files Next.js reads by name rather than by import. Their shape
	// is the framework's contract rather than an authoring choice, so a rule about how many
	// components belong in a file has no standing in one.
	IsSpecialNextJsFile bool

	// IsGeneratedFile marks output nobody edits by hand, where a finding asks for a change that the
	// next generation would undo.
	IsGeneratedFile bool
}

// nextJsSpecialFileBaseNames are the files Next.js resolves by name. Both React extensions apply to
// each, and route additionally exists as a plain .ts API route.
var nextJsSpecialFileBaseNames = []string{
	"page", "layout", "error", "global-error", "not-found", "loading",
}

// FileContextFor classifies one path.
//
// Backslashes are folded to forward slashes first, so a Windows-shaped path classifies the same way.
// The original does this too, and it matters here: every check below is a suffix or substring test
// that a backslash would silently defeat.
func FileContextFor(fileName string) FileContext {
	normalizedPath := strings.ReplaceAll(fileName, `\`, "/")

	isReactFile := strings.HasSuffix(normalizedPath, ".tsx") || strings.HasSuffix(normalizedPath, ".jsx")

	isSpecialNextJsFile := strings.HasSuffix(normalizedPath, "/route.ts") ||
		strings.HasSuffix(normalizedPath, "/route.tsx")
	for _, baseName := range nextJsSpecialFileBaseNames {
		if strings.HasSuffix(normalizedPath, "/"+baseName+".tsx") ||
			strings.HasSuffix(normalizedPath, "/"+baseName+".jsx") {
			isSpecialNextJsFile = true
			break
		}
	}

	return FileContext{
		IsReactFile:         isReactFile,
		IsSpecialNextJsFile: isSpecialNextJsFile,
		IsGeneratedFile:     strings.Contains(normalizedPath, "/generated/"),
	}
}
