package program

import (
	"fmt"

	"github.com/system-inc/cohere/internal/gitignore"
)

// TrackedIgnoredKey is what an index input is keyed on: the digest gitignore.TrackedIgnoredDigest took, for the walk of
// Scope in the repository at Root, of the index the run read.
type TrackedIgnoredKey struct {
	// Root is the repository's root, which holds the `.git` the index belongs to, as an absolute path.
	Root string
	// Scope is the directory the run walked, relative to Root and slash separated, "" for Root itself.
	Scope string
	// Digest is the digest, as TrackedIgnoredDigest returns it.
	Digest string
}

// stillDecides reports whether the index at index, read anew with the ignore files from Root, still decides what it
// decided for the recorded run. Any doubt is a miss: an index now read from another file, one the reader cannot read
// whole, or an ignore file it cannot.
func (key *TrackedIgnoredKey) stillDecides(index string) error {
	matcher, newError := gitignore.New(key.Root)
	if newError != nil {
		return fmt.Errorf("%w: %s moved, and its ignore rules cannot be read: %w", ErrRunCacheMiss, index, newError)
	}
	if matcher.IndexFile() != index {
		return fmt.Errorf("%w: %s moved, and the repository at %s now reads its index from %q", ErrRunCacheMiss,
			index, key.Root, matcher.IndexFile())
	}
	digest, digestError := matcher.TrackedIgnoredDigest(key.Scope)
	if digestError != nil {
		return fmt.Errorf("%w: %s moved, and %w", ErrRunCacheMiss, index, digestError)
	}
	if digest != key.Digest {
		return fmt.Errorf("%w: %s changed which paths its ignore rules exclude are tracked, or its gitlinks",
			ErrRunCacheMiss, index)
	}
	return nil
}
