package gitignore

import "path"

// Patterns is a list of ignore-file lines that is not a file in the tree, read with the same syntax and
// relative to the root it is applied at: a project setting that says what to skip in the words a
// `.gitignore` would use. Within the list the last matching line decides, as within one file.
type Patterns struct {
	rules []rule
}

// Ignored reports whether the list excludes relativePath, given relative to the root it applies at, and
// the line that decided. Unlike a Matcher it knows nothing of directories above the path: a walk applying
// it prunes each directory it excludes, so a path below one is never asked about.
func (patterns *Patterns) Ignored(relativePath string, isDirectory bool) (bool, Source) {
	cleanedPath := cleanRelative(relativePath)
	decided := lastMatching(patterns.rules, cleanedPath, path.Base(cleanedPath), isDirectory)
	if decided == nil {
		return false, Source{}
	}
	return !decided.negated, decided.source
}
