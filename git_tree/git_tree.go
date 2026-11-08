// Package git_tree is git's own arithmetic over a repository's files, written once for the three
// repositories that need it: an object's id (blob, tree and commit, as `git hash-object` and
// `git write-tree` name them), the listings `git ls-tree -z` and `git ls-files --stage -z` print, the
// mode git records for a file on disk, and whether a working-tree file still is what a listing records.
// It also holds the `.tracked` manifest's layout, the record Loom's builder writes into a source that
// carries no .git and Adamic reads there (manifest.go). That layout is our protocol, not git's.
//
// Every answer is git's, checked against git itself in this module's tests: the executable bit is the
// owner's alone, a symbolic link is a blob of its target's bytes, a gitlink is a submodule's commit and
// sorts as a file, and in a tree object a directory sorts as if its name ended in "/".
//
// It is pure: it runs no git, opens no store and keys no cache. Each consumer brings its own git runner
// and its own words for an error, and calls this for the arithmetic. It is a module of its own, beside
// static_single_assignment, so Loom and Adamic can require it without requiring cohere's root module.
package git_tree

import (
	"encoding/hex"
	"fmt"
	"io"
)

// Kind is an object's type, spelled as git writes it in the object's header and in `git ls-tree`.
type Kind string

// The object kinds a tree's arithmetic meets. Their values are git's own spellings, since they are
// hashed into every object's header. A tag is left out: no listing or manifest holds one.
const (
	Blob   Kind = "blob"
	Tree   Kind = "tree"
	Commit Kind = "commit"
)

// HashObject is git's name for content as an object of kind: the format's hash of the header
// "<kind> <size>" and a zero byte, then the content. A reader that doesn't hold exactly size bytes is
// refused, so a file that grows or shrinks while it is read is an error, never another file's id.
func HashObject(format ObjectFormat, kind Kind, content io.Reader, size int64) (string, error) {
	sum, newHashError := format.newHash()
	if newHashError != nil {
		return "", newHashError
	}
	fmt.Fprintf(sum, "%s %d\x00", kind, size)
	read, copyError := io.Copy(sum, content)
	if copyError != nil {
		return "", copyError
	}
	if read != size {
		return "", fmt.Errorf("%d bytes read, and it held %d", read, size)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// HashBytes is HashObject over content held whole.
func HashBytes(format ObjectFormat, kind Kind, content []byte) (string, error) {
	sum, newHashError := format.newHash()
	if newHashError != nil {
		return "", newHashError
	}
	fmt.Fprintf(sum, "%s %d\x00", kind, len(content))
	sum.Write(content)
	return hex.EncodeToString(sum.Sum(nil)), nil
}
