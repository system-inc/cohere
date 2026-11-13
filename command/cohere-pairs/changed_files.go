package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// changedFileOutcome says what became of a tracked file a run changed, as the progress and the table say it. Since
// engine's #34r3v1y the engine's go no longer rewrites a tracked file, so one a run changed is a defect the pair is
// refused for (refusals), and it is put back all the same, so the measurement's later runs check the commit's tree
// and the table that names the refusal is whole.
const changedFileOutcome = "put back from its commit, and the pair is refused for it (#34r3v1y)"

// restoreChangedFiles finds every tracked file a run changed in a corpus's layers, puts each back as its layer's commit
// holds it, and returns their paths within the corpus, slash separated. Before #34r3v1y the go command rewrote files
// as it read a workspace: on Pupa a `--no-fix --no-cache` run left deeafbf4's go.work.sum modified, as go list tidied
// it, and every run after it would have checked another tree than the first. Each file is written from its commit's
// blob, with no checkout, restore or reset, and a layer still changed after that stops the measurement, since its
// later runs would read what no record says. Nothing here is silent: every path goes into the run's record, its
// progress line and the table, each with its corpus, side and run, and refusals refuses the pair for each.
func restoreChangedFiles(corpusRoot string, layers []corpusLayer) ([]string, error) {
	restored := []string{}
	for _, layer := range layers {
		directory := layerDirectory(corpusRoot, layer)
		changed, changedError := writeBackChangedFiles(directory, layer.commit)
		for _, changedPath := range changed {
			restored = append(restored, path.Join(layer.within, changedPath))
		}
		if changedError != nil {
			return restored, changedError
		}
	}
	return restored, nil
}

// writeBackChangedFiles puts every tracked file changed in the repository at directory back as commit holds it, and
// returns their paths, relative to its root. It refuses a repository still changed after that.
func writeBackChangedFiles(directory string, commit string) ([]string, error) {
	changed, changedError := changedTrackedFiles(directory)
	if changedError != nil || len(changed) == 0 {
		return changed, changedError
	}
	for _, changedPath := range changed {
		if restoreError := restoreTrackedFile(directory, commit, changedPath); restoreError != nil {
			return changed, restoreError
		}
	}
	still, stillError := changedTrackedFiles(directory)
	if stillError != nil {
		return changed, stillError
	}
	if len(still) > 0 {
		return changed, fmt.Errorf("%s still has changed tracked files after they were put back: %q", directory, still)
	}
	return changed, nil
}

// changedTrackedFiles lists the tracked files of the repository at directory whose contents differ from its index,
// relative to its root. Submodules are left to their own layer's listing, and untracked files, a cache among them,
// are no file of the commit's. The -z listing is "XY path" a record, and a rename or copy, which only a staged
// change makes, carries the path it came from in the record after it.
func changedTrackedFiles(directory string) ([]string, error) {
	listing, statusError := gitBytes(directory, "status", "--porcelain", "-z", "--untracked-files=no",
		"--ignore-submodules=all")
	if statusError != nil {
		return nil, statusError
	}
	records := bytes.Split(bytes.TrimSuffix(listing, []byte{0}), []byte{0})
	changed := []string{}
	for index := 0; index < len(records); index++ {
		record := string(records[index])
		if record == "" {
			continue
		}
		if len(record) < 4 {
			return nil, fmt.Errorf("git status in %s printed %q, not a status and a path", directory, record)
		}
		changed = append(changed, record[3:])
		if record[0] == 'R' || record[0] == 'C' {
			index++
		}
	}
	return changed, nil
}

// restoreTrackedFile writes a file back as commit holds it, its contents from `git cat-file blob`, which prints the
// blob's bytes with no filter between, with the commit's mode, or the link it records.
func restoreTrackedFile(directory string, commit string, trackedPath string) error {
	entry, listTreeError := gitOutput(directory, "ls-tree", commit, "--", trackedPath)
	if listTreeError != nil {
		return listTreeError
	}
	fields := strings.Fields(entry)
	if len(fields) < 3 {
		return fmt.Errorf("%s at %s holds no %s to put back", directory, commit, trackedPath)
	}
	contents, catFileError := gitBytes(directory, "cat-file", "blob", fields[2])
	if catFileError != nil {
		return catFileError
	}
	target := filepath.Join(directory, filepath.FromSlash(trackedPath))
	if createParentError := os.MkdirAll(filepath.Dir(target), 0o755); createParentError != nil {
		return createParentError
	}
	// Whatever the run left at the path that is not a regular file, a link or an empty directory, is taken away
	// first, so the commit's file is what stands there.
	if information, lstatError := os.Lstat(target); lstatError == nil && !information.Mode().IsRegular() {
		if removeError := os.Remove(target); removeError != nil {
			return fmt.Errorf("clearing %s to put it back: %w", target, removeError)
		}
	} else if lstatError != nil && !errors.Is(lstatError, fs.ErrNotExist) {
		return lstatError
	}
	switch fields[0] {
	case "120000":
		if removeError := os.Remove(target); removeError != nil && !errors.Is(removeError, fs.ErrNotExist) {
			return fmt.Errorf("clearing %s to put its link back: %w", target, removeError)
		}
		return os.Symlink(string(contents), target)
	case "100644", "100755":
		permissions := os.FileMode(0o644)
		if fields[0] == "100755" {
			permissions = 0o755
		}
		if writeError := os.WriteFile(target, contents, permissions); writeError != nil {
			return fmt.Errorf("putting %s back: %w", target, writeError)
		}
		// WriteFile keeps the mode of a file that already stood there, so the commit's is set.
		return os.Chmod(target, permissions)
	}
	return fmt.Errorf("%s at %s holds %s as mode %s, which is not a file or a link to put back", directory, commit,
		trackedPath, fields[0])
}
