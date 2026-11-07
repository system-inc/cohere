package main

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
)

// folderMove is one line of the mapping: a folder, relative to the root, the folder it becomes, and the name its
// package clause takes there.
type folderMove struct {
	OldFolder  string
	NewFolder  string
	NewPackage string
}

// mapping is every move, with the module path that turns a folder into an import path.
type mapping struct {
	modulePath string
	moves      []folderMove
	// byOldFolder holds each move by its old folder, for the longest-prefix lookup.
	byOldFolder map[string]folderMove
	// newPackageByNewFolder is the package name each moved folder's clause takes, by its new folder.
	newPackageByNewFolder map[string]string
}

// readMoves reads the mapping file: old folder, new folder and new package name, tab-separated, one per line,
// with # opening a comment line.
func readMoves(mappingPath string) ([]folderMove, error) {
	file, openError := os.Open(mappingPath)
	if openError != nil {
		return nil, openError
	}
	// Ignored: the file was only read, so a failed close loses nothing.
	defer file.Close()
	var moves []folderMove
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || fields[0] == "" || fields[1] == "" || fields[2] == "" {
			return nil, fmt.Errorf(
				"%s:%d: want old folder, new folder and new package name, tab-separated",
				mappingPath,
				lineNumber,
			)
		}
		moves = append(moves, folderMove{
			OldFolder:  path.Clean(fields[0]),
			NewFolder:  path.Clean(fields[1]),
			NewPackage: fields[2],
		})
	}
	return moves, scanner.Err()
}

// newMapping checks the moves agree with each other and indexes them. A move nested in another must name its new
// folder under the outer move's new folder, so applying the outer move first and then the inner one lands where
// the line says.
func newMapping(modulePath string, moves []folderMove) (*mapping, error) {
	result := &mapping{
		modulePath:            modulePath,
		byOldFolder:           map[string]folderMove{},
		newPackageByNewFolder: map[string]string{},
	}
	for _, move := range moves {
		if _, duplicate := result.byOldFolder[move.OldFolder]; duplicate {
			return nil, fmt.Errorf("%s is moved twice", move.OldFolder)
		}
		if move.OldFolder == move.NewFolder {
			return nil, fmt.Errorf("%s moves to itself", move.OldFolder)
		}
		result.byOldFolder[move.OldFolder] = move
	}
	result.moves = append([]folderMove(nil), moves...)
	sort.SliceStable(result.moves, func(left, right int) bool {
		return strings.Count(result.moves[left].OldFolder, "/") < strings.Count(result.moves[right].OldFolder, "/")
	})
	for _, move := range result.moves {
		parentOld := path.Dir(move.OldFolder)
		if expected := result.NewFolder(parentOld); expected != path.Dir(move.NewFolder) {
			return nil, fmt.Errorf(
				"%s moves to %s, but its parent %s moves to %s",
				move.OldFolder,
				move.NewFolder,
				parentOld,
				expected,
			)
		}
		result.newPackageByNewFolder[move.NewFolder] = move.NewPackage
	}
	return result, nil
}

// NewFolder is where folder, relative to the root, lands: under the move whose old folder is its longest prefix,
// or where it is when no move holds it.
func (mapping *mapping) NewFolder(folder string) string {
	cleaned := path.Clean(folder)
	for candidate := cleaned; candidate != "." && candidate != "/"; candidate = path.Dir(candidate) {
		if move, found := mapping.byOldFolder[candidate]; found {
			return move.NewFolder + strings.TrimPrefix(cleaned, candidate)
		}
	}
	return cleaned
}

// Moved reports whether folder lies in a moved folder.
func (mapping *mapping) Moved(folder string) bool {
	return mapping.NewFolder(folder) != path.Clean(folder)
}

// NewImportPath is an import path of the root module after the moves; any other path is returned as it is.
func (mapping *mapping) NewImportPath(importPath string) string {
	if importPath == mapping.modulePath {
		return importPath
	}
	relative, inModule := strings.CutPrefix(importPath, mapping.modulePath+"/")
	if !inModule {
		return importPath
	}
	return mapping.modulePath + "/" + mapping.NewFolder(relative)
}

// NewPackagePath is a package path as go/packages spells one after the moves: an import path, or an external test
// package's, which is its folder's import path with _test after it.
func (mapping *mapping) NewPackagePath(packagePath string) string {
	if base, isExternalTest := strings.CutSuffix(
		packagePath,
		"_test",
	); isExternalTest && mapping.NewImportPath(base) != base {
		return mapping.NewImportPath(base) + "_test"
	}
	return mapping.NewImportPath(packagePath)
}

// NewPackageId is a go/packages ID after the moves: `p`, `p [q.test]`, `p_test [q.test]` or `q.test`.
func (mapping *mapping) NewPackageId(id string) string {
	packagePart, variant, hasVariant := strings.Cut(id, " [")
	result := mapping.newTestBinaryPath(packagePart)
	if hasVariant {
		testedPath := strings.TrimSuffix(variant, "]")
		result += " [" + mapping.newTestBinaryPath(testedPath) + "]"
	}
	return result
}

// newTestBinaryPath maps a package path that may be a test binary's, `q.test`.
func (mapping *mapping) newTestBinaryPath(packagePath string) string {
	if base, isBinary := strings.CutSuffix(packagePath, ".test"); isBinary {
		return mapping.NewPackagePath(base) + ".test"
	}
	return mapping.NewPackagePath(packagePath)
}

// NewPackageName is the name a package clause in folder takes after the moves, given the name it has now: the
// mapping's name for a moved folder of its own, with _test kept on an external test package, and the old name
// for everything else. It refuses a clause a moved folder's line did not expect.
func (mapping *mapping) NewPackageName(folder string, oldName string) (string, error) {
	move, moved := mapping.byOldFolder[path.Clean(folder)]
	if !moved {
		return oldName, nil
	}
	expected := path.Base(move.OldFolder)
	if move.NewPackage == "main" || oldName == "main" {
		if oldName != "main" || move.NewPackage != "main" {
			return "", fmt.Errorf("%s: package %s, and the mapping names %s", folder, oldName, move.NewPackage)
		}
		return "main", nil
	}
	switch oldName {
	case expected:
		return move.NewPackage, nil
	case expected + "_test":
		return move.NewPackage + "_test", nil
	}
	return "", fmt.Errorf(
		"%s: package %s, where the mapping expects %s or %s_test",
		folder,
		oldName,
		expected,
		expected,
	)
}

// ImportedPackageName is the name an import of importPath declares after the moves, given the name it declares
// now.
func (mapping *mapping) ImportedPackageName(importPath string, oldName string) string {
	relative, inModule := strings.CutPrefix(importPath, mapping.modulePath+"/")
	if !inModule {
		return oldName
	}
	if _, moved := mapping.byOldFolder[relative]; !moved {
		return oldName
	}
	newName, nameError := mapping.NewPackageName(relative, oldName)
	if nameError != nil {
		return oldName
	}
	return newName
}
