package git_tree

import (
	"fmt"
	"io/fs"
	"os"
	"strconv"
)

// Mode is the mode git records for a tree entry. Git records only these five, whatever the file's
// permissions on disk were.
type Mode uint32

const (
	// ModeTree is a directory, a tree object of its own.
	ModeTree Mode = 0o040000
	// ModeRegular is a file its owner can't execute.
	ModeRegular Mode = 0o100644
	// ModeExecutable is a file its owner can execute.
	ModeExecutable Mode = 0o100755
	// ModeSymbolicLink is a symbolic link, whose blob is its target's bytes.
	ModeSymbolicLink Mode = 0o120000
	// ModeGitlink is a submodule, whose object is the commit its parent pins.
	ModeGitlink Mode = 0o160000
)

// String is the mode as `git ls-tree` and `git ls-files --stage` print it: six octal digits, so a
// tree is "040000".
func (mode Mode) String() string {
	return fmt.Sprintf("%06o", uint32(mode))
}

// treeObjectSpelling is the mode as a tree object holds it: octal with no leading zero, so a tree is
// "40000" there, where ls-tree prints "040000". Hashing ls-tree's spelling would name another tree.
func (mode Mode) treeObjectSpelling() string {
	return strconv.FormatUint(uint64(mode), 8)
}

// kind is the object an entry of the mode names, as ls-tree prints it beside the mode.
func (mode Mode) kind() Kind {
	switch mode {
	case ModeTree:
		return Tree
	case ModeGitlink:
		return Commit
	}
	return Blob
}

// ParseMode reads a mode as ls-tree prints it, refusing any spelling but the five git records.
func ParseMode(text string) (Mode, error) {
	for _, mode := range []Mode{ModeTree, ModeRegular, ModeExecutable, ModeSymbolicLink, ModeGitlink} {
		if text == mode.String() {
			return mode, nil
		}
	}
	return 0, fmt.Errorf("%q isn't a mode git records (040000, 100644, 100755, 120000 or 160000)", text)
}

// ModeOf is the mode `git add` records for a file with this information, from os.Lstat: git's
// create_ce_mode. A symbolic link is ModeSymbolicLink. A regular file is ModeExecutable when its owner
// may execute it and ModeRegular otherwise, since git reads only the owner's bit (ce_permissions in
// git's object.h is `mode & 0100`): a file at 0o654 is 100644, and one at 0o745 is 100755.
//
// A directory is refused: git records one as a gitlink when it holds a repository and as a tree of its
// files otherwise, and its information can't tell which. Anything else (a device, a socket, a pipe) is
// nothing git tracks.
func ModeOf(information fs.FileInfo) (Mode, error) {
	switch {
	case information.Mode()&fs.ModeSymlink != 0:
		return ModeSymbolicLink, nil
	case information.Mode().IsRegular():
		if information.Mode().Perm()&0o100 != 0 {
			return ModeExecutable, nil
		}
		return ModeRegular, nil
	case information.IsDir():
		return 0, fmt.Errorf(
			"%s is a directory: a tree, or a submodule's gitlink when it holds a repository, "+
				"which its file information can't tell",
			information.Name(),
		)
	}
	return 0, fmt.Errorf("%s has mode %v, a kind of file git doesn't track", information.Name(), information.Mode())
}

// HashFile is what `git add` would record for the file at name: its mode (ModeOf), and the id of its
// blob, which is its content for a file and its target's bytes for a symbolic link (never followed).
// It is `git hash-object --no-filters`, without git's attributes, so a path a .gitattributes filter
// rewrites hashes to its bytes on disk.
func HashFile(format ObjectFormat, name string) (Mode, string, error) {
	information, lstatError := os.Lstat(name)
	if lstatError != nil {
		return 0, "", lstatError
	}
	mode, modeError := ModeOf(information)
	if modeError != nil {
		return 0, "", modeError
	}
	object, hashError := hashBlobAt(format, name, mode, information)
	if hashError != nil {
		return 0, "", hashError
	}
	return mode, object, nil
}

// hashBlobAt is the blob id of the file at name, whose Lstat information is given and whose mode is
// ModeOf's for it: the link's target for a link, the file's content, exactly information.Size() bytes
// of it, for a file.
func hashBlobAt(format ObjectFormat, name string, mode Mode, information fs.FileInfo) (string, error) {
	if mode == ModeSymbolicLink {
		target, readlinkError := os.Readlink(name)
		if readlinkError != nil {
			return "", readlinkError
		}
		return HashBytes(format, Blob, []byte(target))
	}
	file, openError := os.Open(name)
	if openError != nil {
		return "", openError
	}
	// Ignored: the file is only read, and HashObject has already checked it gave every byte it held.
	defer file.Close()
	object, hashError := HashObject(format, Blob, file, information.Size())
	if hashError != nil {
		return "", fmt.Errorf("hashing %s: %w", name, hashError)
	}
	return object, nil
}
