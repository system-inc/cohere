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

	"github.com/system-inc/cohere/internal/go_engine/timing"
	"github.com/system-inc/cohere/internal/listed_directories"
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

	// EmbedFiles are the files the package's own `//go:embed` patterns match, which go list resolves without
	// compiling. Its tests' patterns it resolves only under -test, which a runner of prebuilt tests refuses, so a test
	// variant lists the package's embedded files and not its tests' (see newNode).
	EmbedFiles []string

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
	"XTestGoFiles,TestImports,XTestImports,EmbedFiles"

// listing is every package the patterns name, everything they import, and everything their tests import, in an
// order where each package comes after what it imports.
type listing struct {
	packages []*listedPackage
	byPath   map[string]*listedPackage

	// roots are the packages the patterns matched, by import path.
	roots map[string]bool

	// testOnly are the packages listed for no reason but that a root's tests import them.
	testOnly map[string]bool

	// inProcess is set when the listing was read in this process, and goListReason, when it was not, says why go list
	// answered instead (see listInProcess).
	inProcess    bool
	goListReason string
}

// list lists the patterns in goEnvironment's workspace under its whole environment, as `go list -deps -json -e` lists
// them: read in this process when the lister can answer exactly, each file held holds read from its held bytes, and by
// go list itself, which reads the disk, when it has any doubt. The lister walks each pattern through walks, which a nil
// one walks again (PackageWalks). The go commands either runs add their CPU to collector, and so do the lister's
// goroutines.
func list(
	goEnvironment *GoEnvironment,
	patterns []string,
	held Held,
	shared *SharedReads,
	walks *PackageWalks,
	collector *timing.Collector,
) (*listing, error) {
	listed, doubt := listInProcess(goEnvironment, patterns, held, shared, walks, collector)
	if doubt == "" {
		listed.inProcess = true
		return listed, nil
	}
	listed, listError := listWithGoList(goEnvironment.guard, goEnvironment.workspace, goEnvironment.fullEnvironment,
		patterns, collector)
	if listError != nil {
		return nil, listError
	}
	listed.goListReason = doubt
	return listed, nil
}

// listWithGoList is list by `go list -deps -json -e` alone, the one go command a runner of prebuilt tests answers,
// beside guard's copy of the go.work. go list -deps follows no test's imports, and -test is refused there, so what
// only the tests import is listed a second time by name, and added after.
func listWithGoList(
	guard *workspaceSumGuard,
	workspace string,
	environment []string,
	patterns []string,
	collector *timing.Collector,
) (*listing, error) {
	listed, listError := runList(guard, workspace, environment, patterns, collector)
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
	extra, extraError := runList(guard, workspace, environment, testImports, collector)
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

// runList runs go list once beside guard's copy of the go.work and reads what it printed, dependencies first, adding go
// list's CPU to collector.
func runList(
	guard *workspaceSumGuard,
	workspace string,
	environment []string,
	patterns []string,
	collector *timing.Collector,
) ([]*listedPackage, error) {
	// No "--" before the patterns: a runner's stand-in go refuses any flag it does not know, that one too.
	arguments := append([]string{"list", "-e", "-deps", "-json=" + listedFields}, patterns...)
	command := exec.Command("go", arguments...)
	command.Dir = workspace
	// PWD names the directory as this process spells it, so the positions go list writes relative to it are
	// relative to the workspace as the load names it, through a symbolic link too (/tmp is /private/tmp on a Mac).
	command.Env = append(slices.Clone(environment), "PWD="+workspace)
	var output []byte
	var standardError []byte
	var runError error
	measureGoCommands(collector, func() { output, standardError, runError = guard.output(command) })
	if runError != nil {
		return nil, fmt.Errorf("listing %s with go list in %s: %w: %s", strings.Join(patterns, " "), workspace,
			runError, strings.TrimSpace(string(standardError)))
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
	// What go list read in its process, read again in this one, so a test that loads fixtures has its result keyed
	// on them (#cmpsz8p).
	reads := make([]listed_directories.Package, 0, len(listed))
	for _, listedPackage := range listed {
		each := listed_directories.Package{
			ImportPath: listedPackage.ImportPath,
			Directory:  listedPackage.Directory,
			Root:       !listedPackage.DepOnly,
		}
		if listedPackage.Module != nil && listedPackage.Module.Main {
			each.MainModule, each.MainGoMod = true, listedPackage.Module.GoMod
		}
		reads = append(reads, each)
	}
	listed_directories.Open(workspace, patterns, reads)
	return listed, nil
}

// listCompiled runs `go list -e -compiled` once in workspace for every listed package with cgo files, the
// dependencies' and the standard library's too, so the cgo tool translates each, and returns what it said of each by
// import path. environment is the whole environment, the one translatedEnvironment gives, and -pgo=off is asked as
// go/packages asks it, so a command with a default.pgo profile is not listed twice. No -deps and no -test: the graph,
// its test variants among it, is the plain listing's, and only the compiled files are read from this one. Nothing of
// it is kept with go list's kept answer, whose shape stays the plain listing's, so the translation runs every run. It
// runs beside guard's copy of the go.work, as every go list of a load does.
func listCompiled(
	guard *workspaceSumGuard,
	workspace string,
	environment []string,
	listed *listing,
) (map[string]*compiledPackage, error) {
	var paths []string
	compiled := map[string]*compiledPackage{}
	for _, listedPackage := range listed.packages {
		if len(listedPackage.CgoFiles) > 0 {
			paths = append(paths, listedPackage.ImportPath)
			compiled[listedPackage.ImportPath] = &compiledPackage{ImportPath: listedPackage.ImportPath}
		}
	}
	if len(paths) == 0 {
		return compiled, nil
	}
	arguments := append([]string{"list", "-e", "-compiled", "-pgo=off", "-json=ImportPath,CompiledGoFiles,Error"},
		paths...)
	command := exec.Command("go", arguments...)
	command.Dir = workspace
	// PWD as in runList, so a position go list writes relative to the workspace is relative to it as the load names it.
	command.Env = append(slices.Clone(environment), "PWD="+workspace)
	output, standardError, runError := guard.output(command)
	if runError != nil && !cgoFailureOnly(string(standardError)) {
		return nil, fmt.Errorf("translating the cgo files of %s with go list -compiled in %s: %w: %s",
			strings.Join(paths, " "), workspace, runError, strings.TrimSpace(string(standardError)))
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		each := &compiledPackage{}
		if decodeError := decoder.Decode(each); errors.Is(decodeError, io.EOF) {
			break
		} else if decodeError != nil {
			return nil, fmt.Errorf("reading go list -compiled's output in %s: %w", workspace, decodeError)
		}
		if compiled[each.ImportPath] == nil {
			return nil, fmt.Errorf("go list -compiled in %s printed %s, which was not asked for", workspace,
				each.ImportPath)
		}
		listedPackage := listed.byPath[each.ImportPath]
		// go list names the package's own files relative to its directory and the cgo tool's outputs absolute. Any
		// file with an extension other than .go is left out, as go/packages leaves it out (golang.org/issue/28749):
		// the outputs have none.
		var files []string
		for _, file := range each.CompiledGoFiles {
			if extension := filepath.Ext(file); extension != ".go" && extension != "" {
				continue
			}
			if !filepath.IsAbs(file) {
				file = filepath.Join(listedPackage.Directory, file)
			}
			files = append(files, file)
		}
		each.CompiledGoFiles = files
		compiled[each.ImportPath] = each
	}
	// A package go list neither translated nor gave an error for, as one it left out of its answer when a dependency's
	// C compiler failed, is a translation that failed all the same, and says so, with what go printed, rather than
	// being checked with its package "C" faked and nothing said.
	said := strings.TrimSpace(string(standardError))
	for _, path := range paths {
		if each := compiled[path]; each.Error == nil && !each.translated(listed.byPath[path]) {
			each.Error = &listedError{Message: "go list -compiled gave no translation of the package's cgo files"}
			if said != "" {
				each.Error.Message += ": " + said
			}
		}
	}
	return compiled, nil
}

// cgoFailureOnly reports whether go list's standard error is only a package's C compiler failing, "# path" and then
// the compiler's words, which go list -e reports in the package's Error and yet exits non-zero for, as go/packages
// reads it: the answer on standard output is whole, and the failure is the package's diagnostic.
func cgoFailureOnly(standardError string) bool {
	header, _, found := strings.Cut(standardError, "\n")
	path, isHeader := strings.CutPrefix(header, "# ")
	return found && isHeader && path != "" && !strings.ContainsAny(path, " \t")
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
