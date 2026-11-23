package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// sideWorkspace is the go.work every load of a tree reads: `off` for a tree with only a go.mod, or a side copy of the
// tree's go.work in a directory of its own, removed by remove.
type sideWorkspace struct {
	workFile  string
	directory string
}

// workFileName and workSumName are a workspace's file and its sums.
const (
	workFileName = "go.work"
	workSumName  = "go.work.sum"
)

// sideWorkFile makes the go.work root's loads read. In workspace mode go writes every sum it read that go.work.sum
// lacks whatever -mod says, so a tree with a go.work is loaded under a copy that names every directory absolutely and
// holds go.work.sum's bytes, and a go list that rewrites the sums rewrites the copy's.
func sideWorkFile(root string) (sideWorkspace, error) {
	original := filepath.Join(root, workFileName)
	contents, readError := os.ReadFile(original)
	if os.IsNotExist(readError) {
		return sideWorkspace{workFile: "off"}, nil
	}
	if readError != nil {
		return sideWorkspace{}, readError
	}
	parsed, parseError := modfile.ParseWork(original, contents, nil)
	if parseError != nil {
		return sideWorkspace{}, parseError
	}
	var written strings.Builder
	if parsed.Go != nil {
		fmt.Fprintf(&written, "go %s\n", parsed.Go.Version)
	}
	if parsed.Toolchain != nil {
		fmt.Fprintf(&written, "toolchain %s\n", parsed.Toolchain.Name)
	}
	for _, setting := range parsed.Godebug {
		fmt.Fprintf(&written, "godebug %s=%s\n", setting.Key, setting.Value)
	}
	for _, used := range parsed.Use {
		fmt.Fprintf(&written, "use %s\n", modfile.AutoQuote(absoluteFrom(root, used.Path)))
	}
	for _, replaced := range parsed.Replace {
		old := modfile.AutoQuote(replaced.Old.Path)
		if replaced.Old.Version != "" {
			old += " " + replaced.Old.Version
		}
		replacement := replaced.New.Path
		if replaced.New.Version != "" {
			replacement = modfile.AutoQuote(replacement) + " " + replaced.New.Version
		} else {
			// A replacement with no version is a directory, which go reads from the go.work's own.
			replacement = modfile.AutoQuote(absoluteFrom(root, replacement))
		}
		fmt.Fprintf(&written, "replace %s => %s\n", old, replacement)
	}
	directory, makeError := os.MkdirTemp("", "platform-census-")
	if makeError != nil {
		return sideWorkspace{}, makeError
	}
	side := sideWorkspace{workFile: filepath.Join(directory, workFileName), directory: directory}
	if writeError := os.WriteFile(side.workFile, []byte(written.String()), 0o644); writeError != nil {
		side.remove()
		return sideWorkspace{}, writeError
	}
	sums, sumsError := os.ReadFile(filepath.Join(root, workSumName))
	if sumsError != nil && !os.IsNotExist(sumsError) {
		side.remove()
		return sideWorkspace{}, sumsError
	}
	if writeError := os.WriteFile(filepath.Join(directory, workSumName), sums, 0o644); writeError != nil {
		side.remove()
		return sideWorkspace{}, writeError
	}
	return side, nil
}

// remove deletes the side copy's two files and its directory, each by name.
func (side sideWorkspace) remove() {
	if side.directory == "" {
		return
	}
	for _, name := range []string{workFileName, workSumName} {
		// Ignored: a file the copy never wrote is no loss, and the copy lives in the temporary directory.
		_ = os.Remove(filepath.Join(side.directory, name))
	}
	// Ignored: a directory go wrote more into stays behind in the temporary directory, which is the system's to empty.
	_ = os.Remove(side.directory)
}

// absoluteFrom is path, read from directory when relative.
func absoluteFrom(directory string, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(directory, path)
}
