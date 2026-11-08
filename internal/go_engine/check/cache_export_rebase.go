package check

import (
	"bytes"
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/gcexportdata"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// goRootMarker stands for the Go installation's root in the file names of an entry computed at release, whose
// export data and Withheld would otherwise name the release machine's files: a release writes them with
// renameToGoRootMarker, and a run maps them to its own installation with renameFromGoRootMarker, so a finding that
// names a standard library position names the local file. What follows the marker is slash-separated whatever the
// release machine's separator, so entries made on one platform read on another.
const goRootMarker = "$GOROOT"

// renameExportDataFiles is exportData, as gcexportdata.Write wrote the package at packagePath, written again with
// every file it names renamed by rename: the package's own and those of the dependencies whose declarations deep
// export data carries. gcexportdata.Read has no hook for a file's name, so it reads into a private file set, which
// is copied file by file in base order under the new names, each with the base, size and lines it had, so every
// token.Pos the read package holds lands on the same line of the renamed file, and the package is written again
// from the copy. The importer gives every file it meets maxlines of one byte each and keeps no column, so a
// position read back is the file and line written, at column 1, from the original bytes as from these. A rename
// that refuses a file, export data that does not read, or a package that does not write is an error.
func renameExportDataFiles(
	exportData []byte,
	packagePath string,
	rename func(string) (string, error),
) (renamed []byte, renameError error) {
	readFileSet := token.NewFileSet()
	read, readError := gcexportdata.Read(bytes.NewReader(exportData), readFileSet, map[string]*types.Package{},
		packagePath)
	if readError != nil {
		return nil, fmt.Errorf("reading the export data of %s to rename its files: %w", packagePath, readError)
	}
	renamedFileSet := token.NewFileSet()
	readFileSet.Iterate(func(file *token.File) bool {
		name, nameError := rename(file.Name())
		if nameError != nil {
			renameError = fmt.Errorf("renaming a file of the export data of %s: %w", packagePath, nameError)
			return false
		}
		// The bases go in increasing order, each past the end of the file before it, as AddFile asks.
		copied := renamedFileSet.AddFile(name, file.Base(), file.Size())
		if !copied.SetLines(file.Lines()) {
			renameError = fmt.Errorf("renaming %s in the export data of %s: its lines do not copy", file.Name(),
				packagePath)
			return false
		}
		return true
	})
	if renameError != nil {
		return nil, renameError
	}
	// The writer panics on what it cannot carry, as writeExportData says; a package it read moments ago should never
	// be one, but a panic here must still be a refusal rather than a crash.
	defer func() {
		if recovered := recover(); recovered != nil {
			renamed = nil
			renameError = fmt.Errorf("writing the export data of %s with its files renamed: %v", packagePath, recovered)
		}
	}()
	var written bytes.Buffer
	if writeError := gcexportdata.Write(&written, renamedFileSet, read); writeError != nil {
		return nil, fmt.Errorf("writing the export data of %s with its files renamed: %w", packagePath, writeError)
	}
	return written.Bytes(), nil
}

// renameWithheldFiles is withheld with every file it names renamed by rename: each withheld error's file, and the
// file of each withheld declaration's "file:line:column". A rename that refuses a file is an error.
func renameWithheldFiles(
	withheld load.PackageWithheld,
	rename func(string) (string, error),
) (load.PackageWithheld, error) {
	renamed := load.PackageWithheld{}
	for _, withheldError := range withheld.Errors {
		file, renameError := rename(withheldError.File)
		if renameError != nil {
			return load.PackageWithheld{}, fmt.Errorf("renaming a withheld error's file: %w", renameError)
		}
		withheldError.File = file
		renamed.Errors = append(renamed.Errors, withheldError)
	}
	for _, position := range withheld.Declarations {
		file, lineAndColumn := splitPosition(position)
		renamedFile, renameError := rename(file)
		if renameError != nil {
			return load.PackageWithheld{}, fmt.Errorf("renaming a withheld declaration's file: %w", renameError)
		}
		renamed.Declarations = append(renamed.Declarations, renamedFile+lineAndColumn)
	}
	return renamed, nil
}

// splitPosition is a position as token.Position.String writes it, "file:line:column", split into its file and the
// rest, ":line:column", which is whichever of the two numbers it has. The numbers are taken from the end, since a
// file on Windows holds a colon of its own.
func splitPosition(position string) (string, string) {
	file := position
	for range 2 {
		colon := strings.LastIndexByte(file, ':')
		if colon < 0 || !isDigits(file[colon+1:]) {
			break
		}
		file = file[:colon]
	}
	return file, position[len(file):]
}

// isDigits reports whether text is one or more decimal digits.
func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// renameToGoRootMarker is the rename a release writes an entry with: a file under goRoot becomes goRootMarker and
// its slash-separated path below goRoot. A file outside goRoot is refused, since no other machine could resolve it,
// and an entry that names one must not ship.
func renameToGoRootMarker(goRoot string) func(string) (string, error) {
	root := filepath.Clean(goRoot)
	return func(file string) (string, error) {
		below, relativeError := filepath.Rel(root, file)
		if relativeError != nil || !filepath.IsAbs(file) || !filepath.IsLocal(below) || below == "." {
			return "", fmt.Errorf("naming %s by %s: it is not under %s", file, goRootMarker, root)
		}
		return goRootMarker + "/" + filepath.ToSlash(below), nil
	}
}

// renameFromGoRootMarker is the rename a run reads an entry with: a file goRootMarker names becomes the same path
// below goRoot, the run's own Go installation. A name without the marker, or whose path would leave goRoot, is
// refused, since only a release's entry is read through it and every file such an entry names is the marker's.
func renameFromGoRootMarker(goRoot string) func(string) (string, error) {
	root := filepath.Clean(goRoot)
	return func(file string) (string, error) {
		below, marked := strings.CutPrefix(file, goRootMarker+"/")
		local := filepath.Clean(filepath.FromSlash(below))
		if !marked || !filepath.IsLocal(local) || local == "." {
			return "", fmt.Errorf("resolving %s under %s: it is not a path below %s", file, root, goRootMarker)
		}
		return filepath.Join(root, local), nil
	}
}
