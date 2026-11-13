// Package build is go/build's reading of a package directory, copied from the toolchain so the builds of one run
// read each Go file's header once (#tb538na). go/build reads a file's header, its package clause, imports, build
// constraints, cgo preamble and `//go:embed` lines, and parses it in readGoInfo, in every build that lists the file,
// though the header and its parse depend on the file alone; only which files a build selects depends on the build.
// Of an extra build's listing at cohere's root that parse was 1.0 to 2.4 CPU-s a run under `--no-cache`. go/build has
// no hook for it, so the copied files beside this one carry the part of Context.Import after the package is found,
// and the files that declare no copy are ours: the Context the copy hangs off, the run's table of reads (Headers), and
// ImportDirectory, which finds the package through go/build itself and reads its files through the copy.
//
// Each build still matches every file by its own context, by name (goodOSArchFile), by build constraint
// (shouldBuild), cgo on or off, and assembles its own Package; only readGoInfo's result is shared.
package build

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/token"
	"maps"
	"slices"
	"strings"
)

// CopiedToolchain is the Go release whose go/build the copied files are, the version each copy's header names.
const CopiedToolchain = "go1.27.0"

// sharedReleases are the Go releases whose go/build/build.go, go/build/read.go and internal/syslist/syslist.go are
// CopiedToolchain's byte for byte, so a binary any of them links reads a directory through the copy as through its own
// go/build: the copy calls nothing else that is go/build's own, and what it calls of go/parser, go/scanner, go/ast and
// the rest is the linked release's either way. go1.27.1 to go1.27.3 were held to go1.27.0's sources by their sha256
// (#tb538na), and the release workflow builds with the newest go1.27 (setup-go reads go.mod's `go 1.27`). A binary
// another release links reads through go/build itself (Sharing). A test holds the toolchain's sources to the copy's
// and its release to this list, so a release that moves the sources fails it, and one that leaves them is added.
var sharedReleases = map[string]bool{"go1.27.0": true, "go1.27.1": true, "go1.27.2": true, "go1.27.3": true}

// Context is a go/build build context with the run's shared reads of Go files' headers. Embedding go/build's keeps
// every field the copied code reads, `ctxt.GOOS` and `ctxt.OpenFile` among them, spelled as upstream spells it.
type Context struct {
	build.Context

	// held is the bytes the run holds for a file, as the listing's build context opens it from, and headers the run's
	// reads of each Go file's header.
	held    func(path string) ([]byte, bool)
	headers *Headers
}

// The types and modes of go/build the copied code names, which are go/build's own, so the Package the copy assembles
// is the one go/build's ImportDir returns and its errors are go/build's types.
type (
	Package              = build.Package
	Directive            = build.Directive
	ImportMode           = build.ImportMode
	NoGoError            = build.NoGoError
	MultiplePackageError = build.MultiplePackageError
)

// The modes the copied part of Import reads.
const (
	FindOnly      = build.FindOnly
	AllowBinary   = build.AllowBinary
	ImportComment = build.ImportComment
)

// errorCgoFile is how the copied Import says a file it matched imports "C": go/build's saveCgo reads the file's #cgo
// lines against the build, which the copy does not carry, so ImportDirectory reads that directory through go/build.
var errorCgoFile = errors.New(`a file imports "C"`)

// Sharing is whether a binary linked by the Go release linked, as runtime.Version gives it, may read through the
// copy, and when it may not, why, in words a record can carry: only a release whose go/build is the copy's
// (sharedReleases) reads a header as the copy does. A version's experiment suffix, ` X:` and what follows, changes
// nothing go/build reads.
func Sharing(linked string) (bool, string) {
	release, _, _ := strings.Cut(linked, " ")
	if sharedReleases[release] {
		return true, ""
	}
	return false, fmt.Sprintf("this binary links go/build of %s, and the copy that reads each Go file's header once "+
		"a run is of %s's, which only %s ship, so every build's listing parsed every header itself", linked,
		CopiedToolchain, strings.Join(slices.Sorted(maps.Keys(sharedReleases)), ", "))
}

// ImportDirectory is go/build's context.ImportDir(directory, 0), each Go file's header read through headers, which
// every listing of the run shares, or from held when the run holds the file. go/build finds the package itself,
// through ImportDir with FindOnly, which stops before reading a file: everything up to there, which a local import's
// directory decides, is upstream's own. A directory with a file that imports "C" is read by go/build's ImportDir
// whole (errorCgoFile).
func ImportDirectory(
	context build.Context,
	directory string,
	held func(path string) ([]byte, bool),
	headers *Headers,
) (*build.Package, error) {
	found, findError := context.ImportDir(directory, build.FindOnly)
	if findError != nil {
		return found, findError
	}
	copied := &Context{Context: context, held: held, headers: headers}
	built, importError := copied.importFiles(found, 0)
	if errors.Is(importError, errorCgoFile) {
		return context.ImportDir(directory, 0)
	}
	return built, importError
}

// readGoInfoOnce is what readGoInfo reads of the file at name into fileSet, read once for the run, with the error
// opening the file and the error reading it apart, as matchFile returns them apart, and each import's position
// resolved (resolved). The first build to finish reading the file keeps of the read what Import reads (compacted),
// and every build after takes that. A build that finds the file being read by another reads it itself rather than
// wait, into a file set of its own, and takes whichever read finished first: the two read the same bytes, the held
// bytes being the ones the shared read was made for (Headers.header), so either answers alike. Without headers it
// opens and reads the file into fileSet, as upstream's matchFile does.
//
// The receiver is named ctxt, as every method of Context in the copied files names it.
func (ctxt *Context) readGoInfoOnce(name string, fileSet *token.FileSet) (fileInfo, error, error) {
	if ctxt.headers == nil {
		read, openError, readError := ctxt.openAndReadGoInfo(name, fileSet)
		return resolved(read), openError, readError
	}
	var source []byte
	held := false
	if ctxt.held != nil {
		source, held = ctxt.held(name)
	}
	shared := ctxt.headers.header(name, held, source)
	if finished := shared.read.Load(); finished != nil {
		return finished.info, finished.openError, finished.readError
	}
	if ctxt.headers.reading != nil {
		ctxt.headers.reading(name)
	}
	// A file set of the read's own, which no other build reads a position through: every position Import reads is
	// resolved before the read is shared.
	read, openError, readError := ctxt.openAndReadGoInfo(name, token.NewFileSet())
	ctxt.headers.parsed.Add(1)
	ours := &sharedRead{info: compacted(resolved(read)), openError: openError, readError: readError}
	if shared.read.CompareAndSwap(nil, ours) {
		return ours.info, ours.openError, ours.readError
	}
	// Another build finished first. Every field the read set is one a build reads and never writes, the slices
	// included: Import only appends their elements to its own Package's lists.
	kept := shared.read.Load()
	return kept.info, kept.openError, kept.readError
}

// openAndReadGoInfo opens the file at name and reads it as readGoInfo reads it, into fileSet, then closes it, as
// upstream's matchFile does, with the open's error and the read's apart.
func (ctxt *Context) openAndReadGoInfo(name string, fileSet *token.FileSet) (fileInfo, error, error) {
	read := fileInfo{name: name, fset: fileSet}
	reader, openError := ctxt.openFile(name)
	if openError != nil {
		return read, openError, nil
	}
	readError := readGoInfo(reader, &read)
	// Ignored: the file was only read, and upstream's matchFile ignores it too.
	_ = reader.Close()
	return read, nil, readError
}

// compacted is read as the copied Import reads it, and no more, so the run keeps less of each file for every build:
// the header's bytes at their length, which shouldBuild reads; of the syntax tree, the package's name and its doc
// comment, all Import reads of it; and each import without its doc comment, which only saveCgo reads, and a file that
// imports "C" is go/build's to read (errorCgoFile). Of cohere's root, a header kept whole held 3.9 KB, and compacted
// 2.4 KB.
func compacted(read fileInfo) fileInfo {
	read.header = bytes.Clone(read.header)
	if read.parsed != nil {
		read.parsed = &ast.File{Package: read.parsed.Package, Name: read.parsed.Name, Doc: read.parsed.Doc}
	}
	imports := make([]fileImport, len(read.imports))
	for index, each := range read.imports {
		imports[index] = fileImport{path: each.path, pos: each.pos, position: each.position}
	}
	read.imports = imports
	// Import reads no position through the file set once each is resolved, so the set and its file go with the read.
	read.fset = nil
	return read
}

// resolved is read with each import's position resolved in the file set read was read into, the one position the
// copied Import reads through a file set (fileImport's position): the embeds' and the directives' readGoInfo resolves
// itself. Resolved once by the read, no build reads a position through another's file set, whose file locks to answer.
func resolved(read fileInfo) fileInfo {
	imports := make([]fileImport, len(read.imports))
	for index, each := range read.imports {
		each.position = read.fset.Position(each.pos)
		imports[index] = each
	}
	read.imports = imports
	return read
}
