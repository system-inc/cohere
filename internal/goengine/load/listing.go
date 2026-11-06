package load

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// listedPackage is what `go list -deps -json -e` says of one package, the fields the load reads. Module decodes
// into go/packages' own type, as go/packages decodes it, so a package's Module is the one go/packages gave.
type listedPackage struct {
	ImportPath string
	Name       string
	Directory  string `json:"Dir"`
	DepOnly    bool

	GoFiles           []string
	CgoFiles          []string
	IgnoredGoFiles    []string
	IgnoredOtherFiles []string
	CFiles            []string
	CxxFiles          []string `json:"CXXFiles"`
	MFiles            []string
	HFiles            []string
	FFiles            []string
	SFiles            []string
	SwigFiles         []string
	SwigCxxFiles      []string `json:"SwigCXXFiles"`
	SysoFiles         []string

	Imports   []string
	ImportMap map[string]string
	Deps      []string
	Module    *packages.Module
	Error     *listedError

	TestGoFiles  []string
	XTestGoFiles []string
	TestImports  []string
	XTestImports []string
}

// listedError is an error go list reports for a package. go list names the text Err and the position Pos, and the
// tags keep those keys.
type listedError struct {
	Position    string `json:"Pos"`
	Message     string `json:"Err"`
	ImportStack []string
}

// listedFields are the fields asked of go list, so it computes nothing else.
var listedFields = "ImportPath,Name,Dir,DepOnly,GoFiles,CgoFiles,IgnoredGoFiles,IgnoredOtherFiles,CFiles,CXXFiles," +
	"MFiles,HFiles,FFiles,SFiles,SwigFiles,SwigCXXFiles,SysoFiles,Imports,ImportMap,Deps,Module,Error,TestGoFiles," +
	"XTestGoFiles,TestImports,XTestImports"

// listing is every package the patterns name, everything they import, and everything their tests import, in an
// order where each package comes after what it imports.
type listing struct {
	packages []*listedPackage
	byPath   map[string]*listedPackage

	// roots are the packages the patterns matched, by import path.
	roots map[string]bool

	// testOnly are the packages listed for no reason but that a root's tests import them.
	testOnly map[string]bool
}

// list lists the patterns in workspace under environment, the whole environment, with `go list -deps -json -e`
// alone, the one go command a runner of prebuilt tests answers. go list -deps follows no test's imports, and -test
// is refused there, so what only the tests import is listed a second time by name, and added after.
func list(workspace string, environment []string, patterns []string) (*listing, error) {
	listed, listError := runList(workspace, environment, patterns)
	if listError != nil {
		return nil, listError
	}
	result := &listing{byPath: map[string]*listedPackage{}, roots: map[string]bool{}, testOnly: map[string]bool{}}
	result.add(listed)
	for _, listedPackage := range result.packages {
		if !listedPackage.DepOnly {
			result.roots[listedPackage.ImportPath] = true
		}
	}
	var testImports []string
	for _, listedPackage := range result.packages {
		if !result.roots[listedPackage.ImportPath] {
			continue
		}
		for _, imported := range slices.Concat(listedPackage.TestImports, listedPackage.XTestImports) {
			imported = canonical(listedPackage, imported)
			if imported != "C" && result.byPath[imported] == nil && !slices.Contains(testImports, imported) {
				testImports = append(testImports, imported)
			}
		}
	}
	if len(testImports) == 0 {
		return result, nil
	}
	slices.Sort(testImports)
	extra, extraError := runList(workspace, environment, testImports)
	if extraError != nil {
		return nil, extraError
	}
	before := len(result.packages)
	result.add(extra)
	for _, listedPackage := range result.packages[before:] {
		result.testOnly[listedPackage.ImportPath] = true
	}
	result.placeTestImportErrors()
	return result, nil
}

// add appends each of listed the listing does not hold yet. go list can print one import path twice, once with an
// error, and as go/packages does, the one without is kept.
func (listing *listing) add(listed []*listedPackage) {
	for _, listedPackage := range listed {
		earlier := listing.byPath[listedPackage.ImportPath]
		if earlier != nil && (earlier.Error == nil || listedPackage.Error != nil) {
			continue
		}
		if earlier != nil {
			*earlier = *listedPackage
			continue
		}
		listing.packages = append(listing.packages, listedPackage)
		listing.byPath[listedPackage.ImportPath] = listedPackage
	}
}

// runList runs go list once and reads what it printed, dependencies first.
func runList(workspace string, environment []string, patterns []string) ([]*listedPackage, error) {
	// No "--" before the patterns: a runner's stand-in go refuses any flag it does not know, that one too.
	arguments := append([]string{"list", "-e", "-deps", "-json=" + listedFields}, patterns...)
	command := exec.Command("go", arguments...)
	command.Dir = workspace
	// PWD names the directory as this process spells it, so the positions go list writes relative to it are
	// relative to the workspace as the load names it, through a symbolic link too (/tmp is /private/tmp on a Mac).
	command.Env = append(slices.Clone(environment), "PWD="+workspace)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("listing %s with go list in %s: %w: %s", strings.Join(patterns, " "), workspace,
			runError, strings.TrimSpace(standardError.String()))
	}
	var listed []*listedPackage
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		listedPackage := &listedPackage{}
		if decodeError := decoder.Decode(listedPackage); errors.Is(decodeError, io.EOF) {
			break
		} else if decodeError != nil {
			return nil, fmt.Errorf("reading go list's output in %s: %w", workspace, decodeError)
		}
		if listedPackage.ImportPath == "" {
			return nil, fmt.Errorf("go list in %s printed a package with no import path: %+v", workspace,
				listedPackage)
		}
		listed = append(listed, listedPackage)
	}
	return listed, nil
}

// canonical is the import path a package's import of path resolves to.
func canonical(importer *listedPackage, path string) string {
	if resolved, found := importer.ImportMap[path]; found {
		return resolved
	}
	return path
}

// placeTestImportErrors places the error of each package listed only for a test's import, which go list wrote
// with no position since a pattern named it, where go test would: at the first test file that imports it, in the
// first root whose tests do, the in-package tests before the external ones.
func (listing *listing) placeTestImportErrors() {
	for _, listedPackage := range listing.packages {
		nothingToPlace := listedPackage.Error == nil || listedPackage.Error.Position != ""
		if !listing.testOnly[listedPackage.ImportPath] || nothingToPlace {
			continue
		}
		for _, root := range listing.packages {
			if !listing.roots[root.ImportPath] {
				continue
			}
			if position := importPosition(root, listedPackage.ImportPath); position != "" {
				// The stack names the test that imports it, as go list's does, so the error stays the missing
				// package's own and its file is not taken for the package's (see addFileFromError).
				listedPackage.Error.Position = position
				listedPackage.Error.ImportStack = []string{root.ImportPath + " (test)"}
				break
			}
		}
	}
}

// importPosition is "file:line:column" of the first of root's test files that imports path, by its import path's
// literal as go list places an import's error, or empty when none does.
func importPosition(root *listedPackage, path string) string {
	fileSet := token.NewFileSet()
	for _, name := range slices.Concat(root.TestGoFiles, root.XTestGoFiles) {
		// Ignored: a header that does not parse still yields the imports read before the error, and the load
		// reports the error itself when it parses the file whole.
		header, _ := parser.ParseFile(fileSet, filepath.Join(root.Directory, name), nil, parser.ImportsOnly)
		if header == nil {
			continue
		}
		for _, imported := range header.Imports {
			written, unquoteError := strconv.Unquote(imported.Path.Value)
			if unquoteError == nil && canonical(root, written) == path {
				return fileSet.Position(imported.Path.Pos()).String()
			}
		}
	}
	return ""
}
