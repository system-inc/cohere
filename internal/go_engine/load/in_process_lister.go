package load

import (
	"errors"
	"fmt"
	"go/build"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/mod/module"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/timing"
	"github.com/system-inc/cohere/internal/listed_directories"
)

// inProcessLister lists a workspace's packages the way `go list -e -deps -json` lists them, by reading what go list
// reads, in this process and in parallel: each package directory through go/build, go list's own reader of build
// constraints, file kinds and imports, and each `//go:embed` pattern's files. What only the go command can say it
// asks the go command, in three small commands run at once: the build list with each module's directory (`go list
// -m -json all`, which is minimal version selection, pruning, replacements and the workspace), the build context go
// list reads files under, and the go environment. Those take a few tens of milliseconds where go list's walk of every
// package took most of a second before any check could start (#tqcmpg8).
//
// It answers only what it can answer exactly. Anything go list would report as an error, a missing or ambiguous
// import, a cycle, an internal package used from outside, an embed pattern that matches nothing, a file that does
// not parse to its imports, and anything whose rules it does not carry, vendoring, a go.mod ignore directive, a
// `GODEBUG` setting, a flag in `GOFLAGS` that changes what is listed, a target whose commands link externally, another
// compiler or another toolchain, is a doubt, and a doubt is answered by go list itself, so a listing is never this
// lister's guess (see listInProcess). Every other target the toolchain builds for it answers, the host's and the
// extra builds' alike (#37vy8cm): go/build matches each file to the target as go list does, and what the linker adds
// to a command is the one other thing a target changes in the walk (linkerDependencies).
type inProcessLister struct {
	workspace   string
	environment []string
	context     build.Context

	// modules is the build list, the main modules first, as `go list -m -json all` gives it, and mainModules its
	// main modules.
	modules     []*packages.Module
	mainModules []*packages.Module

	// goModDirect are the modules a main module's go.mod requires without `// indirect`, which go list reports as
	// direct, and required is path@version of every requirement a main module's go.mod names, the only modules a
	// main module's package may import from as go list loads it (see checkDirectImports).
	goModDirect map[string]bool
	required    map[string]bool

	// inWorkspace is set when a go.work is in use, where go list also reports as direct a module a main module's
	// package imports from, which it does not for a module alone (see directModules).
	inWorkspace bool

	// slots bounds how many directories are read at once, and prefetchSlots how many of those the standard library's
	// prefetch may hold, so the reads a walk is waiting on are never queued behind it. pending counts the packages
	// still being read.
	slots         chan struct{}
	prefetchSlots chan struct{}
	pending       sync.WaitGroup

	// prefetchDone closes when the listing ends, after which the prefetch starts no read, and prefetching counts the
	// prefetch's walk and every read it started, which the listing waits for before it returns (see stopPrefetch).
	prefetchDone chan struct{}
	prefetching  sync.WaitGroup

	// visited is how far order's walk has got with each package, by import path.
	visited map[string]visitState

	mutex  sync.Mutex
	byPath map[string]*inProcessPackage
	reads  map[string]*directoryRead
	doubts []string

	// timing is the run's collector under `--timing`, nil otherwise. The lister's work is on many goroutines, and a
	// thread clock sees only its own thread, so each goroutine adds what it spent where it runs (measureListing).
	timing *timing.Collector
}

// inProcessPackage is one package as the lister read it: what go list would print of it, and the import paths go
// list walks from it, in its order, each import as resolved and then what cgo and the linker add.
type inProcessPackage struct {
	listed *listedPackage
	walked []string
}

// directoryRead is go/build's reading of one directory, read once however many patterns and imports reach it.
type directoryRead struct {
	once        sync.Once
	built       *build.Package
	importError error
}

// visitState is how far order's walk has got with a package: not reached, open while what it walks to is walked,
// and closed once it is in the order.
type visitState int

// The states of order's walk past not reached.
const visitOpen visitState = 1

const visitClosed visitState = 2

// The packages cgo excludes from the imports its translation adds, so they do not import themselves.
var (
	cgoExcluded        = map[string]bool{"runtime/cgo": true}
	cgoSyscallExcluded = map[string]bool{"runtime/cgo": true, "runtime/race": true, "runtime/msan": true,
		"runtime/asan": true}
)

// listInProcess is list without go list: the same listing, in the same order, with the same fields, read in this
// process (see inProcessLister), each file held holds read from its held bytes, and every other read through shared
// when it is set, adding its CPU to collector. It returns
// why it could not answer when it could not, and then the listing is nil and go list answers instead.
func listInProcess(
	workspace string,
	environment []string,
	patterns []string,
	held Held,
	shared *SharedReads,
	collector *timing.Collector,
) (*listing, string) {
	lister, doubt := newInProcessLister(workspace, environment, held, shared, collector)
	if doubt != "" {
		return nil, doubt
	}
	return lister.list(patterns)
}

// list is listInProcess's listing of patterns by a lister already built, the standard library prefetched while it
// lists and the prefetch ended before it returns.
func (lister *inProcessLister) list(patterns []string) (*listing, string) {
	lister.prefetchStandardLibrary()
	// The prefetch reads through held, whose bytes the caller writes again once the listing returns, so nothing it
	// started may outlive the listing, whatever the listing answers.
	defer lister.stopPrefetch()
	rootPaths, doubt := lister.match(patterns)
	if doubt != "" {
		return nil, doubt
	}
	// Every root was loaded as it was matched.
	lister.pending.Wait()
	if doubt = lister.firstDoubt(); doubt != "" {
		return nil, doubt
	}
	first, doubt := lister.order(rootPaths)
	if doubt != "" {
		return nil, doubt
	}

	// go list -deps follows no test's imports, so list does, as a second go list of what only the roots' tests import,
	// and this does the same.
	testImports := lister.testImports(rootPaths)
	for _, testImport := range testImports {
		lister.load(testImport)
	}
	lister.pending.Wait()
	if doubt = lister.firstDoubt(); doubt != "" {
		return nil, doubt
	}
	second, doubt := lister.order(testImports)
	if doubt != "" {
		return nil, doubt
	}

	result := &listing{byPath: map[string]*listedPackage{}, roots: map[string]bool{}, testOnly: map[string]bool{}}
	for passIndex, pass := range []struct {
		starts  []string
		ordered []*inProcessPackage
	}{{rootPaths, first}, {testImports, second}} {
		reached := lister.reached(pass.starts)
		if doubt = lister.checkImports(reached); doubt != "" {
			return nil, doubt
		}
		direct := lister.directModules(reached)
		var group sync.WaitGroup
		for _, current := range pass.ordered {
			group.Go(func() {
				measureListing(lister.timing, func() { current.listed.Deps = lister.deps(current) })
			})
		}
		group.Wait()
		for _, current := range pass.ordered {
			current.listed.DepOnly = !slices.Contains(pass.starts, current.listed.ImportPath)
			if module := current.listed.Module; module != nil {
				reported := *module
				reported.Indirect = !module.Main && !direct[module.Path]
				current.listed.Module = &reported
			}
			result.packages = append(result.packages, current.listed)
			result.byPath[current.listed.ImportPath] = current.listed
			if passIndex == 0 && !current.listed.DepOnly {
				result.roots[current.listed.ImportPath] = true
			}
			if passIndex == 1 {
				result.testOnly[current.listed.ImportPath] = true
			}
		}
	}
	if doubt = foldCollision(result.packages); doubt != "" {
		return nil, doubt
	}
	return result, ""
}

// newInProcessLister reads the build list, the build context and the go environment, and says why it cannot list
// at all when something in them is beyond what it carries. Its build context opens a file held holds from its held
// bytes, and any other on disk, so go/build reads no file the run already holds again, or from shared, which every
// listing of the run reads its directories and files through when it is set (SharedReads). What it and its listing
// spend is added to collector.
func newInProcessLister(
	workspace string,
	environment []string,
	held Held,
	shared *SharedReads,
	collector *timing.Collector,
) (*inProcessLister, string) {
	read, doubt := readGoState(workspace, environment, collector)
	if doubt != "" {
		return nil, doubt
	}
	read.context.OpenFile = held.openFile()
	if shared != nil {
		read.context.ReadDir = shared.readDirectory
		read.context.OpenFile = shared.openFile(held)
	}
	lister := &inProcessLister{
		workspace:     workspace,
		environment:   environment,
		context:       read.context,
		modules:       read.modules,
		inWorkspace:   read.workspaceFile != "",
		goModDirect:   map[string]bool{},
		required:      map[string]bool{},
		slots:         make(chan struct{}, 2*runtime.GOMAXPROCS(0)),
		prefetchSlots: make(chan struct{}, max(1, runtime.GOMAXPROCS(0)/2)),
		prefetchDone:  make(chan struct{}),
		byPath:        map[string]*inProcessPackage{},
		visited:       map[string]visitState{},
		reads:         map[string]*directoryRead{},
		timing:        collector,
	}
	for _, each := range read.modules {
		if each.Error != nil {
			return nil, fmt.Sprintf("module %s has an error: %s", each.Path, each.Error.Err)
		}
		if each.Main {
			lister.mainModules = append(lister.mainModules, each)
		} else if !each.Indirect {
			lister.goModDirect[each.Path] = true
		}
	}
	if len(lister.mainModules) == 0 {
		return nil, "there is no main module"
	}
	if doubt = lister.readMainModules(read.workspaceFile); doubt != "" {
		return nil, doubt
	}
	return lister, ""
}

// match is the import paths of the packages the patterns name, as go list orders them: each local pattern's in the
// order its walk meets their directories, each module pattern's sorted, and a package that two patterns name where the
// first names it.
func (lister *inProcessLister) match(patterns []string) ([]string, string) {
	// Each pattern is walked at once, and what they name is put in the patterns' order after.
	matches := make([][]string, len(patterns))
	doubts := make([]string, len(patterns))
	var group sync.WaitGroup
	for index, pattern := range patterns {
		group.Go(func() {
			if pattern == "./..." {
				measureListing(lister.timing, func() { matches[index], doubts[index] = lister.matchLocal() })
			} else if pattern == "." || strings.HasPrefix(pattern, "./") && !strings.Contains(pattern, "...") {
				measureListing(lister.timing, func() { matches[index], doubts[index] = lister.matchDirectory(pattern) })
			} else if strings.HasPrefix(pattern, ".") || filepath.IsAbs(pattern) {
				// A directory pattern below or outside the workspace, `./sub/...` or an absolute path, is matched by
				// directory, which matchLocal does only for the workspace itself and matchDirectory only for one
				// directory below it. Read as a module import path it would name no module, match nothing and answer
				// an empty listing, which go list never does.
				doubts[index] = fmt.Sprintf("the pattern %s names a directory, which the lister matches only as ./...",
					pattern)
			} else if prefix, wildcard := strings.CutSuffix(
				pattern,
				"/...",
			); wildcard && !strings.Contains(prefix, "...") {
				measureListing(lister.timing, func() { matches[index], doubts[index] = lister.matchModules(prefix) })
			} else {
				doubts[index] = fmt.Sprintf("the pattern %s is not one the lister matches", pattern)
			}
		})
	}
	group.Wait()
	var matched []string
	seen := map[string]bool{}
	for index := range patterns {
		if doubts[index] != "" {
			return nil, doubts[index]
		}
		if len(matches[index]) == 0 {
			// go list warns that such a pattern matched no packages and lists the others, so an empty match is
			// go list's to answer, never an empty listing that would check nothing and say nothing.
			return nil, fmt.Sprintf("the pattern %s matched no packages", patterns[index])
		}
		for _, each := range matches[index] {
			if !seen[each] {
				seen[each] = true
				matched = append(matched, each)
			}
		}
	}
	return matched, ""
}

// matchLocal is what `./...` names from the workspace: every directory below it, as go's walk of a local pattern
// goes, that holds a package, in the module the workspace is in.
func (lister *inProcessLister) matchLocal() ([]string, string) {
	owner := lister.moduleRootOf(lister.workspace)
	if owner == nil {
		return nil, fmt.Sprintf("%s is in no main module", lister.workspace)
	}
	directories, doubt := listed_directories.PackageDirectories(lister.workspace, func(string) bool { return true })
	if doubt != "" {
		return nil, doubt
	}
	return lister.packagesIn(directories, owner.Dir, owner.Path)
}

// matchDirectory is what a local pattern naming one directory, `.` or `./sub`, names: the package in that directory,
// by its import path in the main module the directory is in, as go list names it. A directory in no main module is a
// doubt, and one holding no package matches nothing, which match leaves to go list. The editor's save lists its
// file's package so (see the engine's stdinPackage).
func (lister *inProcessLister) matchDirectory(pattern string) ([]string, string) {
	directory := filepath.Join(lister.workspace, filepath.FromSlash(pattern))
	owner := lister.moduleRootOf(directory)
	if owner == nil {
		return nil, fmt.Sprintf("%s is in no main module", directory)
	}
	return lister.packagesIn([]string{directory}, owner.Dir, owner.Path)
}

// matchModules is what `prefix/...` names: every package of a main module whose import path is prefix or below it,
// sorted. A module outside the workspace whose packages the pattern could name is a doubt, since go list would fetch
// and walk it.
func (lister *inProcessLister) matchModules(prefix string) ([]string, string) {
	underPattern := func(name string) bool { return name == prefix || strings.HasPrefix(name, prefix+"/") }
	canMatch := func(name string) bool { return underPattern(name) || strings.HasPrefix(prefix, name+"/") }
	var matched []string
	for _, each := range lister.modules {
		if !canMatch(each.Path) {
			continue
		}
		if !each.Main {
			return nil, fmt.Sprintf("the pattern %s/... could name packages of %s, which is not a main module",
				prefix, each.Path)
		}
		directories, doubt := listed_directories.PackageDirectories(each.Dir, func(directory string) bool {
			return canMatch(importPathIn(each.Dir, each.Path, directory))
		})
		if doubt != "" {
			return nil, doubt
		}
		var inModule []string
		for _, directory := range directories {
			if underPattern(importPathIn(each.Dir, each.Path, directory)) {
				inModule = append(inModule, directory)
			}
		}
		paths, doubt := lister.packagesIn(inModule, each.Dir, each.Path)
		if doubt != "" {
			return nil, doubt
		}
		matched = append(matched, paths...)
	}
	slices.Sort(matched)
	return slices.Compact(matched), ""
}

// packagesIn is the import paths of the directories that hold a package, in moduleDirectory's module: a directory
// go/build finds no Go file to build in holds none, as go's walk skips it, and one it cannot read cleanly is a doubt,
// since go list would list it with an error.
func (lister *inProcessLister) packagesIn(
	directories []string,
	moduleDirectory string,
	modulePath string,
) ([]string, string) {
	reads := make([]*directoryRead, len(directories))
	var group sync.WaitGroup
	for index, directory := range directories {
		group.Go(func() { reads[index] = lister.readDirectory(directory) })
	}
	group.Wait()
	var paths []string
	for index, directory := range directories {
		importPath := importPathIn(moduleDirectory, modulePath, directory)
		if noGo, isNoGo := errors.AsType[*build.NoGoError](reads[index].importError); isNoGo {
			if reads[index].built != nil && len(reads[index].built.InvalidGoFiles) > 0 {
				return nil, fmt.Sprintf("%s has Go files that do not parse: %v", importPath, noGo)
			}
			continue
		}
		if reads[index].importError != nil {
			return nil, fmt.Sprintf("%s: %v", importPath, reads[index].importError)
		}
		if filepath.Base(directory) == "vendor" {
			return nil, fmt.Sprintf("%s is a package in a directory named vendor", importPath)
		}
		// Its imports start now, while the other patterns are still walked; the order is set after, from the roots.
		lister.load(importPath)
		paths = append(paths, importPath)
	}
	return paths, ""
}

// prefetchStandardLibrary starts reading every directory of the standard library at once, but the commands' and
// testdata, without waiting. A package's imports are known only once it is read, so the standard library's long chain
// of packages, each importing the next down to runtime, would otherwise be read one link at a time; read all at
// once, every link is ready when the walk reaches it. What no package imports costs CPU and no time, since the
// prefetch holds at most half the read slots. It runs until the listing ends, when stopPrefetch stops it.
func (lister *inProcessLister) prefetchStandardLibrary() {
	source := filepath.Join(lister.context.GOROOT, "src")
	lister.prefetching.Go(func() {
		// The walk waits for nothing, as each read it starts is on a goroutine of its own, so it is measured whole.
		measureListing(lister.timing, func() {
			// Ignored: a directory the walk cannot read is read again, its error reported, once an import reaches it.
			_ = filepath.WalkDir(source, func(directory string, entry fs.DirEntry, pathError error) error {
				if lister.prefetchStopped() {
					return filepath.SkipAll
				}
				if pathError != nil || !entry.IsDir() {
					return nil
				}
				name := entry.Name()
				if directory != source && (name == "testdata" || name == "cmd" && filepath.Dir(directory) == source ||
					strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
					return filepath.SkipDir
				}
				// The walk is counted in prefetching until it returns, so this adds to a count above zero, which may
				// happen while stopPrefetch waits.
				lister.prefetching.Go(func() {
					select {
					case lister.prefetchSlots <- struct{}{}:
					case <-lister.prefetchDone:
						return
					}
					defer func() { <-lister.prefetchSlots }()
					// A slot and the end can be ready together, and select takes either, so the end is asked again.
					if lister.prefetchStopped() {
						return
					}
					lister.readDirectory(directory)
				})
				return nil
			})
		})
	})
}

// stopPrefetch ends the prefetch: no read starts after it, and it returns once every read already started has ended,
// so none reads through held after the listing returns. What it waits for is at most one read per prefetch slot.
func (lister *inProcessLister) stopPrefetch() {
	close(lister.prefetchDone)
	lister.prefetching.Wait()
}

// prefetchStopped reports whether the listing has ended, after which the prefetch starts nothing.
func (lister *inProcessLister) prefetchStopped() bool {
	select {
	case <-lister.prefetchDone:
		return true
	default:
		return false
	}
}

// load reads importPath and, in parallel, everything it imports, each package once.
func (lister *inProcessLister) load(importPath string) {
	lister.mutex.Lock()
	if lister.byPath[importPath] != nil {
		lister.mutex.Unlock()
		return
	}
	current := &inProcessPackage{listed: &listedPackage{ImportPath: importPath}}
	lister.byPath[importPath] = current
	lister.mutex.Unlock()
	lister.pending.Go(func() {
		listed, walked, doubt := lister.read(importPath)
		if doubt != "" {
			lister.doubt(doubt)
			return
		}
		// Nothing reads a package's fields until every read has ended (see listInProcess).
		current.listed, current.walked = listed, walked
		for _, each := range walked {
			lister.load(each)
		}
	})
}

// read is one package as go list's load of it reads it from its directory, its files and its imports resolved, and
// the import paths go list walks from it: each import, then those cgo and the linker add, which it does not print
// among Imports.
func (lister *inProcessLister) read(importPath string) (*listedPackage, []string, string) {
	var directory string
	var owner *packages.Module
	var doubt string
	measureListing(lister.timing, func() { directory, owner, doubt = lister.locate(importPath) })
	if doubt != "" {
		return nil, nil, doubt
	}
	// The directory's read is measured where it runs (readDirectory) and never around this wait for it, which would
	// hold a thread for each package waiting at once.
	read := lister.readDirectory(directory)
	var listed *listedPackage
	var walked []string
	measureListing(lister.timing, func() {
		listed, walked, doubt = lister.readFrom(importPath, directory, owner, read)
	})
	return listed, walked, doubt
}

// readFrom is read once the package is located in directory, in owner's module, and the directory is read.
func (lister *inProcessLister) readFrom(
	importPath string,
	directory string,
	owner *packages.Module,
	read *directoryRead,
) (*listedPackage, []string, string) {
	listed := &listedPackage{ImportPath: importPath}
	var walked []string
	if read.importError != nil {
		return nil, nil, fmt.Sprintf("%s: %v", listed.ImportPath, read.importError)
	}
	built := read.built
	if built.Name == "" || len(built.InvalidGoFiles) > 0 {
		return nil, nil, fmt.Sprintf("%s has Go files that do not parse", listed.ImportPath)
	}
	if len(built.SwigFiles)+len(built.SwigCXXFiles) > 0 {
		return nil, nil, fmt.Sprintf("%s has files for the swig tool", listed.ImportPath)
	}
	listed.Name = built.Name
	listed.Directory = directory
	listed.Module = owner
	// Every list is nil when empty, as go list omits an empty one and decoding its answer leaves it nil.
	listed.GoFiles = nonEmpty(built.GoFiles)
	listed.CgoFiles = nonEmpty(built.CgoFiles)
	listed.IgnoredGoFiles = nonEmpty(built.IgnoredGoFiles)
	listed.IgnoredOtherFiles = nonEmpty(built.IgnoredOtherFiles)
	listed.HFiles = nonEmpty(built.HFiles)
	listed.SFiles = nonEmpty(built.SFiles)
	listed.SysoFiles = nonEmpty(built.SysoFiles)
	if lister.context.CgoEnabled {
		// go list drops the C, C++ and Objective-C files of every package when cgo is off, and the swig tool's too.
		listed.CFiles = nonEmpty(built.CFiles)
		listed.CxxFiles = nonEmpty(built.CXXFiles)
		listed.MFiles = nonEmpty(built.MFiles)
	}
	listed.FFiles = nonEmpty(built.FFiles)
	usesCgo := len(built.CgoFiles) > 0
	if !usesCgo && len(listed.CFiles)+len(listed.CxxFiles)+len(listed.MFiles)+len(listed.FFiles) > 0 {
		return nil, nil, fmt.Sprintf("%s has C, C++, Objective-C or Fortran files and no cgo", listed.ImportPath)
	}
	listed.TestGoFiles = nonEmpty(built.TestGoFiles)
	listed.XTestGoFiles = nonEmpty(built.XTestGoFiles)
	standard := owner == nil
	// go list prints test imports resolved, a standard package's vendored, deduplicated and sorted by what they resolve
	// to, though it follows none of them.
	listed.TestImports = lister.resolvedTestImports(standard, built.TestImports)
	listed.XTestImports = lister.resolvedTestImports(standard, built.XTestImports)
	if len(built.EmbedPatterns) > 0 {
		files, embedDoubt := resolveEmbedPatterns(directory, built.EmbedPatterns)
		if embedDoubt != "" {
			return nil, nil, fmt.Sprintf("%s: %s", listed.ImportPath, embedDoubt)
		}
		listed.EmbedFiles = files
	}

	written := slices.Clone(built.Imports)
	for _, each := range written {
		if each == "simd" {
			return nil, nil, fmt.Sprintf("%s imports simd, whose bridge go list adds", listed.ImportPath)
		}
	}
	added := written
	addImport := func(each string) {
		if !slices.Contains(added, each) {
			added = append(added, each)
		}
	}
	if usesCgo {
		addImport("unsafe")
		if !standard || !cgoExcluded[listed.ImportPath] {
			addImport("runtime/cgo")
		}
		if !standard || !cgoSyscallExcluded[listed.ImportPath] {
			addImport("syscall")
		}
	}
	if built.Name == "main" {
		// The linker's implicit dependencies on the target, with no flag that adds more.
		for _, each := range linkerDependencies(lister.context.GOARCH) {
			addImport(each)
		}
	}
	if len(written) > 0 {
		listed.Imports = make([]string, len(written))
	}
	for index, each := range added {
		if each == "C" {
			if index < len(written) {
				listed.Imports[index] = each
			}
			continue
		}
		resolved, resolveDoubt := lister.resolve(listed, standard, each)
		if resolveDoubt != "" {
			return nil, nil, resolveDoubt
		}
		if index < len(written) {
			listed.Imports[index] = resolved
			if resolved != each {
				if listed.ImportMap == nil {
					listed.ImportMap = map[string]string{}
				}
				listed.ImportMap[each] = resolved
			}
		}
		walked = append(walked, resolved)
	}
	return listed, walked, ""
}

// resolve is the import path an import written in listed resolves to: a standard package's import of a package
// outside the standard library is of the toolchain's vendored copy, and every other import is of itself. An import
// that is not a valid path, that names a vendor or command directory, or that reaches an internal package from
// outside its tree is a doubt.
func (lister *inProcessLister) resolve(listed *listedPackage, standard bool, written string) (string, string) {
	if checkError := module.CheckImportPath(written); checkError != nil {
		return "", fmt.Sprintf("%s imports %q: %v", listed.ImportPath, written, checkError)
	}
	if written == "vendor" || strings.HasPrefix(written, "vendor/") || written == "cmd" ||
		strings.HasPrefix(written, "cmd/") {
		return "", fmt.Sprintf("%s imports %s", listed.ImportPath, written)
	}
	resolved := lister.vendored(standard, written)
	if !internalAllowed(listed.ImportPath, standard, resolved) {
		return "", fmt.Sprintf("%s imports %s, an internal package of another tree", listed.ImportPath, resolved)
	}
	return resolved, ""
}

// vendored is what an import resolves to from a standard package or another: a standard package's import of one
// outside the standard library is of the toolchain's vendored copy when the toolchain has a directory for it, as go's
// stdVendor decides by the directory alone, and every other import is of itself.
func (lister *inProcessLister) vendored(standard bool, written string) string {
	if !standard || standardImportPath(written) {
		return written
	}
	vendored := "vendor/" + written
	if exists(filepath.Join(lister.context.GOROOT, "src", filepath.FromSlash(vendored))) {
		return vendored
	}
	return written
}

// resolvedTestImports is a package's test imports as go list prints them: each resolved as vendored resolves it,
// deduplicated, sorted, and nil when there are none.
func (lister *inProcessLister) resolvedTestImports(standard bool, imported []string) []string {
	var resolved []string
	for _, each := range imported {
		resolved = append(resolved, lister.vendored(standard, each))
	}
	slices.Sort(resolved)
	return slices.Compact(resolved)
}

// locate is the directory of the package at importPath and the module it is in, nil for the standard library, as go
// list finds it: a standard import path in the toolchain's root, and any other in the one module of the build list
// whose directory holds Go files at that path below it. None, or more than one, is a doubt, which go list reports as a
// missing or an ambiguous import. A directory that is the only place the package could be is not checked for Go files
// here: reading it says so, and a directory without them is a doubt there.
func (lister *inProcessLister) locate(importPath string) (string, *packages.Module, string) {
	if standardImportPath(importPath) {
		return filepath.Join(lister.context.GOROOT, "src", filepath.FromSlash(importPath)), nil, ""
	}
	var candidates []string
	var owners []*packages.Module
	for _, each := range lister.modules {
		if importPath != each.Path && !strings.HasPrefix(importPath, each.Path+"/") {
			continue
		}
		if each.Dir == "" {
			return "", nil, fmt.Sprintf("%s could be in %s, which has no directory", importPath, each.Path)
		}
		directory := filepath.Join(each.Dir, filepath.FromSlash(strings.TrimPrefix(importPath[len(each.Path):], "/")))
		local := each.Version == "" || each.Replace != nil && each.Replace.Version == ""
		if local && nestedModule(each.Dir, directory) {
			continue
		}
		candidates = append(candidates, directory)
		owners = append(owners, each)
	}
	if len(candidates) == 1 {
		return candidates[0], owners[0], ""
	}
	var found []string
	var owner *packages.Module
	for index, directory := range candidates {
		if hasGoFiles(directory) {
			found = append(found, directory)
			owner = owners[index]
		}
	}
	if len(found) != 1 {
		return "", nil, fmt.Sprintf("%s is in %d modules", importPath, len(found))
	}
	return found[0], owner, ""
}

// readDirectory is go/build's reading of directory under the lister's build context, read once.
func (lister *inProcessLister) readDirectory(directory string) *directoryRead {
	lister.mutex.Lock()
	read := lister.reads[directory]
	if read == nil {
		read = &directoryRead{}
		lister.reads[directory] = read
	}
	lister.mutex.Unlock()
	read.once.Do(func() {
		lister.slots <- struct{}{}
		defer func() { <-lister.slots }()
		// Measured once its slot is held, so no measurement holds a thread while it waits for one.
		measureListing(lister.timing, func() { read.built, read.importError = lister.context.ImportDir(directory, 0) })
	})
	return read
}

// moduleRootOf is the main module directory is in, by the nearest go.mod at or above it, or nil when that go.mod is
// no main module's.
func (lister *inProcessLister) moduleRootOf(directory string) *packages.Module {
	for each := directory; ; each = filepath.Dir(each) {
		if listed_directories.GoModAt(each) {
			for _, main := range lister.mainModules {
				if main.Dir == each {
					return main
				}
			}
			return nil
		}
		if filepath.Dir(each) == each {
			return nil
		}
	}
}

// testImports is what the roots' tests import that nothing listed yet is, sorted, as list names it to go list.
func (lister *inProcessLister) testImports(rootPaths []string) []string {
	var imported []string
	for _, rootPath := range rootPaths {
		root := lister.byPath[rootPath].listed
		for _, each := range slices.Concat(root.TestImports, root.XTestImports) {
			each = canonical(root, each)
			if each != "C" && lister.byPath[each] == nil && !slices.Contains(imported, each) {
				imported = append(imported, each)
			}
		}
	}
	slices.Sort(imported)
	return imported
}

// order is the packages reached from starts that no earlier order reached, in go list's order: depth first from each
// start in turn, each package after everything it walks to. A package reached again while its own walk is still open
// is a cycle, which go list reports, so it is a doubt. What an order visited stays visited, as the second listing's
// packages are the ones the first did not list.
func (lister *inProcessLister) order(starts []string) ([]*inProcessPackage, string) {
	var ordered []*inProcessPackage
	var visit func(importPath string) string
	visit = func(importPath string) string {
		switch lister.visited[importPath] {
		case visitOpen:
			return fmt.Sprintf("%s is in an import cycle", importPath)
		case visitClosed:
			return ""
		}
		lister.visited[importPath] = visitOpen
		current := lister.byPath[importPath]
		for _, walked := range current.walked {
			if doubt := visit(walked); doubt != "" {
				return doubt
			}
		}
		lister.visited[importPath] = visitClosed
		ordered = append(ordered, current)
		return ""
	}
	for _, start := range starts {
		if doubt := visit(start); doubt != "" {
			return nil, doubt
		}
	}
	return ordered, ""
}

// reached is every package reached from starts, starts among them, whether listed before or not.
func (lister *inProcessLister) reached(starts []string) []*inProcessPackage {
	seen := map[string]bool{}
	var reached []*inProcessPackage
	queue := slices.Clone(starts)
	for len(queue) > 0 {
		importPath := queue[0]
		queue = queue[1:]
		if seen[importPath] {
			continue
		}
		seen[importPath] = true
		current := lister.byPath[importPath]
		reached = append(reached, current)
		queue = append(queue, current.walked...)
	}
	return reached
}

// checkImports is a doubt when a main module's package imports from a module outside the workspace that no main
// module's go.mod requires at the version selected, which go list reports as a missing requirement, or in a
// workspace as an import from an implicit dependency.
func (lister *inProcessLister) checkImports(reached []*inProcessPackage) string {
	for _, current := range reached {
		if current.listed.Module == nil || !current.listed.Module.Main {
			continue
		}
		for _, walked := range current.walked {
			dependency := lister.byPath[walked].listed.Module
			if dependency == nil || dependency.Main {
				continue
			}
			if !lister.required[dependency.Path+"@"+dependency.Version] {
				return fmt.Sprintf("%s imports %s from %s@%s, which no main module requires at that version",
					current.listed.ImportPath, walked, dependency.Path, dependency.Version)
			}
		}
	}
	return ""
}

// directModules are the modules go list reports as direct for the packages a listing reached: those a main module's
// go.mod requires without `// indirect`, and in a workspace those a main module's package imports from too. go
// computes the second for a module alone as well, but under -mod=readonly reports its module information from the
// go.mod as written, so there `// indirect` stands.
func (lister *inProcessLister) directModules(reached []*inProcessPackage) map[string]bool {
	direct := maps.Clone(lister.goModDirect)
	if !lister.inWorkspace {
		return direct
	}
	for _, current := range reached {
		if current.listed.Module == nil || !current.listed.Module.Main {
			continue
		}
		for _, walked := range current.walked {
			if dependency := lister.byPath[walked].listed.Module; dependency != nil && !dependency.Main {
				direct[dependency.Path] = true
			}
		}
	}
	return direct
}

// deps is every package current reaches, by import path, sorted, as go list's Deps.
func (lister *inProcessLister) deps(current *inProcessPackage) []string {
	seen := map[string]bool{}
	queue := slices.Clone(current.walked)
	for len(queue) > 0 {
		importPath := queue[0]
		queue = queue[1:]
		if seen[importPath] {
			continue
		}
		seen[importPath] = true
		queue = append(queue, lister.byPath[importPath].walked...)
	}
	if len(seen) == 0 {
		return nil
	}
	return sortedKeys(seen)
}

// doubt records why the listing cannot be answered here.
func (lister *inProcessLister) doubt(reason string) {
	lister.mutex.Lock()
	defer lister.mutex.Unlock()
	lister.doubts = append(lister.doubts, reason)
}

// firstDoubt is the least of the recorded doubts, so the reason a run gives does not depend on which read ended
// first, or empty when there is none.
func (lister *inProcessLister) firstDoubt() string {
	lister.mutex.Lock()
	defer lister.mutex.Unlock()
	if len(lister.doubts) == 0 {
		return ""
	}
	return slices.Min(lister.doubts)
}

// importPathIn is the import path of directory in the module at moduleDirectory with path modulePath.
func importPathIn(moduleDirectory string, modulePath string, directory string) string {
	if directory == moduleDirectory {
		return modulePath
	}
	return path.Join(
		modulePath,
		filepath.ToSlash(strings.TrimPrefix(directory, moduleDirectory+string(filepath.Separator))),
	)
}

// standardImportPath reports whether importPath is one go looks for in the standard library: its first element has
// no dot.
func standardImportPath(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

// internalAllowed reports whether importer may import imported, as go list judges an internal package by import path:
// a package below an "internal" element may be imported only from the tree its parent roots, and a top-level internal
// package only from the standard library.
func internalAllowed(importer string, importerStandard bool, imported string) bool {
	var index int
	switch {
	case strings.HasSuffix(imported, "/internal"):
		index = len(imported) - len("internal")
	case strings.Contains(imported, "/internal/"):
		index = strings.LastIndex(imported, "/internal/") + 1
	case imported == "internal" || strings.HasPrefix(imported, "internal/"):
		index = 0
	default:
		return true
	}
	parent := strings.TrimSuffix(imported[:index], "/")
	if parent == "" {
		return importerStandard
	}
	return importer == parent || strings.HasPrefix(importer, parent+"/")
}

// nestedModule reports whether a go.mod between moduleDirectory, not counted, and directory, counted, starts another
// module, so directory is not moduleDirectory's.
func nestedModule(moduleDirectory string, directory string) bool {
	for each := directory; len(each) > len(moduleDirectory); each = filepath.Dir(each) {
		if listed_directories.GoModAt(each) {
			return true
		}
	}
	return false
}

// hasGoFiles reports whether directory holds a .go file, whatever its build constraints, a symbolic link to a
// regular file counted as one, which is what go asks of a directory to count it as where a package is.
func hasGoFiles(directory string) bool {
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if entry.Type().IsRegular() {
			return true
		}
		if info, statError := os.Stat(
			filepath.Join(directory, entry.Name()),
		); statError == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// nonEmpty is list, or nil when it holds nothing.
func nonEmpty(list []string) []string {
	if len(list) == 0 {
		return nil
	}
	return list
}

// foldCollision is a doubt when two listed import paths differ only in case, which go list reports. Every import path
// the lister accepts is ASCII, so folding is lowering.
func foldCollision(listed []*listedPackage) string {
	folded := map[string]string{}
	for _, each := range listed {
		key := strings.ToLower(each.ImportPath)
		if earlier, found := folded[key]; found && earlier != each.ImportPath {
			return fmt.Sprintf("%s and %s differ only in case", earlier, each.ImportPath)
		}
		folded[key] = each.ImportPath
	}
	return ""
}
