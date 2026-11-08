package git_tree

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
)

// An Entry is one path a listing records: its mode, its object (a blob for a file or a link, the
// commit a gitlink pins, the tree a directory is) and its path in the repository, slash-separated and
// relative to the repository's root.
type Entry struct {
	Mode   Mode
	Object string
	Path   string
}

// ParseTreeListing reads `git ls-tree -r -z --full-tree <commit>`'s output, with or without -t: one
// record per entry, "<mode> <kind> <object>", a tab and the path, each ended by a zero byte. It is
// strict, since a listing that isn't git's would name files no commit holds: a record git wouldn't
// print (a mode git doesn't record, a kind that disagrees with its mode, an object that isn't a name,
// a path that isn't local and clean), objects of two formats, or a last record with no zero byte after
// it is refused, named.
func ParseTreeListing(listing []byte) ([]Entry, error) {
	return parseListing(listing, "git ls-tree -r -z", func(header string) (Mode, string, error) {
		fields := strings.Split(header, " ")
		if len(fields) != 3 {
			return 0, "", fmt.Errorf("its header %q isn't \"<mode> <kind> <object>\"", header)
		}
		mode, modeError := ParseMode(fields[0])
		if modeError != nil {
			return 0, "", modeError
		}
		if Kind(fields[1]) != mode.kind() {
			return 0, "", fmt.Errorf("its kind is %q, and mode %s is a %s", fields[1], mode, mode.kind())
		}
		return mode, fields[2], nil
	})
}

// ParseStageListing reads `git ls-files --stage -z`'s output: one record per index entry,
// "<mode> <object> <stage>", a tab and the path, each ended by a zero byte. Every entry must be at
// stage 0, since an unmerged index records no one tree, and none may be a directory, which only a
// sparse index holds (ls-files expands one unless asked not to). Otherwise it is as strict as
// ParseTreeListing.
func ParseStageListing(listing []byte) ([]Entry, error) {
	return parseListing(listing, "git ls-files --stage -z", func(header string) (Mode, string, error) {
		fields := strings.Split(header, " ")
		if len(fields) != 3 {
			return 0, "", fmt.Errorf("its header %q isn't \"<mode> <object> <stage>\"", header)
		}
		mode, modeError := ParseMode(fields[0])
		if modeError != nil {
			return 0, "", modeError
		}
		if mode == ModeTree {
			return 0, "", fmt.Errorf("it is a directory, which only a sparse index records")
		}
		if fields[2] != "0" {
			return 0, "", fmt.Errorf("it is at stage %q, unmerged", fields[2])
		}
		return mode, fields[1], nil
	})
}

// parseListing splits a -z listing into its records and reads each with header, which returns the
// record's mode and object from the part before its tab. command names the listing in an error.
func parseListing(
	listing []byte,
	command string,
	header func(header string) (Mode, string, error),
) ([]Entry, error) {
	if len(listing) == 0 {
		return nil, nil
	}
	if listing[len(listing)-1] != 0 {
		return nil, fmt.Errorf("%s: the last record has no zero byte after it, so the listing was cut short", command)
	}
	var entries []Entry
	for index, record := range bytes.Split(listing[:len(listing)-1], []byte{0}) {
		// The path is everything after the first tab: a tab in a file's name is part of its path.
		before, path, found := strings.Cut(string(record), "\t")
		if !found {
			return nil, fmt.Errorf("%s: record %d, %q, has no tab before its path", command, index, record)
		}
		mode, object, headerError := header(before)
		if headerError != nil {
			return nil, fmt.Errorf("%s: record %d, %q: %w", command, index, record, headerError)
		}
		if !ValidObjectName(object) {
			return nil, fmt.Errorf("%s: record %d, %q: %q isn't an object's name", command, index, record, object)
		}
		if len(entries) != 0 && len(object) != len(entries[0].Object) {
			return nil, fmt.Errorf(
				"%s: record %d, %q: its object is %d digits, and the first record's is %d: "+
					"one repository's ids are one format",
				command,
				index,
				record,
				len(object),
				len(entries[0].Object),
			)
		}
		// fs.ValidPath is git's own rule for a tree's path, but for the root: no empty, "." or ".."
		// element, no leading or trailing slash.
		if !fs.ValidPath(path) || path == "." {
			return nil, fmt.Errorf(
				"%s: record %d, %q: %q isn't a clean path inside the repository",
				command,
				index,
				record,
				path,
			)
		}
		entries = append(entries, Entry{Mode: mode, Object: object, Path: path})
	}
	return entries, nil
}
