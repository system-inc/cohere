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
	listed, parseError := parseListing(listing, "git ls-tree -r -z", func(header string) (Mode, string, int, error) {
		fields := strings.Split(header, " ")
		if len(fields) != 3 {
			return 0, "", 0, fmt.Errorf("its header %q isn't \"<mode> <kind> <object>\"", header)
		}
		mode, modeError := ParseMode(fields[0])
		if modeError != nil {
			return 0, "", 0, modeError
		}
		if Kind(fields[1]) != mode.kind() {
			return 0, "", 0, fmt.Errorf("its kind is %q, and mode %s is a %s", fields[1], mode, mode.kind())
		}
		// A tree holds no stages: every entry it lists is merged.
		return mode, fields[2], 0, nil
	})
	return entriesOf(listed), parseError
}

// ParseStageListing reads `git ls-files --stage -z`'s output: one record per index entry,
// "<mode> <object> <stage>", a tab and the path, each ended by a zero byte. Every entry must be at
// stage 0, since an unmerged index records no one tree, and none may be a directory, which only a
// sparse index holds (ls-files expands one unless asked not to). Otherwise it is as strict as
// ParseTreeListing.
func ParseStageListing(listing []byte) ([]Entry, error) {
	staged, parseError := parseListing(
		listing,
		"git ls-files --stage -z",
		func(header string) (Mode, string, int, error) {
			mode, object, stage, headerError := readStageHeader(header)
			if headerError != nil {
				return 0, "", 0, headerError
			}
			if stage != 0 {
				return 0, "", 0, fmt.Errorf("it is at stage %d, unmerged", stage)
			}
			return mode, object, stage, nil
		},
	)
	return entriesOf(staged), parseError
}

// An IndexEntry is one record of the working index: an Entry and the stage git holds it at. Stage 0 is
// a merged path. A path a merge left in conflict is listed once per side that holds it instead: stage 1
// the merge's base, 2 ours, 3 theirs. It is a type of its own rather than a field on Entry, so an Entry
// stays what a tree holds and TreeId never meets a stage.
type IndexEntry struct {
	Entry
	Stage int
}

// ParseIndexListing reads `git ls-files --stage -z`'s output as ParseStageListing does, but keeps each
// entry's stage and reads an unmerged one, so an index mid-merge still reads: a conflicted path is one
// entry per stage, in git's order. It refuses everything else ParseStageListing refuses, and a stage
// git doesn't record.
func ParseIndexListing(listing []byte) ([]IndexEntry, error) {
	return parseListing(listing, "git ls-files --stage -z", readStageHeader)
}

// readStageHeader reads the part of an ls-files record before its tab, "<mode> <object> <stage>",
// refusing a directory (only a sparse index records one) and a stage but 0 to 3.
func readStageHeader(header string) (Mode, string, int, error) {
	fields := strings.Split(header, " ")
	if len(fields) != 3 {
		return 0, "", 0, fmt.Errorf("its header %q isn't \"<mode> <object> <stage>\"", header)
	}
	mode, modeError := ParseMode(fields[0])
	if modeError != nil {
		return 0, "", 0, modeError
	}
	if mode == ModeTree {
		return 0, "", 0, fmt.Errorf("it is a directory, which only a sparse index records")
	}
	// Checked as the one digit git prints, not read by strconv.Atoi, which would also take "+1" and "01".
	if len(fields[2]) != 1 || fields[2][0] < '0' || fields[2][0] > '3' {
		return 0, "", 0, fmt.Errorf("its stage %q isn't one git records (0 to 3)", fields[2])
	}
	return mode, fields[1], int(fields[2][0] - '0'), nil
}

// entriesOf is the entries of a listing with their stages dropped, for a reader whose every entry is
// at stage 0.
func entriesOf(staged []IndexEntry) []Entry {
	if staged == nil {
		return nil
	}
	entries := make([]Entry, 0, len(staged))
	for _, entry := range staged {
		entries = append(entries, entry.Entry)
	}
	return entries
}

// parseListing splits a -z listing into its records and reads each with header, which returns the
// record's mode, object and stage from the part before its tab. command names the listing in an error.
func parseListing(
	listing []byte,
	command string,
	header func(header string) (Mode, string, int, error),
) ([]IndexEntry, error) {
	if len(listing) == 0 {
		return nil, nil
	}
	if listing[len(listing)-1] != 0 {
		return nil, fmt.Errorf("%s: the last record has no zero byte after it, so the listing was cut short", command)
	}
	var entries []IndexEntry
	for index, record := range bytes.Split(listing[:len(listing)-1], []byte{0}) {
		// The path is everything after the first tab: a tab in a file's name is part of its path.
		before, path, found := strings.Cut(string(record), "\t")
		if !found {
			return nil, fmt.Errorf("%s: record %d, %q, has no tab before its path", command, index, record)
		}
		mode, object, stage, headerError := header(before)
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
		entries = append(entries, IndexEntry{Entry: Entry{Mode: mode, Object: object, Path: path}, Stage: stage})
	}
	return entries, nil
}
