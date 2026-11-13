// cohere:copied std@go1.27.0 go/build/build.go
// cohere:until go/build lets a build context share each Go file's header
// read across contexts, as a hook on Context or a cache of readGoInfo, so
// one run's builds stop parsing every header again (#tb538na). Before
// then, once a toolchain's go/build moves past go1.27.0's, the tests fail
// until the copy is diffed against the new go/build and bumped.
//
// cohere: the in-process lister lists each of a run's builds through
// go/build's Context.ImportDir, which reads every Go file's header through
// readGoInfo and parses it again in every build, though the header and its
// parse depend on the file alone. Only which files a build selects, by
// name and by build constraint, depends on the build. go/build has no hook
// for the parse, so this copy carries the part of Import after the package
// is found, and the reads it calls, and reads each Go file's header once
// for the run (readGoInfoOnce, in context.go). go/build itself still finds
// the package (Context.ImportDir with FindOnly, in context.go's
// ImportDirectory), and every build still matches each file by its own
// context.
//
// cohere: every line outside the marked changes is go1.27.0's
// src/go/build/build.go, byte for byte, which a test in
// internal/go_engine/copied holds true against the toolchain's source, and
// a test beside this file holds the toolchain's source to go1.27.0's, byte
// for byte, and its release to those the binary reads through the copy
// under (sharedReleases, in context.go). The changes are eight: the
// imports; the code dropped, each run where it was; Import's head, now
// importFiles's; the cgo files, sent back to go/build (errorCgoFile); each
// import's position, resolved once by the read and held beside it
// (fileImport's position); NoGoError's literal keyed, since it is
// go/build's type here; the shared read of a Go file's header in
// matchFile; and the linkname dropped from parseFileHeader.

// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/doc"
	"go/token"
	// cohere: no internal package can be imported from this module. The copy calls syslist alone of them, copied
	// cohere: below this package; the others served the code it drops.
	"io"
	"io/fs"
	"os"
	// cohere: os/exec and path served the code the copy drops.
	"path/filepath"
	// cohere: runtime served the code the copy drops.
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
	// cohere: package unsafe served linkname alone, which the copy drops. syslist is internal/syslist's copy.

	"github.com/system-inc/cohere/internal/go_engine/load/build/syslist"
)

// cohere: dropped: Context, which context.go's Context embeds, and splitPathList.
// joinPath calls ctxt.JoinPath (if not nil) or else filepath.Join.
func (ctxt *Context) joinPath(elem ...string) string {
	if f := ctxt.JoinPath; f != nil {
		return f(elem...)
	}
	return filepath.Join(elem...)
}

// cohere: dropped: isAbsPath.
// isDir calls ctxt.IsDir (if not nil) or else uses os.Stat.
func (ctxt *Context) isDir(path string) bool {
	if f := ctxt.IsDir; f != nil {
		return f(path)
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// cohere: dropped: hasSubdir, and the default build context and Package, which context.go aliases.
// readDir calls ctxt.ReadDir (if not nil) or else os.ReadDir.
func (ctxt *Context) readDir(path string) ([]fs.DirEntry, error) {
	// TODO: add a fs.DirEntry version of Context.ReadDir
	if f := ctxt.ReadDir; f != nil {
		fis, err := f(path)
		if err != nil {
			return nil, err
		}
		des := make([]fs.DirEntry, len(fis))
		for i, fi := range fis {
			des[i] = fs.FileInfoToDirEntry(fi)
		}
		return des, nil
	}
	return os.ReadDir(path)
}

// openFile calls ctxt.OpenFile (if not nil) or else os.Open.
func (ctxt *Context) openFile(path string) (io.ReadCloser, error) {
	if fn := ctxt.OpenFile; fn != nil {
		return fn(path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err // nil interface
	}
	return f, nil
}

// cohere: dropped: isFile, gopath, SrcDirs, the defaults, and ImportMode, Package, Directive, NoGoError and
// cohere: MultiplePackageError, which context.go aliases.
func nameExt(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return ""
	}
	return name[i:]
}

// cohere: dropped: installgoroot, and Import up to where FindOnly returns, which go/build runs itself for the
// cohere: copy (context.go's ImportDirectory, which calls Context.ImportDir with FindOnly). importFiles is the rest
// cohere: of Import, given the Package that found, with binaryOnly and pkgerr as Import leaves them for a local
// cohere: import by the gc compiler, the only one the in-process lister reads with.
func (ctxt *Context) importFiles(p *Package, mode ImportMode) (*Package, error) {
	binaryOnly := false // cohere: a local import is never binary only.
	var pkgerr error    // cohere: nil for the gc compiler.
	if mode&FindOnly != 0 {
		return p, pkgerr
	}
	if binaryOnly && (mode&AllowBinary) != 0 {
		return p, pkgerr
	}

	if ctxt.Compiler == "gccgo" && p.Goroot {
		// gccgo has no sources for GOROOT packages.
		return p, nil
	}

	dirs, err := ctxt.readDir(p.Dir)
	if err != nil {
		return p, err
	}

	var badGoError error
	badGoFiles := make(map[string]bool)
	badGoFile := func(name string, err error) {
		if badGoError == nil {
			badGoError = err
		}
		if !badGoFiles[name] {
			p.InvalidGoFiles = append(p.InvalidGoFiles, name)
			badGoFiles[name] = true
		}
	}

	var Sfiles []string // files with ".S"(capital S)/.sx(capital s equivalent for case insensitive filesystems)
	var firstFile, firstCommentFile string
	embedPos := make(map[string][]token.Position)
	testEmbedPos := make(map[string][]token.Position)
	xTestEmbedPos := make(map[string][]token.Position)
	importPos := make(map[string][]token.Position)
	testImportPos := make(map[string][]token.Position)
	xTestImportPos := make(map[string][]token.Position)
	allTags := make(map[string]bool)
	fset := token.NewFileSet()
	for _, d := range dirs {
		if d.IsDir() {
			continue
		}
		if d.Type() == fs.ModeSymlink {
			if ctxt.isDir(ctxt.joinPath(p.Dir, d.Name())) {
				// Symlinks to directories are not source files.
				continue
			}
		}

		name := d.Name()
		ext := nameExt(name)

		info, err := ctxt.matchFile(p.Dir, name, allTags, &p.BinaryOnly, fset)
		if err != nil && strings.HasSuffix(name, ".go") {
			badGoFile(name, err)
			continue
		}
		if info == nil {
			if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
				// not due to build constraints - don't report
			} else if ext == ".go" {
				p.IgnoredGoFiles = append(p.IgnoredGoFiles, name)
			} else if fileListForExt(p, ext) != nil {
				p.IgnoredOtherFiles = append(p.IgnoredOtherFiles, name)
			}
			continue
		}

		// Going to save the file. For non-Go files, can stop here.
		switch ext {
		case ".go":
			// keep going
		case ".S", ".sx":
			// special case for cgo, handled at end
			Sfiles = append(Sfiles, name)
			continue
		default:
			if list := fileListForExt(p, ext); list != nil {
				*list = append(*list, name)
			}
			continue
		}

		data, filename := info.header, info.name

		if info.parseErr != nil {
			badGoFile(name, info.parseErr)
			// Fall through: we might still have a partial AST in info.parsed,
			// and we want to list files with parse errors anyway.
		}

		var pkg string
		if info.parsed != nil {
			pkg = info.parsed.Name.Name
			if pkg == "documentation" {
				p.IgnoredGoFiles = append(p.IgnoredGoFiles, name)
				continue
			}
		}

		isTest := strings.HasSuffix(name, "_test.go")
		isXTest := false
		if isTest && strings.HasSuffix(pkg, "_test") && p.Name != pkg {
			isXTest = true
			pkg = pkg[:len(pkg)-len("_test")]
		}

		if p.Name == "" {
			p.Name = pkg
			firstFile = name
		} else if pkg != p.Name {
			// TODO(#45999): The choice of p.Name is arbitrary based on file iteration
			// order. Instead of resolving p.Name arbitrarily, we should clear out the
			// existing name and mark the existing files as also invalid.
			badGoFile(name, &MultiplePackageError{
				Dir:      p.Dir,
				Packages: []string{p.Name, pkg},
				Files:    []string{firstFile, name},
			})
		}
		// Grab the first package comment as docs, provided it is not from a test file.
		if info.parsed != nil && info.parsed.Doc != nil && p.Doc == "" && !isTest && !isXTest {
			//cohere:ignore SA1019 upstream's go/build calls it, and the copy has to read the Doc go/build reads.
			p.Doc = doc.Synopsis(info.parsed.Doc.Text())
		}

		if mode&ImportComment != 0 {
			qcom, line := findImportComment(data)
			if line != 0 {
				com, err := strconv.Unquote(qcom)
				if err != nil {
					badGoFile(name, fmt.Errorf("%s:%d: cannot parse import comment", filename, line))
				} else if p.ImportComment == "" {
					p.ImportComment = com
					firstCommentFile = name
				} else if p.ImportComment != com {
					badGoFile(name, fmt.Errorf("found import comments %q (%s) and %q (%s) in %s", p.ImportComment, firstCommentFile, com, name, p.Dir))
				}
			}
		}

		// Record imports and information about cgo.
		isCgo := false
		for _, imp := range info.imports {
			if imp.path == "C" {
				if isTest {
					badGoFile(name, fmt.Errorf("use of cgo in test %s not supported", filename))
					continue
				}
				// cohere: a file that imports "C" sends its directory back to go/build's own ImportDir
				// cohere: (errorCgoFile, in context.go), which reads its #cgo lines; the copy carries no saveCgo, and
				// cohere: so isCgo stays false.
				return p, errorCgoFile
			}
		}

		var fileList *[]string
		var importMap, embedMap map[string][]token.Position
		var directives *[]Directive
		switch {
		case isCgo:
			allTags["cgo"] = true
			if ctxt.CgoEnabled {
				fileList = &p.CgoFiles
				importMap = importPos
				embedMap = embedPos
				directives = &p.Directives
			} else {
				// Ignore imports and embeds from cgo files if cgo is disabled.
				fileList = &p.IgnoredGoFiles
			}
		case isXTest:
			fileList = &p.XTestGoFiles
			importMap = xTestImportPos
			embedMap = xTestEmbedPos
			directives = &p.XTestDirectives
		case isTest:
			fileList = &p.TestGoFiles
			importMap = testImportPos
			embedMap = testEmbedPos
			directives = &p.TestDirectives
		default:
			fileList = &p.GoFiles
			importMap = importPos
			embedMap = embedPos
			directives = &p.Directives
		}
		*fileList = append(*fileList, name)
		if importMap != nil {
			for _, imp := range info.imports {
				// cohere: the position the read resolved once, in its own file set (readGoInfoOnce, in context.go), so
				// cohere: no build reads a position through another build's file set, or locks it to.
				importMap[imp.path] = append(importMap[imp.path], imp.position)
			}
		}
		if embedMap != nil {
			for _, emb := range info.embeds {
				embedMap[emb.pattern] = append(embedMap[emb.pattern], emb.pos)
			}
		}
		if directives != nil {
			*directives = append(*directives, info.directives...)
		}
	}

	for tag := range allTags {
		p.AllTags = append(p.AllTags, tag)
	}
	slices.Sort(p.AllTags)

	p.EmbedPatterns, p.EmbedPatternPos = cleanDecls(embedPos)
	p.TestEmbedPatterns, p.TestEmbedPatternPos = cleanDecls(testEmbedPos)
	p.XTestEmbedPatterns, p.XTestEmbedPatternPos = cleanDecls(xTestEmbedPos)

	p.Imports, p.ImportPos = cleanDecls(importPos)
	p.TestImports, p.TestImportPos = cleanDecls(testImportPos)
	p.XTestImports, p.XTestImportPos = cleanDecls(xTestImportPos)

	// add the .S/.sx files only if we are using cgo
	// (which means gcc will compile them).
	// The standard assemblers expect .s files.
	if len(p.CgoFiles) > 0 {
		p.SFiles = append(p.SFiles, Sfiles...)
		slices.Sort(p.SFiles)
	} else {
		p.IgnoredOtherFiles = append(p.IgnoredOtherFiles, Sfiles...)
		slices.Sort(p.IgnoredOtherFiles)
	}

	if badGoError != nil {
		return p, badGoError
	}
	if len(p.GoFiles)+len(p.CgoFiles)+len(p.TestGoFiles)+len(p.XTestGoFiles) == 0 {
		// cohere: keyed, since NoGoError is go/build's, whose literals vet's composites check holds to keyed fields.
		return p, &NoGoError{Dir: p.Dir}
	}
	return p, pkgerr
}

func fileListForExt(p *Package, ext string) *[]string {
	switch ext {
	case ".c":
		return &p.CFiles
	case ".cc", ".cpp", ".cxx":
		return &p.CXXFiles
	case ".m":
		return &p.MFiles
	case ".h", ".hh", ".hpp", ".hxx":
		return &p.HFiles
	case ".f", ".F", ".for", ".f90":
		return &p.FFiles
	case ".s", ".S", ".sx":
		return &p.SFiles
	case ".swig":
		return &p.SwigFiles
	case ".swigcxx":
		return &p.SwigCXXFiles
	case ".syso":
		return &p.SysoFiles
	}
	return nil
}

// cohere: dropped: uniq, errNoModules, importGo and hasGoFiles, which find a package.
func findImportComment(data []byte) (s string, line int) {
	// expect keyword package
	word, data := parseWord(data)
	if string(word) != "package" {
		return "", 0
	}

	// expect package name
	_, data = parseWord(data)

	// now ready for import comment, a // or /* */ comment
	// beginning and ending on the current line.
	for len(data) > 0 && (data[0] == ' ' || data[0] == '\t' || data[0] == '\r') {
		data = data[1:]
	}

	var comment []byte
	switch {
	case bytes.HasPrefix(data, slashSlash):
		comment, _, _ = bytes.Cut(data[2:], newline)
	case bytes.HasPrefix(data, slashStar):
		var ok bool
		comment, _, ok = bytes.Cut(data[2:], starSlash)
		if !ok {
			// malformed comment
			return "", 0
		}
		if bytes.Contains(comment, newline) {
			return "", 0
		}
	}
	comment = bytes.TrimSpace(comment)

	// split comment into `import`, `"pkg"`
	word, arg := parseWord(comment)
	if string(word) != "import" {
		return "", 0
	}

	line = 1 + bytes.Count(data[:cap(data)-cap(arg)], newline)
	return strings.TrimSpace(string(arg)), line
}

var (
	slashSlash = []byte("//")
	slashStar  = []byte("/*")
	starSlash  = []byte("*/")
	newline    = []byte("\n")
)

// skipSpaceOrComment returns data with any leading spaces or comments removed.
func skipSpaceOrComment(data []byte) []byte {
	for len(data) > 0 {
		switch data[0] {
		case ' ', '\t', '\r', '\n':
			data = data[1:]
			continue
		case '/':
			if bytes.HasPrefix(data, slashSlash) {
				i := bytes.Index(data, newline)
				if i < 0 {
					return nil
				}
				data = data[i+1:]
				continue
			}
			if bytes.HasPrefix(data, slashStar) {
				data = data[2:]
				i := bytes.Index(data, starSlash)
				if i < 0 {
					return nil
				}
				data = data[i+2:]
				continue
			}
		}
		break
	}
	return data
}

// parseWord skips any leading spaces or comments in data
// and then parses the beginning of data as an identifier or keyword,
// returning that word and what remains after the word.
func parseWord(data []byte) (word, rest []byte) {
	data = skipSpaceOrComment(data)

	// Parse past leading word characters.
	rest = data
	for {
		r, size := utf8.DecodeRune(rest)
		if unicode.IsLetter(r) || '0' <= r && r <= '9' || r == '_' {
			rest = rest[size:]
			continue
		}
		break
	}

	word = data[:len(data)-len(rest)]
	if len(word) == 0 {
		return nil, nil
	}

	return word, rest
}

// cohere: dropped: MatchFile.
var dummyPkg Package

// fileInfo records information learned about a file included in a build.
type fileInfo struct {
	name       string // full name including dir
	header     []byte
	fset       *token.FileSet
	parsed     *ast.File
	parseErr   error
	imports    []fileImport
	embeds     []fileEmbed
	directives []Directive
}

type fileImport struct {
	path string
	pos  token.Pos
	doc  *ast.CommentGroup
	// cohere: pos resolved in the file set the file was read into, once, by readGoInfoOnce (context.go).
	position token.Position
}

type fileEmbed struct {
	pattern string
	pos     token.Position
}

// matchFile determines whether the file with the given name in the given directory
// should be included in the package being constructed.
// If the file should be included, matchFile returns a non-nil *fileInfo (and a nil error).
// Non-nil errors are reserved for unexpected problems.
//
// If name denotes a Go program, matchFile reads until the end of the
// imports and returns that section of the file in the fileInfo's header field,
// even though it only considers text until the first non-comment
// for go:build lines.
//
// If allTags is non-nil, matchFile records any encountered build tag
// by setting allTags[tag] = true.
func (ctxt *Context) matchFile(dir, name string, allTags map[string]bool, binaryOnly *bool, fset *token.FileSet) (*fileInfo, error) {
	if strings.HasPrefix(name, "_") ||
		strings.HasPrefix(name, ".") {
		return nil, nil
	}

	i := strings.LastIndex(name, ".")
	if i < 0 {
		i = len(name)
	}
	ext := name[i:]

	if ext != ".go" && fileListForExt(&dummyPkg, ext) == nil {
		// skip
		return nil, nil
	}

	if !ctxt.goodOSArchFile(name, allTags) && !ctxt.UseAllFiles {
		return nil, nil
	}

	info := &fileInfo{name: ctxt.joinPath(dir, name), fset: fset}
	if ext == ".syso" {
		// binary, no reading
		return info, nil
	}

	// cohere: a Go file is opened by readGoInfoOnce (in context.go), and only by the build that reads its header
	// cohere: first, since every other build takes that read; this opens every other file, as upstream opens it.
	var f io.ReadCloser
	var err, openErr error
	if !strings.HasSuffix(name, ".go") {
		f, openErr = ctxt.openFile(info.name)
	}
	if openErr != nil {
		return nil, openErr
	}

	if strings.HasSuffix(name, ".go") {
		// cohere: the header is read once for the run, and its open's failure is returned as the open's above.
		if *info, openErr, err = ctxt.readGoInfoOnce(info.name, info.fset); openErr != nil {
			return nil, openErr
		}
		if strings.HasSuffix(name, "_test.go") {
			binaryOnly = nil // ignore //go:binary-only-package comments in _test.go files
		}
	} else {
		binaryOnly = nil // ignore //go:binary-only-package comments in non-Go sources
		info.header, err = readComments(f)
		// cohere: closed here, since only a file other than Go is opened here.
		f.Close()
	}
	// cohere: upstream closes f here, which the branch above does.
	if err != nil {
		return info, fmt.Errorf("read %s: %v", info.name, err)
	}

	// Look for go:build comments to accept or reject the file.
	ok, sawBinaryOnly, err := ctxt.shouldBuild(info.header, allTags)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	if !ok && !ctxt.UseAllFiles {
		return nil, nil
	}

	if binaryOnly != nil && sawBinaryOnly {
		*binaryOnly = true
	}

	return info, nil
}

func cleanDecls(m map[string][]token.Position) ([]string, map[string][]token.Position) {
	all := make([]string, 0, len(m))
	for path := range m {
		all = append(all, path)
	}
	slices.Sort(all)
	return all, m
}

// cohere: dropped: the Import and ImportDir shorthands for Default.
var (
	plusBuild = []byte("+build")

	goBuildComment = []byte("//go:build")

	errMultipleGoBuild = errors.New("multiple //go:build comments")
)

func isGoBuildComment(line []byte) bool {
	if !bytes.HasPrefix(line, goBuildComment) {
		return false
	}
	line = bytes.TrimSpace(line)
	rest := line[len(goBuildComment):]
	return len(rest) == 0 || len(bytes.TrimSpace(rest)) < len(rest)
}

// Special comment denoting a binary-only package.
// See https://golang.org/design/2775-binary-only-packages
// for more about the design of binary-only packages.
var binaryOnlyComment = []byte("//go:binary-only-package")

// shouldBuild reports whether it is okay to use this file,
// The rule is that in the file's leading run of // comments
// and blank lines, which must be followed by a blank line
// (to avoid including a Go package clause doc comment),
// lines beginning with '//go:build' are taken as build directives.
//
// The file is accepted only if each such line lists something
// matching the file. For example:
//
//	//go:build windows linux
//
// marks the file as applicable only on Windows and Linux.
//
// For each build tag it consults, shouldBuild sets allTags[tag] = true.
//
// shouldBuild reports whether the file should be built
// and whether a //go:binary-only-package comment was found.
func (ctxt *Context) shouldBuild(content []byte, allTags map[string]bool) (shouldBuild, binaryOnly bool, err error) {
	// Identify leading run of // comments and blank lines,
	// which must be followed by a blank line.
	// Also identify any //go:build comments.
	content, goBuild, sawBinaryOnly, err := parseFileHeader(content)
	if err != nil {
		return false, false, err
	}

	// If //go:build line is present, it controls.
	// Otherwise fall back to +build processing.
	switch {
	case goBuild != nil:
		x, err := constraint.Parse(string(goBuild))
		if err != nil {
			return false, false, fmt.Errorf("parsing //go:build line: %v", err)
		}
		shouldBuild = ctxt.eval(x, allTags)

	default:
		shouldBuild = true
		p := content
		for len(p) > 0 {
			line := p
			if i := bytes.IndexByte(line, '\n'); i >= 0 {
				line, p = line[:i], p[i+1:]
			} else {
				p = p[len(p):]
			}
			line = bytes.TrimSpace(line)
			if !bytes.HasPrefix(line, slashSlash) || !bytes.Contains(line, plusBuild) {
				continue
			}
			text := string(line)
			if !constraint.IsPlusBuild(text) {
				continue
			}
			if x, err := constraint.Parse(text); err == nil {
				if !ctxt.eval(x, allTags) {
					shouldBuild = false
				}
			}
		}
	}

	return shouldBuild, sawBinaryOnly, nil
}

// parseFileHeader should be an internal detail,
// but widely used packages access it using linkname.
// Notable members of the hall of shame include:
//   - github.com/bazelbuild/bazel-gazelle
//
// Do not remove or change the type signature.
// See go.dev/issue/67401.
//
// cohere: no linkname, which needs package unsafe imported; nothing links to this copy.
func parseFileHeader(content []byte) (trimmed, goBuild []byte, sawBinaryOnly bool, err error) {
	end := 0
	p := content
	ended := false       // found non-blank, non-// line, so stopped accepting //go:build lines
	inSlashStar := false // in /* */ comment

Lines:
	for len(p) > 0 {
		line := p
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, p = line[:i], p[i+1:]
		} else {
			p = p[len(p):]
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 && !ended { // Blank line
			// Remember position of most recent blank line.
			// When we find the first non-blank, non-// line,
			// this "end" position marks the latest file position
			// where a //go:build line can appear.
			// (It must appear _before_ a blank line before the non-blank, non-// line.
			// Yes, that's confusing, which is part of why we moved to //go:build lines.)
			// Note that ended==false here means that inSlashStar==false,
			// since seeing a /* would have set ended==true.
			end = len(content) - len(p)
			continue Lines
		}
		if !bytes.HasPrefix(line, slashSlash) { // Not comment line
			ended = true
		}

		if !inSlashStar && isGoBuildComment(line) {
			if goBuild != nil {
				return nil, nil, false, errMultipleGoBuild
			}
			goBuild = line
		}
		if !inSlashStar && bytes.Equal(line, binaryOnlyComment) {
			sawBinaryOnly = true
		}

	Comments:
		for len(line) > 0 {
			if inSlashStar {
				if i := bytes.Index(line, starSlash); i >= 0 {
					inSlashStar = false
					line = bytes.TrimSpace(line[i+len(starSlash):])
					continue Comments
				}
				continue Lines
			}
			if bytes.HasPrefix(line, slashSlash) {
				continue Lines
			}
			if bytes.HasPrefix(line, slashStar) {
				inSlashStar = true
				line = bytes.TrimSpace(line[len(slashStar):])
				continue Comments
			}
			// Found non-comment text.
			break Lines
		}
	}

	return content[:end], goBuild, sawBinaryOnly, nil
}

// cohere: dropped: saveCgo and its helpers, since a directory with cgo files is go/build's to read
// cohere: (errorCgoFile), and matchAuto, which only saveCgo called.
func (ctxt *Context) eval(x constraint.Expr, allTags map[string]bool) bool {
	return x.Eval(func(tag string) bool { return ctxt.matchTag(tag, allTags) })
}

// matchTag reports whether the name is one of:
//
//	cgo (if cgo is enabled)
//	$GOOS
//	$GOARCH
//	ctxt.Compiler
//	linux (if GOOS = android)
//	solaris (if GOOS = illumos)
//	darwin (if GOOS = ios)
//	unix (if this is a Unix GOOS)
//	boringcrypto (if GOEXPERIMENT=boringcrypto is enabled)
//	tag (if tag is listed in ctxt.BuildTags, ctxt.ToolTags, or ctxt.ReleaseTags)
//
// It records all consulted tags in allTags.
func (ctxt *Context) matchTag(name string, allTags map[string]bool) bool {
	if allTags != nil {
		allTags[name] = true
	}

	// special tags
	if ctxt.CgoEnabled && name == "cgo" {
		return true
	}
	if name == ctxt.GOOS || name == ctxt.GOARCH || name == ctxt.Compiler {
		return true
	}
	if ctxt.GOOS == "android" && name == "linux" {
		return true
	}
	if ctxt.GOOS == "illumos" && name == "solaris" {
		return true
	}
	if ctxt.GOOS == "ios" && name == "darwin" {
		return true
	}
	if name == "unix" && syslist.UnixOS[ctxt.GOOS] {
		return true
	}
	if name == "boringcrypto" {
		name = "goexperiment.boringcrypto" // boringcrypto is an old name for goexperiment.boringcrypto
	}

	// other tags
	return slices.Contains(ctxt.BuildTags, name) || slices.Contains(ctxt.ToolTags, name) ||
		slices.Contains(ctxt.ReleaseTags, name)
}

// goodOSArchFile returns false if the name contains a $GOOS or $GOARCH
// suffix which does not match the current system.
// The recognized name formats are:
//
//	name_$(GOOS).*
//	name_$(GOARCH).*
//	name_$(GOOS)_$(GOARCH).*
//	name_$(GOOS)_test.*
//	name_$(GOARCH)_test.*
//	name_$(GOOS)_$(GOARCH)_test.*
//
// Exceptions:
// if GOOS=android, then files with GOOS=linux are also matched.
// if GOOS=illumos, then files with GOOS=solaris are also matched.
// if GOOS=ios, then files with GOOS=darwin are also matched.
func (ctxt *Context) goodOSArchFile(name string, allTags map[string]bool) bool {
	name, _, _ = strings.Cut(name, ".")

	// Before Go 1.4, a file called "linux.go" would be equivalent to having a
	// build tag "linux" in that file. For Go 1.4 and beyond, we require this
	// auto-tagging to apply only to files with a non-empty prefix, so
	// "foo_linux.go" is tagged but "linux.go" is not. This allows new operating
	// systems, such as android, to arrive without breaking existing code with
	// innocuous source code in "android.go". The easiest fix: cut everything
	// in the name before the initial _.
	i := strings.Index(name, "_")
	if i < 0 {
		return true
	}
	name = name[i:] // ignore everything before first _

	l := strings.Split(name, "_")
	if n := len(l); n > 0 && l[n-1] == "test" {
		l = l[:n-1]
	}
	n := len(l)
	if n >= 2 && syslist.KnownOS[l[n-2]] && syslist.KnownArch[l[n-1]] {
		if allTags != nil {
			// In case we short-circuit on l[n-1].
			allTags[l[n-2]] = true
		}
		return ctxt.matchTag(l[n-1], allTags) && ctxt.matchTag(l[n-2], allTags)
	}
	if n >= 1 && (syslist.KnownOS[l[n-1]] || syslist.KnownArch[l[n-1]]) {
		return ctxt.matchTag(l[n-1], allTags)
	}
	return true
}

// cohere: dropped: ToolDir.
// IsLocalImport reports whether the import path is
// a local import path, like ".", "..", "./foo", or "../foo".
func IsLocalImport(path string) bool {
	return path == "." || path == ".." ||
		strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

// ArchChar returns "?" and an error.
// In earlier versions of Go, the returned string was used to derive
// the compiler and linker tool names, the default object file suffix,
// and the default linker output name. As of Go 1.5, those strings
// no longer vary by architecture; they are compile, link, .o, and a.out, respectively.
func ArchChar(goarch string) (string, error) {
	return "?", errors.New("architecture letter no longer used")
}
