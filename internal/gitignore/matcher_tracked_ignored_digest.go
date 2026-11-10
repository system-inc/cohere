package gitignore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// indexModeGitlink is a gitlink's mode, a submodule's commit standing where its directory is.
const indexModeGitlink = 0o160000

// trackedIgnoredDigestFormat opens every digest, so one taken under another rule never equals one taken under this.
const trackedIgnoredDigestFormat = "cohere tracked ignored 1"

// ErrNoTrackedIgnoredDigest is a matcher that read no tracked paths, so it has no digest to take: its IndexFallback
// says why. A run cache keys on the index's bytes instead, as it did before the digest.
var ErrNoTrackedIgnoredDigest = errors.New("no digest of what the index tracks")

// TrackedIgnoredDigest is what a repository's index decides for a walk of scope, digested: every entry at or below
// scope, a directory relative to the matcher's root, slash separated, "" for the root, that the ignore rules alone
// would exclude, and every gitlink there. A run cache keys on it rather than on the index's bytes, which git rewrites
// on a `git status` and on every `git add`, so a run over a tree nothing changed in would rarely replay
// (#m1daxxk).
//
// The index decides two things for a walk. A path the ignore rules exclude is checked when the index tracks it
// (gitignore(5): "Files already tracked by Git are not affected"), so the entries the rules exclude are the ones
// whose tracking decides anything. And a go.work root reads its submodules from the index's gitlinks
// (internal/go_engine/project), so every gitlink is in too. Every other entry is a file the walk finds by listing
// its directory, decided by the rules alone and read as an input of its own, so its tracking decides nothing. Each
// entry is digested by its path and its mode: every stage of a conflict, an intent-to-add entry and a gitlink alike,
// and a file swapped for a gitlink at one path, which a `dir/` pattern matches as a directory, reads as a change.
//
// What decides an entry is the ignore files from the root down to its directory, read as Enter reads them, stopping
// at an excluded directory, whose rule then decides everything below. So the digest of an unchanged index is the same
// while those files are, and a change to one that excludes a tracked path newly, or no longer, changes it. A file the
// walk never asked about may be decided by an ignore file the walk never read; it only ever costs a miss, since an
// entry the walk asked about is decided by files the walk read.
//
// A matcher that read no index, for any reason IndexFallback gives, returns ErrNoTrackedIgnoredDigest wrapping it,
// and so does an ignore file between the root and an entry that cannot be read faithfully: the caller falls back to
// the index's bytes, never to a digest of less than the index says. It is asked of the matcher New returns.
func (matcher *Matcher) TrackedIgnoredDigest(scope string) (string, error) {
	if matcher.index == nil {
		return "", fmt.Errorf("%w: %w", ErrNoTrackedIgnoredDigest, matcher.indexFallback)
	}
	if matcher.directory != "" {
		// Invariant: the digest is asked of New's matcher, whose directories are entered from the root.
		panic(fmt.Sprintf("gitignore: a digest asked of the matcher for %s, not the root's", matcher.directory))
	}
	cleanedScope := cleanRelative(scope)
	var entries []indexEntry
	for _, entry := range matcher.index.entries {
		if cleanedScope == "" || entry.path == cleanedScope || strings.HasPrefix(entry.path, cleanedScope+"/") {
			entries = append(entries, entry)
		}
	}
	scopes, scopesError := matcher.entryDirectories(entries)
	if scopesError != nil {
		return "", fmt.Errorf("%w: %w", ErrNoTrackedIgnoredDigest, scopesError)
	}

	// Which entries the rules exclude, decided across workers, each into its own slot, then digested in order.
	ruled := make([]bool, len(entries))
	workers := min(runtime.NumCPU(), 8)
	var group sync.WaitGroup
	for worker := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for position := worker; position < len(entries); position += workers {
				entry := entries[position]
				ruled[position] = entry.mode&0o170000 == indexModeGitlink ||
					scopes[parentDirectory(entry.path)].ruledOut(entry.path, false)
			}
		}()
	}
	group.Wait()

	digest := sha256.New()
	digest.Write([]byte(trackedIgnoredDigestFormat + "\x00" + cleanedScope + "\x00"))
	for position, entry := range entries {
		if ruled[position] {
			digest.Write([]byte(entry.path + "\x00" + strconv.FormatUint(uint64(entry.mode), 8) + "\n"))
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// entryDirectories is the matcher of every directory holding one of entries, by its path relative to the root, ""
// for the root. Each is stepped into from its parent's, a level of the tree at a time and a level's directories
// across workers, since each reads its own ignore file: ahra's index names 1,790 directories, and entered one after
// another on a loaded machine they cost about 140ms (#m1daxxk). No directory is asked whether it holds a `.git`, as
// Enter asks: an entry inside a repository of its own is one no walk of this one reaches, and deciding it by these
// rules only ever costs a miss.
func (matcher *Matcher) entryDirectories(entries []indexEntry) (map[string]*Matcher, error) {
	var levels [][]string
	seen := map[string]bool{"": true}
	for _, entry := range entries {
		for directory := parentDirectory(entry.path); !seen[directory]; directory = parentDirectory(directory) {
			seen[directory] = true
			depth := strings.Count(directory, "/")
			for len(levels) <= depth {
				levels = append(levels, nil)
			}
			levels[depth] = append(levels[depth], directory)
		}
	}
	scopes := map[string]*Matcher{"": matcher}
	workers := min(runtime.NumCPU(), 8)
	for _, level := range levels {
		stepped := make([]*Matcher, len(level))
		failures := make([]error, len(level))
		var group sync.WaitGroup
		for worker := range workers {
			group.Add(1)
			go func() {
				defer group.Done()
				for position := worker; position < len(level); position += workers {
					child := *scopes[parentDirectory(level[position])]
					// Capped, so a step's append never writes into the parent's files, which siblings share.
					child.files = child.files[:len(child.files):len(child.files)]
					failures[position] = child.step(level[position])
					stepped[position] = &child
				}
			}()
		}
		group.Wait()
		for position, directory := range level {
			if failures[position] != nil {
				return nil, failures[position]
			}
			scopes[directory] = stepped[position]
		}
	}
	return scopes, nil
}

// parentDirectory is the directory holding relativePath, slash separated, "" for an entry of the root.
func parentDirectory(relativePath string) string {
	directory := path.Dir(relativePath)
	if directory == "." {
		return ""
	}
	return directory
}

// ruledOut reports whether the ignore rules alone exclude relativePath, an entry of this matcher's directory, as
// Ignored would answer for a path the index does not track: the rule that excluded a directory above it, or the last
// rule matching it.
func (matcher *Matcher) ruledOut(relativePath string, isDirectory bool) bool {
	if matcher.excludedBy != nil {
		return true
	}
	decided := matcher.decide(relativePath, path.Base(relativePath), isDirectory, len(matcher.files))
	return decided != nil && !decided.negated
}
