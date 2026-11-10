package program

import (
	"errors"
	"fmt"
	"os"
)

// RunCacheInput is one input's signature.
type RunCacheInput struct {
	Path string

	// Directory is true for an entry recorded to catch files added beneath it. Kept explicit
	// rather than inferred, because a path that was a directory and is now a file is a change.
	Directory bool

	// Exists is false for an input that was absent when recorded and must stay absent. A config the
	// run looked for and did not find is still an input: creating it changes the run.
	Exists bool

	Size                int64
	ModifiedNanoseconds int64

	// ChangedNanoseconds and Inode catch what the modification time cannot: different bytes of the same
	// size under a restored modification time, as cp -p, rsync -t and touch -r leave a file. Nothing in
	// userland can set a change time, and a file replaced by a rename has a new inode. Zero where the
	// platform's stat does not give them.
	ChangedNanoseconds int64
	Inode              uint64

	// ExistenceOnly is set on a directory the run only asked whether it exists (see InputRecorder.Inputs),
	// which matches while it is still the same directory, by inode, whatever was added to it since. Module
	// resolution asks it of every directory from the project up to the root, and on a busy machine /tmp and
	// the like change many times a second: checked at full signature, they cost a replay on a tree nothing
	// had touched (#q51f02a). A package.json or node_modules appearing in one is its own absent input.
	ExistenceOnly bool

	// TrackedIgnored, set on a repository's index, keys the input on what the index decides for the run rather than
	// on its bytes (#m1daxxk). git rewrites the index on a `git status` and on every `git add`, and what the run
	// read from it is narrow: which paths the ignore rules exclude are tracked, and which are gitlinks. So an index
	// whose signature moved still matches when gitignore.TrackedIgnoredDigest reads the digest recorded here. Nil
	// for every other input, and for an index the reader could not read whole, which is keyed on its bytes.
	TrackedIgnored *TrackedIgnoredKey
}

// stillMatches compares one input against the filesystem now, noting what the stat found in snapshot, matching or
// not: the stat is current either way.
func (input RunCacheInput) stillMatches(snapshot *StatSnapshot) error {
	information, statError := os.Stat(input.Path)
	if snapshot != nil {
		snapshot.note(input.Path, information, statError)
	}
	if !input.Exists {
		if statError == nil {
			return fmt.Errorf("%w: %s now exists", ErrRunCacheMiss, input.Path)
		}
		if errors.Is(statError, os.ErrNotExist) {
			return nil
		}
		// Neither present nor provably absent, for example a permission error. Doubt is a miss.
		return fmt.Errorf("%w: %s cannot be checked: %w", ErrRunCacheMiss, input.Path, statError)
	}
	if statError != nil {
		return fmt.Errorf("%w: %s: %w", ErrRunCacheMiss, input.Path, statError)
	}
	if information.IsDir() != input.Directory {
		return fmt.Errorf("%w: %s changed between file and directory", ErrRunCacheMiss, input.Path)
	}
	if input.ExistenceOnly {
		// Still there and still the same directory, which is all the run asked of it.
		if _, inode := changeTimeAndInode(input.Path, information); inode != input.Inode {
			return fmt.Errorf("%w: %s was replaced", ErrRunCacheMiss, input.Path)
		}
		return nil
	}
	if signatureError := input.signatureMatches(information); signatureError != nil {
		// An index whose bytes moved is read anew, and matches while it decides what it decided.
		if input.TrackedIgnored != nil && !input.Directory {
			return input.TrackedIgnored.stillDecides(input.Path)
		}
		return signatureError
	}
	return nil
}

// signatureMatches compares the stat of a present input against its recorded signature.
func (input RunCacheInput) signatureMatches(information os.FileInfo) error {
	// A directory's size is filesystem bookkeeping and moves with its entries on some filesystems,
	// so only its modification time decides. A file needs both.
	if !input.Directory && information.Size() != input.Size {
		return fmt.Errorf("%w: %s changed size", ErrRunCacheMiss, input.Path)
	}
	if information.ModTime().UnixNano() != input.ModifiedNanoseconds {
		return fmt.Errorf("%w: %s was modified", ErrRunCacheMiss, input.Path)
	}
	if changed, inode := changeTimeAndInode(
		input.Path,
		information,
	); changed != input.ChangedNanoseconds || inode != input.Inode {
		return fmt.Errorf("%w: %s was changed or replaced under its modification time", ErrRunCacheMiss, input.Path)
	}
	return nil
}
