package gitignore

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Matcher is one directory's view of a repository's ignore rules: those of every ignore file from the
// root down to that directory. It is never modified once made, so a walk may keep one per directory and
// share them freely.
type Matcher struct {
	root      string
	directory string

	// files are the rule lists of each `.gitignore` from the root down, root first. exclude is
	// info/exclude's.
	files   [][]rule
	exclude []rule

	// excludedBy is the rule that excluded this directory or one above it. It decides every path below that the
	// index does not track.
	excludedBy *rule

	// index is every path the repository's index tracks, shared by every matcher Enter makes, and nil when it was
	// not read. indexFile is the index read or looked for, "" for a root with no `.git`, and indexFallback is why
	// no tracked path was read from it, nil when they were.
	index         *trackedIndex
	indexFile     string
	indexFallback error

	// listing, when set, answers Enter's questions about a `.git` or a `.gitignore` a directory does not
	// hold, from a listing already read. See WithListing.
	listing Listing
}

// WithListing returns the matcher, and every matcher Enter makes from it, answering from listing where a
// listed directory shows it holds no `.git` or no `.gitignore`, as an lstat of either would then fail. One
// the listing names is still asked of the disk, so what is read is read as before. The format walk passes
// discovery's listings (#g3046x5), which spares two lstats per directory entered.
func (matcher *Matcher) WithListing(listing Listing) *Matcher {
	listed := *matcher
	listed.listing = listing
	return &listed
}

// listedAbsent reports whether a listing of directory, an absolute path, shows it holds no entry called name.
func (matcher *Matcher) listedAbsent(directory string, name string) bool {
	if matcher.listing == nil {
		return false
	}
	entries, listed := matcher.listing(directory)
	if !listed {
		return false
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return false
		}
	}
	return true
}

// Root is the repository root the matcher reads, as an absolute path.
func (matcher *Matcher) Root() string { return matcher.root }

// Directory is the directory this matcher answers for, relative to the root, "" for the root itself.
func (matcher *Matcher) Directory() string { return matcher.directory }

// IndexFile is the repository's index the matcher read or looked for, so a cache can name it as an input: an
// index appearing or changing changes what is tracked, and so what is ignored. It is "" for a root holding no
// `.git`, whose matcher never looks for one.
func (matcher *Matcher) IndexFile() string { return matcher.indexFile }

// IndexFallback is why the matcher read no tracked paths from the repository's index, wrapping ErrNoIndex,
// ErrSplitIndex, ErrSparseIndex or ErrMalformedIndex, and nil when it read them. A matcher that fell back decides by
// the ignore files alone, so a file git tracks under a pattern that matches it reads ignored.
func (matcher *Matcher) IndexFallback() error { return matcher.indexFallback }

// Excluded reports whether this directory, or one above it, is excluded, and by which rule. A walk that
// entered one anyway gets the same answer for everything inside that the index does not track: Ignored
// answers false for a tracked path, and for a directory holding one, whatever excluded the directory above.
func (matcher *Matcher) Excluded() (bool, Source) {
	if matcher.excludedBy == nil {
		return false, Source{}
	}
	return true, matcher.excludedBy.source
}

// Enter returns the matcher for relativeDirectory, which is this matcher's directory or one below it,
// slash separated and relative to the root.
//
// It reads each `.gitignore` between the two once, and stops reading at a directory that is itself
// excluded, as git does: the rule that excluded it then decides everything below. A directory holding a
// `.git` is refused with ErrNestedRepository, and an ignore file that cannot be read faithfully (a
// symbolic link, which git does not follow in a tree; one too large; one unreadable) is an error naming
// it, never skipped.
func (matcher *Matcher) Enter(relativeDirectory string) (*Matcher, error) {
	cleanedDirectory := cleanRelative(relativeDirectory)
	if cleanedDirectory == matcher.directory {
		return matcher, nil
	}
	remainder, below := strings.CutPrefix(cleanedDirectory, matcher.directory)
	if matcher.directory != "" {
		remainder, below = strings.CutPrefix(remainder, "/")
	}
	if !below || remainder == "" {
		return nil, fmt.Errorf("gitignore: %s is not below %s", cleanedDirectory, displayDirectory(matcher.directory))
	}

	entered := *matcher
	entered.files = entered.files[:len(entered.files):len(entered.files)]
	for _, name := range strings.Split(remainder, "/") {
		child := joinRelative(entered.directory, name)
		childDirectory := filepath.Join(matcher.root, filepath.FromSlash(child))
		if matcher.listedAbsent(childDirectory, ".git") {
			// Not there, as the lstat would have found.
		} else if _, statError := os.Lstat(filepath.Join(childDirectory, ".git")); statError == nil {
			return nil, fmt.Errorf("gitignore: %s %w", childDirectory, ErrNestedRepository)
		}
		entered.directory = child
		if entered.excludedBy != nil {
			continue
		}
		if decided := entered.decide(
			child,
			path.Base(child),
			true,
			len(entered.files),
		); decided != nil && !decided.negated {
			entered.excludedBy = decided
			continue
		}
		if matcher.listedAbsent(childDirectory, IgnoreFileName) {
			// No `.gitignore`, which readTreeFile reads as no rules.
			entered.files = append(entered.files, nil)
			continue
		}
		rules, readTreeFileError := readTreeFile(matcher.root, child)
		if readTreeFileError != nil {
			return nil, readTreeFileError
		}
		entered.files = append(entered.files, rules)
	}
	return &entered, nil
}

// Ignored reports whether relativePath, an entry of this matcher's directory given relative to the root,
// is excluded, and the rule that decided. A rule that re-included the path is reported with false.
//
// A path the index tracks, or a directory holding one, is not ignored and no rule decided it, which is git
// check-ignore's answer for it (`::` in its verbose form). A walk that prunes what Ignored excludes therefore
// enters such a directory even when a rule excludes it, and asks about each entry there: the tracked ones read
// not ignored, and the rest read ignored by the rule that excluded the directory, as before.
//
// isDirectory says whether the path is a directory, which is what a pattern with a trailing `/` asks; a
// symbolic link to a directory is not one, as git does not follow it. Asking about a path outside this
// directory is a programming error and panics, because the answer would silently leave out the ignore
// files between the two.
func (matcher *Matcher) Ignored(relativePath string, isDirectory bool) (bool, Source) {
	cleanedPath := cleanRelative(relativePath)
	parent := path.Dir(cleanedPath)
	if parent == "." {
		parent = ""
	}
	if parent != matcher.directory {
		// Invariant: a caller enters the path's directory before asking, as IgnoredPath and every walk do, so a
		// mismatch is a caller's bug, and answering anyway would leave out the ignore files between the two.
		panic(fmt.Sprintf("gitignore: asked about %s through the matcher for %s; Enter its directory first",
			cleanedPath, displayDirectory(matcher.directory)))
	}
	if matcher.index.tracks(cleanedPath) {
		return false, Source{}
	}
	if matcher.excludedBy != nil {
		return true, matcher.excludedBy.source
	}
	decided := matcher.decide(cleanedPath, path.Base(cleanedPath), isDirectory, len(matcher.files))
	if decided == nil {
		return false, Source{}
	}
	return !decided.negated, decided.source
}

// IgnoredPath is Ignored for a path anywhere below this matcher's directory: it enters the directories in
// between first, reading their ignore files, so the answer names a nested file's line when one decides.
// It is for a question about one path, such as explaining why a file was left out; a walk enters each
// directory itself and asks Ignored, which reads every ignore file once.
func (matcher *Matcher) IgnoredPath(relativePath string, isDirectory bool) (bool, Source, error) {
	cleanedPath := cleanRelative(relativePath)
	parent := path.Dir(cleanedPath)
	if parent == "." {
		parent = ""
	}
	scope, enterError := matcher.Enter(parent)
	if enterError != nil {
		return false, Source{}, enterError
	}
	ignored, source := scope.Ignored(cleanedPath, isDirectory)
	return ignored, source, nil
}

// decide is the rule that decides relativePath among the first fileCount tree files and info/exclude,
// or nil: the deepest file first, the last line of each first, info/exclude last.
func (matcher *Matcher) decide(relativePath string, baseName string, isDirectory bool, fileCount int) *rule {
	for fileIndex := fileCount - 1; fileIndex >= 0; fileIndex-- {
		if decided := lastMatching(matcher.files[fileIndex], relativePath, baseName, isDirectory); decided != nil {
			return decided
		}
	}
	return lastMatching(matcher.exclude, relativePath, baseName, isDirectory)
}
