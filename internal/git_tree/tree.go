package git_tree

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

// ErrCommitNotHead is wrapped by CheckCommit's error when the commit object doesn't hash to the
// commit it is said to be.
var ErrCommitNotHead = errors.New("the commit object isn't the commit it is said to be")

// ErrTreeNotListed is wrapped by CheckCommit's error when the commit's tree isn't the tree its
// listing rebuilds: the listing is another commit's.
var ErrTreeNotListed = errors.New("the listing isn't the commit's tree")

// treeChild is one entry of a directory being built into a tree object: its mode and its name in the
// directory. Its object is looked up by its full path, or built, when the tree is written.
type treeChild struct {
	mode Mode
	name string
}

// TreeId is the id of the root tree `git write-tree` writes for entries: each directory a tree object
// of its children, each child "<mode> <name>", a zero byte and the raw object id, in git's order. That
// order is a byte sort of the names in which a directory's name compares as if it ended in "/"
// (base_name_compare in git's tree.c), so a file a.b sorts before a directory a, and a gitlink, which
// is no directory, sorts by its bare name. A subtree's mode is written "40000".
//
// entries is a listing ParseTreeListing or ParseStageListing read, in any order. A directory entry
// (ls-tree -t) is checked, not trusted: the tree rebuilt under it must be the one it names. A path
// listed twice, or listed both as a file and as a directory others sit under, is refused, as is an
// object not of format.
func TreeId(format ObjectFormat, entries []Entry) (string, error) {
	digits, digitsError := format.digits()
	if digitsError != nil {
		return "", digitsError
	}
	children := map[string][]treeChild{}
	// kinds holds each path's mode as it is first seen: listed, or implied as a directory by a path
	// under it.
	kinds := map[string]Mode{}
	listedTrees := map[string]string{}
	claim := func(name string, mode Mode, listed bool) error {
		seen, found := kinds[name]
		switch {
		case !found:
			kinds[name] = mode
			children[path.Dir(name)] = append(children[path.Dir(name)], treeChild{mode: mode, name: path.Base(name)})
		case seen == ModeTree && mode == ModeTree && !listed:
		case seen == ModeTree && mode == ModeTree:
			if _, twice := listedTrees[name]; twice {
				return fmt.Errorf("%q is listed twice", name)
			}
		case seen == ModeTree || mode == ModeTree:
			return fmt.Errorf("%q is listed as a file and as a directory", name)
		default:
			return fmt.Errorf("%q is listed twice", name)
		}
		return nil
	}
	objects := map[string]string{}
	for _, entry := range entries {
		if len(entry.Object) != digits || !ValidObjectName(entry.Object) {
			return "", fmt.Errorf("%q has object %q, not a %s name", entry.Path, entry.Object, format)
		}
		for parent := path.Dir(entry.Path); parent != "."; parent = path.Dir(parent) {
			if claimError := claim(parent, ModeTree, false); claimError != nil {
				return "", claimError
			}
		}
		if claimError := claim(entry.Path, entry.Mode, true); claimError != nil {
			return "", claimError
		}
		if entry.Mode == ModeTree {
			listedTrees[entry.Path] = entry.Object
		} else {
			objects[entry.Path] = entry.Object
		}
	}
	sortName := func(child treeChild) string {
		if child.mode == ModeTree {
			return child.name + "/"
		}
		return child.name
	}
	var build func(directory string) (string, error)
	build = func(directory string) (string, error) {
		slices.SortFunc(children[directory], func(left, right treeChild) int {
			return strings.Compare(sortName(left), sortName(right))
		})
		var content bytes.Buffer
		for _, child := range children[directory] {
			full := path.Join(directory, child.name)
			object := objects[full]
			if child.mode == ModeTree {
				built, buildError := build(full)
				if buildError != nil {
					return "", buildError
				}
				if named, found := listedTrees[full]; found && named != built {
					return "", fmt.Errorf(
						"%q is listed as tree %s, and its entries rebuild tree %s",
						full,
						named,
						built,
					)
				}
				object = built
			}
			raw, decodeError := hex.DecodeString(object)
			if decodeError != nil {
				return "", fmt.Errorf("%q has object %q: %w", full, object, decodeError)
			}
			fmt.Fprintf(&content, "%s %s\x00", child.mode.treeObjectSpelling(), child.name)
			content.Write(raw)
		}
		return HashBytes(format, Tree, content.Bytes())
	}
	return build(".")
}

// CheckCommit refuses a commit object and listing that aren't head's: the object must hash to head,
// as git hashes a commit, and the tree its first line names must be the tree entries rebuild
// (TreeId), so a listing of any other commit is refused. The error wraps ErrCommitNotHead or
// ErrTreeNotListed for those two, so a caller can word each its own way.
func CheckCommit(head string, commit []byte, entries []Entry) error {
	format, formatError := FormatOf(head)
	if formatError != nil {
		return formatError
	}
	object, hashError := HashBytes(format, Commit, commit)
	if hashError != nil {
		return hashError
	}
	if object != head {
		return fmt.Errorf("%w: it hashes to %s, not %s", ErrCommitNotHead, object, head)
	}
	line, _, _ := bytes.Cut(commit, []byte("\n"))
	named, found := strings.CutPrefix(string(line), "tree ")
	if !found || len(named) != len(head) || !ValidObjectName(named) {
		return fmt.Errorf("commit %s's first line is %q, which names no tree", head, line)
	}
	rebuilt, treeError := TreeId(format, entries)
	if treeError != nil {
		return fmt.Errorf("the listing of commit %s: %w", head, treeError)
	}
	if rebuilt != named {
		return fmt.Errorf(
			"%w: the listing rebuilds tree %s, and commit %s names tree %s",
			ErrTreeNotListed,
			rebuilt,
			head,
			named,
		)
	}
	return nil
}
