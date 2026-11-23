// Command go_libraries copies the third-party Go modules that hand go/types values across, golang.org/x/tools,
// honnef.co/go/tools and golang.org/x/exp/typeparams, from the module cache into libraries/go, with every import of
// go/types rewritten to cohere's fork, github.com/system-inc/go-types/types. Go cannot mix two type universes: a
// *types.Package from the fork is not a *go/types.Package, so every package that hands types to cohere's engine must
// check against the same one, and cohere's go.mod replaces each module with its copy here (#cs9qm4x).
//
// Run it from cohere's root, after every bump of one of the three modules in go.mod:
//
//	go run ./tools/go_libraries
//
// It copies only the packages cohere needs: every package of the three modules a Go file cohere owns imports, or a
// go.mod in go.work names as a tool, and every package of theirs those import in turn, to a fixed point. Each package
// brings its non-test files, every other file beside them (embedded data, assembly) and its whole testdata, which
// cohere's parity tests read; test files stay behind, since nothing here compiles them and a copied test would claim a
// proof nobody runs. A test of cohere's that reads a package's fixtures without importing it names that package in
// fixtureReaders. Each module keeps its root files (go.mod, go.sum and its license and patent grant), its go.mod given
// one more require, the fork's, and a ledger, `COPIED.md`, naming the version, the packages and every rewrite. A
// second run leaves every file as the first wrote it.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/streams"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// library is one module copied: its path, and the directory under cohere's root that holds its copy, named for the
// module's repository (libraries/<language>/<repository>).
type library struct {
	modulePath  string
	destination string
}

// libraries are the modules copied, and their destinations are the only directories this command removes a file
// from or writes one to.
var libraries = []library{
	{modulePath: "golang.org/x/tools", destination: "libraries/go/tools"},
	{modulePath: "honnef.co/go/tools", destination: "libraries/go/go-tools"},
	{modulePath: "golang.org/x/exp/typeparams", destination: "libraries/go/exp/typeparams"},
}

const (
	// command is how the ledger says its copy was made.
	command = "go run ./tools/go_libraries"
	// cohereModulePath is the module whose root this command runs from.
	cohereModulePath = "github.com/system-inc/cohere"
	// standardTypesPath is the import every copied file names the fork instead of.
	standardTypesPath = "go/types"
	// forkModulePath and forkModuleVersion are the fork's module as each copied go.mod requires it; cohere's go.mod
	// replaces it with its submodule, libraries/go/go-types.
	forkModulePath    = "github.com/system-inc/go-types"
	forkModuleVersion = "v0.0.0"
	// forkTypesPath is the fork's go/types package.
	forkTypesPath = forkModulePath + "/types"
	// ledgerName is each copy's ledger, written beside its go.mod.
	ledgerName = "COPIED.md"
	// testdataName is the directory go ignores and tests read fixtures from, copied whole.
	testdataName = "testdata"
)

// rootFiles are the files of a module's root every copy keeps, whichever the module has. `LICENSE-THIRD-PARTY`
// is staticcheck's credit for the code it includes, which cohere's notices carry on.
var rootFiles = []string{"go.mod", "go.sum", "LICENSE", "LICENSE-THIRD-PARTY", "PATENTS"}

// standardTypesHandlers are imports that hand back the standard library's go/types values, which a copied file cannot
// keep, since what they return is not the fork's: a copied package importing one fails the run with its file named.
var standardTypesHandlers = []string{"go/importer", "go/internal/"}

// fixtureReader is a test of cohere's that reads the testdata of packages it does not import, so no import of cohere's
// names them: the module, the directories it reads as patterns path.Match knows, relative to the module's root, and
// the test, named in the ledger. Each package whose testdata matches is copied whole, like any package imported.
type fixtureReader struct {
	modulePath string
	patterns   []string
	test       string
}

// fixtureReaders are the tests of cohere's that read a module's fixtures by pattern rather than by import. The
// staticcheck_wants reader counts the diag notes of every check's fixtures staticcheck has, quickfix's and sa5011's
// among them, which no port imports.
var fixtureReaders = []fixtureReader{{
	modulePath: "honnef.co/go/tools",
	patterns:   []string{"staticcheck/*/testdata", "simple/*/testdata", "stylecheck/*/testdata", "quickfix/*/testdata"},
	test:       "internal/go_engine/rules/staticcheck_wants/staticcheck_fixtures_test.go",
}}

// source is one library as the module cache holds it.
type source struct {
	library   library
	version   string
	directory string
}

// copiedModule is what one library's copy holds: its packages, its files by path relative to the module's root,
// every rewrite as the file and line it changed, and the text appended to its go.mod.
type copiedModule struct {
	source        source
	packages      []string
	files         map[string][]byte
	rewrites      []string
	goModAddition string
}

func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code go_libraries exits with, once streams.Finish has delivered what it printed.
func finishedExitCode() int {
	// Deferred calls run while a panic unwinds, though not on os.Exit, so what was written before a panic is shown.
	defer func() {
		streams.Show(streams.Errors)
		streams.Show(streams.Output)
	}()
	return streams.Finish(runCommandLine())
}

// runCommandLine reads the flags, runs the copy, and returns the code to exit with.
func runCommandLine() int {
	root := flag.String("root", ".", "cohere's root, holding go.mod and go.work")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintf(streams.Errors, "go_libraries: takes no arguments, given %q\n", flag.Args())
		return 2
	}
	if runError := run(*root); runError != nil {
		fmt.Fprintf(streams.Errors, "go_libraries: %v\n", runError)
		return 1
	}
	return 0
}

// run copies every library into its destination under root.
func run(root string) error {
	absoluteRoot, absoluteError := filepath.Abs(root)
	if absoluteError != nil {
		return absoluteError
	}
	rootModule, rootModuleError := readModuleFile(filepath.Join(absoluteRoot, "go.mod"))
	if rootModuleError != nil {
		return rootModuleError
	}
	if rootModule.Module == nil || rootModule.Module.Mod.Path != cohereModulePath {
		return fmt.Errorf("%s is not cohere's root: its go.mod is not module %s", absoluteRoot, cohereModulePath)
	}
	moduleCache, moduleCacheError := goEnvironment(absoluteRoot, "GOMODCACHE")
	if moduleCacheError != nil {
		return moduleCacheError
	}
	sources := []source{}
	for _, library := range libraries {
		librarySource, sourceError := cachedSource(rootModule, moduleCache, library)
		if sourceError != nil {
			return sourceError
		}
		sources = append(sources, librarySource)
	}
	imported, importedError := cohereImports(absoluteRoot)
	if importedError != nil {
		return importedError
	}
	for _, reader := range fixtureReaders {
		packages, fixtureError := fixturePackages(sources, reader)
		if fixtureError != nil {
			return fixtureError
		}
		imported = append(imported, packages...)
	}
	copies, closureError := copyClosure(sources, imported)
	if closureError != nil {
		return closureError
	}
	for _, copied := range copies {
		written, removed, writeError := writeCopy(absoluteRoot, copied)
		if writeError != nil {
			return writeError
		}
		fmt.Fprintf(streams.Output, "%s %s: %d packages, %d files, %d rewrites, into %s (%d written, %d removed)\n",
			copied.source.library.modulePath, copied.source.version, len(copied.packages), len(copied.files),
			len(copied.rewrites), copied.source.library.destination, written, removed)
	}
	return nil
}

// readModuleFile parses the go.mod at filePath.
func readModuleFile(filePath string) (*modfile.File, error) {
	contents, readError := os.ReadFile(filePath)
	if readError != nil {
		return nil, readError
	}
	return modfile.Parse(filePath, contents, nil)
}

// goEnvironment is the go command's value for name, asked in directory.
func goEnvironment(directory string, name string) (string, error) {
	output, outputError := commandOutput(directory, "go", "env", name)
	if outputError != nil {
		return "", outputError
	}
	value := strings.TrimSpace(output)
	if value == "" {
		return "", fmt.Errorf("go env %s printed nothing", name)
	}
	return value, nil
}

// commandOutput runs name with arguments in directory and returns what it printed, or an error carrying what it
// printed to stderr.
func commandOutput(directory string, name string, arguments ...string) (string, error) {
	process := exec.Command(name, arguments...)
	process.Dir = directory
	var standardError bytes.Buffer
	process.Stderr = &standardError
	output, runError := process.Output()
	if runError != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(arguments, " "), runError,
			strings.TrimSpace(standardError.String()))
	}
	return string(output), nil
}

// cachedSource finds library's module in the module cache at the version cohere's go.mod requires. It reads the
// cache's directory rather than asking go list, which answers with the copy itself once go.mod replaces the module.
func cachedSource(rootModule *modfile.File, moduleCache string, library library) (source, error) {
	version := ""
	for _, requirement := range rootModule.Require {
		if requirement.Mod.Path == library.modulePath {
			version = requirement.Mod.Version
		}
	}
	if version == "" {
		return source{}, fmt.Errorf("cohere's go.mod requires no version of %s", library.modulePath)
	}
	escapedPath, escapePathError := module.EscapePath(library.modulePath)
	if escapePathError != nil {
		return source{}, escapePathError
	}
	escapedVersion, escapeVersionError := module.EscapeVersion(version)
	if escapeVersionError != nil {
		return source{}, escapeVersionError
	}
	directory := filepath.Join(moduleCache, filepath.FromSlash(escapedPath)+"@"+escapedVersion)
	cachedModule, cachedModuleError := readModuleFile(filepath.Join(directory, "go.mod"))
	if errors.Is(cachedModuleError, fs.ErrNotExist) {
		return source{}, fmt.Errorf("%s@%s is not in the module cache: `go mod download %s@%s` fetches it",
			library.modulePath, version, library.modulePath, version)
	}
	if cachedModuleError != nil {
		return source{}, cachedModuleError
	}
	if cachedModule.Module == nil || cachedModule.Module.Mod.Path != library.modulePath {
		return source{}, fmt.Errorf("%s does not declare module %s", directory, library.modulePath)
	}
	return source{library: library, version: version, directory: directory}, nil
}

// cohereImports is every import path a Go file cohere owns names, tests included, and every tool a go.mod in go.work
// names. Cohere owns its tracked Go files outside TypeScript (upstream's submodule), libraries (copies like these)
// and any testdata (fixtures a linter reads, not code cohere builds).
func cohereImports(root string) ([]string, error) {
	listed, listError := commandOutput(root, "git", "ls-files", "-z", "--", "*.go")
	if listError != nil {
		return nil, listError
	}
	imported := map[string]bool{}
	for _, file := range strings.Split(listed, "\x00") {
		elements := strings.Split(file, "/")
		if file == "" || elements[0] == "TypeScript" || elements[0] == "libraries" ||
			slices.Contains(elements, testdataName) {
			continue
		}
		contents, readError := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if readError != nil {
			return nil, readError
		}
		paths, importsError := importPaths(file, contents)
		if importsError != nil {
			return nil, importsError
		}
		for _, importPath := range paths {
			imported[importPath] = true
		}
	}
	workspaceContents, readWorkspaceError := os.ReadFile(filepath.Join(root, "go.work"))
	if readWorkspaceError != nil {
		return nil, readWorkspaceError
	}
	workspace, workspaceError := modfile.ParseWork("go.work", workspaceContents, nil)
	if workspaceError != nil {
		return nil, workspaceError
	}
	for _, use := range workspace.Use {
		used, usedError := readModuleFile(filepath.Join(root, filepath.FromSlash(use.Path), "go.mod"))
		if usedError != nil {
			return nil, usedError
		}
		for _, tool := range used.Tool {
			imported[tool.Path] = true
		}
	}
	return sortedKeys(imported), nil
}

// fixturePackages is every package of reader's module whose directory holds a testdata reader's patterns match, in
// the module cache's source.
func fixturePackages(sources []source, reader fixtureReader) ([]string, error) {
	readSource, owned := owner(sources, reader.modulePath)
	if !owned || readSource.library.modulePath != reader.modulePath {
		return nil, fmt.Errorf("%s reads fixtures of %s, which is not a library copied", reader.test, reader.modulePath)
	}
	packages := []string{}
	for _, pattern := range reader.patterns {
		matches, globError := filepath.Glob(filepath.Join(readSource.directory, filepath.FromSlash(pattern)))
		if globError != nil {
			return nil, globError
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("%s reads %s of %s@%s, which matches nothing there", reader.test, pattern,
				reader.modulePath, readSource.version)
		}
		for _, match := range matches {
			relative, relativeError := filepath.Rel(readSource.directory, filepath.Dir(match))
			if relativeError != nil {
				return nil, relativeError
			}
			packages = append(packages, reader.modulePath+"/"+filepath.ToSlash(relative))
		}
	}
	return packages, nil
}

// importPaths is every path the Go file named file imports.
func importPaths(file string, contents []byte) ([]string, error) {
	parsed, parseError := parser.ParseFile(token.NewFileSet(), file, contents, parser.ImportsOnly)
	if parseError != nil {
		return nil, parseError
	}
	paths := []string{}
	for _, spec := range parsed.Imports {
		importPath, unquoteError := strconv.Unquote(spec.Path.Value)
		if unquoteError != nil {
			return nil, fmt.Errorf("%s: %w", file, unquoteError)
		}
		paths = append(paths, importPath)
	}
	return paths, nil
}

// owner is the source among sources whose module holds importPath, if any does.
func owner(sources []source, importPath string) (source, bool) {
	for _, candidate := range sources {
		if importPath == candidate.library.modulePath ||
			strings.HasPrefix(importPath, candidate.library.modulePath+"/") {
			return candidate, true
		}
	}
	return source{}, false
}

// copyClosure gathers each library's copy: the packages of its module that imported names, and every package of any
// of the three modules a copied package imports, to a fixed point. Staticcheck's packages import x/tools' and
// typeparams', and go.mod replaces each module whole, so a package any copy needs must be in its module's copy.
func copyClosure(sources []source, imported []string) ([]*copiedModule, error) {
	copies := map[string]*copiedModule{}
	for _, librarySource := range sources {
		files, goModAddition, rootError := moduleRootFiles(librarySource)
		if rootError != nil {
			return nil, rootError
		}
		copies[librarySource.library.modulePath] = &copiedModule{source: librarySource, files: files,
			goModAddition: goModAddition}
	}
	queued := map[string]bool{}
	pending := []string{}
	for _, importPath := range imported {
		if _, owned := owner(sources, importPath); owned && !queued[importPath] {
			queued[importPath] = true
			pending = append(pending, importPath)
		}
	}
	for len(pending) > 0 {
		importPath := pending[0]
		pending = pending[1:]
		packageSource, _ := owner(sources, importPath)
		copied := copies[packageSource.library.modulePath]
		read, packageError := readPackage(packageSource, importPath)
		if packageError != nil {
			return nil, packageError
		}
		for name, contents := range read.files {
			// A module's root package shares its directory with the root files, already copied, go.mod with the
			// fork's require among them, so those keep the copy moduleRootFiles made.
			if !slices.Contains(rootFiles, name) {
				copied.files[name] = contents
			}
		}
		copied.packages = append(copied.packages, importPath)
		copied.rewrites = append(copied.rewrites, read.rewrites...)
		for _, next := range read.imports {
			if _, owned := owner(sources, next); owned && !queued[next] {
				queued[next] = true
				pending = append(pending, next)
			}
		}
	}
	result := []*copiedModule{}
	for _, librarySource := range sources {
		copied := copies[librarySource.library.modulePath]
		slices.Sort(copied.packages)
		slices.Sort(copied.rewrites)
		if _, collides := copied.files[ledgerName]; collides {
			return nil, fmt.Errorf("%s holds a %s of its own at its root, which the ledger would overwrite",
				librarySource.library.modulePath, ledgerName)
		}
		copied.files[ledgerName] = ledger(copied)
		result = append(result, copied)
	}
	return result, nil
}

// moduleRootFiles is the root files of librarySource's module, its go.mod requiring the fork, which every rewritten
// import names, and the text appended to go.mod to say so. The require is appended, so every byte upstream wrote stays
// as it was.
func moduleRootFiles(librarySource source) (map[string][]byte, string, error) {
	files := map[string][]byte{}
	for _, name := range rootFiles {
		contents, readError := os.ReadFile(filepath.Join(librarySource.directory, name))
		if errors.Is(readError, fs.ErrNotExist) && name != "go.mod" {
			continue
		}
		if readError != nil {
			return nil, "", readError
		}
		files[name] = contents
	}
	parsed, parseError := modfile.Parse("go.mod", files["go.mod"], nil)
	if parseError != nil {
		return nil, "", parseError
	}
	for _, requirement := range parsed.Require {
		if requirement.Mod.Path == forkModulePath {
			return nil, "", fmt.Errorf("%s's go.mod already requires %s", librarySource.library.modulePath,
				forkModulePath)
		}
	}
	addition := "\n// cohere: the fork of go/types every copied import of go/types names instead, which cohere's " +
		"go.mod\n// replaces with libraries/go/go-types (tools/go_libraries).\n" +
		"require " + forkModulePath + " " + forkModuleVersion + "\n"
	rewritten := slices.Clone(files["go.mod"])
	if !bytes.HasSuffix(rewritten, []byte("\n")) {
		rewritten = append(rewritten, '\n')
	}
	rewritten = append(rewritten, addition...)
	if _, reparseError := modfile.Parse("go.mod", rewritten, nil); reparseError != nil {
		return nil, "", fmt.Errorf("%s's go.mod with the fork required: %w", librarySource.library.modulePath,
			reparseError)
	}
	files["go.mod"] = rewritten
	return files, addition, nil
}

// copiedPackage is what one package brings to its module's copy: its files by path relative to the module's root,
// each rewrite as the file and line it changed, and the imports a build of it follows.
type copiedPackage struct {
	files    map[string][]byte
	rewrites []string
	imports  []string
}

// readPackage reads the package at importPath from packageSource: its non-test files, every other regular file in
// its directory, and its whole testdata, with every import its non-test Go files name under every build constraint,
// since the copy must build on every platform cohere builds for.
func readPackage(packageSource source, importPath string) (copiedPackage, error) {
	relative := strings.TrimPrefix(strings.TrimPrefix(importPath, packageSource.library.modulePath), "/")
	directory := filepath.Join(packageSource.directory, filepath.FromSlash(relative))
	entries, readError := os.ReadDir(directory)
	if errors.Is(readError, fs.ErrNotExist) {
		return copiedPackage{}, fmt.Errorf("%s is imported, but %s@%s has no directory %s: a package of another "+
			"module, nested in its path, which this command does not copy", importPath,
			packageSource.library.modulePath, packageSource.version, relative)
	}
	if readError != nil {
		return copiedPackage{}, readError
	}
	read := copiedPackage{files: map[string][]byte{}}
	goFiles := 0
	for _, entry := range entries {
		name := path.Join(relative, entry.Name())
		if entry.IsDir() {
			if entry.Name() == testdataName {
				testdata, testdataError := testdataFiles(packageSource, name)
				if testdataError != nil {
					return copiedPackage{}, testdataError
				}
				maps.Copy(read.files, testdata)
			}
			// Any other directory is another package, copied when something imports it, or holds nothing a
			// copied package builds from.
			continue
		}
		if !entry.Type().IsRegular() || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		contents, fileError := os.ReadFile(filepath.Join(directory, entry.Name()))
		if fileError != nil {
			return copiedPackage{}, fileError
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			goFiles++
			file, rewriteError := rewriteImports(packageSource, name, contents)
			if rewriteError != nil {
				return copiedPackage{}, rewriteError
			}
			handRun, handRunError := isHandRunProgram(name, contents)
			if handRunError != nil {
				return copiedPackage{}, handRunError
			}
			if !handRun {
				read.imports = append(read.imports, file.imports...)
			}
			read.rewrites = append(read.rewrites, file.rewrites...)
			contents = file.contents
		}
		read.files[name] = contents
	}
	if goFiles == 0 {
		return copiedPackage{}, fmt.Errorf("%s is imported, but %s holds no Go file that is not a test", importPath,
			directory)
	}
	return read, nil
}

// testdataFiles is every regular file under the testdata directory relative names in packageSource, byte for byte:
// they are fixtures a linter reads, not code cohere builds, so their imports of go/types stay upstream's.
func testdataFiles(packageSource source, relative string) (map[string][]byte, error) {
	files := map[string][]byte{}
	walkError := filepath.WalkDir(filepath.Join(packageSource.directory, filepath.FromSlash(relative)),
		func(filePath string, entry fs.DirEntry, pathError error) error {
			if pathError != nil || entry.IsDir() {
				return pathError
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("%s is neither a file nor a directory, which no module zip holds", filePath)
			}
			name, relativeError := filepath.Rel(packageSource.directory, filePath)
			if relativeError != nil {
				return relativeError
			}
			contents, readError := os.ReadFile(filePath)
			if readError != nil {
				return readError
			}
			files[filepath.ToSlash(name)] = contents
			return nil
		})
	return files, walkError
}

// rewrittenFile is a copied Go file as rewriteImports leaves it: the imports it names, its contents, and each rewrite
// as the file and line it changed.
type rewrittenFile struct {
	imports  []string
	contents []byte
	rewrites []string
}

// rewriteImports reads the Go file name of packageSource and makes each of its imports of go/types the fork's. Only
// the path literal's bytes change, so any name the spec gives and every other byte stay upstream's. A file importing a
// package that hands back the standard library's types fails.
func rewriteImports(packageSource source, name string, contents []byte) (rewrittenFile, error) {
	files := token.NewFileSet()
	parsed, parseError := parser.ParseFile(files, name, contents, parser.ImportsOnly)
	if parseError != nil {
		return rewrittenFile{}, fmt.Errorf("%s@%s: %w", packageSource.library.modulePath, packageSource.version,
			parseError)
	}
	file := rewrittenFile{contents: slices.Clone(contents)}
	// From the last import to the first, so a replacement's change of length leaves the earlier offsets true.
	for index := len(parsed.Imports) - 1; index >= 0; index-- {
		spec := parsed.Imports[index]
		importPath, unquoteError := strconv.Unquote(spec.Path.Value)
		if unquoteError != nil {
			return rewrittenFile{}, fmt.Errorf("%s: %w", name, unquoteError)
		}
		file.imports = append(file.imports, importPath)
		for _, handler := range standardTypesHandlers {
			if importPath == strings.TrimSuffix(handler, "/") || strings.HasPrefix(importPath, handler) {
				return rewrittenFile{}, fmt.Errorf("%s/%s imports %s, which hands back the standard library's "+
					"go/types values, not the fork's", packageSource.library.destination, name, importPath)
			}
		}
		if importPath != standardTypesPath {
			continue
		}
		start, end := files.Position(spec.Path.Pos()), files.Position(spec.Path.End())
		file.contents = slices.Concat(file.contents[:start.Offset], []byte(strconv.Quote(forkTypesPath)),
			file.contents[end.Offset:])
		file.rewrites = append(file.rewrites, name+":"+strconv.Itoa(start.Line))
	}
	return file, nil
}

// isHandRunProgram is whether the Go file name is constrained to the build tag ignore, by convention a program run by
// hand (`go run gen.go`) that no build of its package compiles. It is copied and rewritten like its package's other
// files, but its imports are not followed: they pull in what only the program needs, x/tools' whole vet suite for
// unitchecker's main.go among them.
func isHandRunProgram(name string, contents []byte) (bool, error) {
	parsed, parseError := parser.ParseFile(token.NewFileSet(), name, contents, parser.ImportsOnly|parser.ParseComments)
	if parseError != nil {
		return false, parseError
	}
	for _, group := range parsed.Comments {
		if group.Pos() > parsed.Package {
			break
		}
		for _, comment := range group.List {
			if !constraint.IsGoBuild(comment.Text) {
				continue
			}
			expression, constraintError := constraint.Parse(comment.Text)
			if constraintError != nil {
				return false, fmt.Errorf("%s: %w", name, constraintError)
			}
			tag, isTag := expression.(*constraint.TagExpr)
			return isTag && tag.Tag == "ignore", nil
		}
	}
	return false, nil
}

// ledger is the `COPIED.md` that says where copied came from, how, and what it changed, sorted and without a date, so
// a rerun over the same versions writes it byte for byte again.
func ledger(copied *copiedModule) []byte {
	var text strings.Builder
	modulePath, version := copied.source.library.modulePath, copied.source.version
	fmt.Fprintf(&text, "# %s %s\n\n", modulePath, version)
	fmt.Fprintf(&text, "This directory is `%s` at `%s`, copied from the module cache by `%s` from cohere's root, "+
		"which wrote every file here, this one too. Edit the command, not the copy, and run it again after every "+
		"bump of the module in cohere's go.mod.\n\n", modulePath, version, command)
	fmt.Fprintf(&text, "Every import of `%s` in a copied Go file outside testdata names `%s` instead, cohere's "+
		"fork, since the fork's types are not the standard library's and every package that hands types to cohere "+
		"must check against the same ones. Nothing else in a Go file changed. Test files were left behind, and "+
		"testdata is copied whole, byte for byte, for the tests of cohere's that read it.\n\n", standardTypesPath,
		forkTypesPath)
	fmt.Fprintf(&text, "go.mod has one change, this appended:\n\n```\n%s```\n\n",
		strings.TrimPrefix(copied.goModAddition, "\n"))
	text.WriteString("The packages are those a Go file of cohere's imports, tests included, or a go.mod in go.work " +
		"names as a tool, and every package of the three copied modules theirs import in turn, on every platform. " +
		"A file built only under the tag `ignore`, a program run by hand, is copied with its package, but what it " +
		"imports is not followed.\n\n")
	for _, reader := range fixtureReaders {
		if reader.modulePath == modulePath {
			fmt.Fprintf(&text, "Beside those, every package whose testdata matches %s is copied, since `%s` reads "+
				"those fixtures without importing their packages.\n\n",
				"`"+strings.Join(reader.patterns, "`, `")+"`", reader.test)
		}
	}
	fmt.Fprintf(&text, "## Packages (%d)\n\n", len(copied.packages))
	for _, packagePath := range copied.packages {
		fmt.Fprintf(&text, "- `%s`\n", packagePath)
	}
	fmt.Fprintf(&text, "\n## Rewrites (%d)\n\n", len(copied.rewrites))
	if len(copied.rewrites) == 0 {
		text.WriteString("None: no copied file imports `" + standardTypesPath + "`.\n")
	}
	for _, rewrite := range copied.rewrites {
		fmt.Fprintf(&text, "- `%s`\n", rewrite)
	}
	return []byte(text.String())
}

// writeCopy makes copied's destination under root hold exactly its files: it writes each that differs, removes each
// it does not list, and then each directory left empty. It returns how many files it wrote and removed. It refuses a
// destination that is not one of the libraries' and any entry there that is neither a file nor a directory.
func writeCopy(root string, copied *copiedModule) (int, int, error) {
	if !slices.ContainsFunc(libraries, func(candidate library) bool {
		return candidate.destination == copied.source.library.destination
	}) {
		return 0, 0, fmt.Errorf("refusing to write %s, which is no library's destination",
			copied.source.library.destination)
	}
	destination := filepath.Join(root, filepath.FromSlash(copied.source.library.destination))
	removed := 0
	directories := []string{}
	walkError := filepath.WalkDir(destination, func(filePath string, entry fs.DirEntry, pathError error) error {
		if errors.Is(pathError, fs.ErrNotExist) && filePath == destination {
			return filepath.SkipAll
		}
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() {
			directories = append(directories, filePath)
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("refusing to touch %s, which is neither a file nor a directory", filePath)
		}
		relative, relativeError := filepath.Rel(destination, filePath)
		if relativeError != nil {
			return relativeError
		}
		if _, kept := copied.files[filepath.ToSlash(relative)]; kept {
			return nil
		}
		removed++
		return os.Remove(filePath)
	})
	if walkError != nil {
		return 0, 0, walkError
	}
	written := 0
	for _, name := range sortedKeys(copied.files) {
		if !filepath.IsLocal(filepath.FromSlash(name)) {
			return 0, 0, fmt.Errorf("refusing to write %s, which is outside %s", name, destination)
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		existing, readError := os.ReadFile(target)
		if readError == nil && bytes.Equal(existing, copied.files[name]) {
			continue
		}
		if mkdirError := os.MkdirAll(filepath.Dir(target), 0o755); mkdirError != nil {
			return 0, 0, mkdirError
		}
		if writeError := os.WriteFile(target, copied.files[name], 0o644); writeError != nil {
			return 0, 0, writeError
		}
		written++
	}
	// Deepest first, so a directory whose only children were directories just removed is empty in turn. A directory
	// that still holds a file is kept: os.Remove refuses it.
	slices.Reverse(directories)
	for _, directory := range directories {
		children, readError := os.ReadDir(directory)
		if readError != nil {
			return 0, 0, readError
		}
		if len(children) == 0 {
			if removeError := os.Remove(directory); removeError != nil {
				return 0, 0, removeError
			}
		}
	}
	return written, removed, nil
}

// sortedKeys is set's keys, sorted.
func sortedKeys[Value any](set map[string]Value) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
