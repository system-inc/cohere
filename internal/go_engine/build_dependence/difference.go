package build_dependence

import (
	"bytes"
	"go/ast"
	"go/scanner"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// Difference is what one extra build changes against the host, as its file sets say: the graph's judgment of the
// build (Against) reads nothing else of it.
type Difference struct {
	// Packages are the packages whose files differ, by package ID: each package both builds have whose files differ or
	// which the cgo tool of either translated, and each package only the build has, with all its files.
	Packages map[string]PackageDifference

	// HostOnlyPackages are the IDs of the packages only the host has, every unit of which is build-dependent.
	HostOnlyPackages map[string]bool

	// BuildOnlyPackages are the IDs of the packages only the build has, whose files Packages holds. None of their units
	// is the host's, so they are counted apart, as work the build alone must check.
	BuildOnlyPackages map[string]bool

	// DependentObject answers for an object of a package the graph does not hold, a dependency checked without
	// syntax: whether its name bit, and its underlying bit, are set in the build. nil means no such object is
	// dependent.
	DependentObject func(object types.Object) (name bool, underlying bool)

	// DependentName answers the same for such an object as a record read back from the store names it, which has no
	// live object (#6a3kjvz): by its package's path and its local name there, its bits, and whether a unit of the
	// answering graph declares it. One the answering graph's package holds but no unit declares is read through its
	// type, as the record holds that (answered false); one of a package that graph does not hold answers both bits set,
	// failing wide. nil, with DependentObject set, fails every such object wide.
	DependentName func(path string, local string) (name bool, underlying bool, declared bool)
}

// PackageDifference is how one package's files differ between the host and the build.
type PackageDifference struct {
	// HostOnlyFiles are the files the host compiles and the build does not, as written (each package's GoFiles), so
	// they match the graph's units' files, which a reader sees. Every unit in one is build-dependent.
	HostOnlyFiles []string

	// SharedFiles are the files both compile, as written. A package with a host-only file has every unit
	// build-dependent whose file, as a reader sees it, is not one of these: a //line directive in a host-only file can
	// name its units by a file no build lists, which matches no host-only file, and the record keeps no other name of a
	// unit's file to match instead (#ss4t5g6). One naming a file both compile is EveryUnitDependent's to seed.
	SharedFiles []string

	// EveryUnitDependent says every unit of the package is build-dependent, in memory only (#ss4t5g6): a file the two
	// builds do not share, host-only or build-only, holds a line directive (HoldsLineDirective), which can name its
	// units by any file, one both builds compile among them, or a build-only file could not be read or parsed, so it is
	// in no BuildOnlyFiles and what it declares is unknown.
	EveryUnitDependent bool

	// BuildOnlyFiles are the files only the build compiles, parsed for syntax alone: a method one declares changes
	// its receiver's method set, a name of the universe one declares changes what that name means in the package's
	// shared files, and its units are counted apart.
	BuildOnlyFiles []*ast.File

	// Workspace says whether a package only the build has belongs to the checked modules, which the graph cannot
	// say of a package it does not hold. A package both builds have is counted where the host's record puts it.
	Workspace bool

	// KeepsCgo says a cgo tool translated the package on either side (KeepsCgo): the build's cgo tool translated it,
	// under its own tags and flags, or the host's did, and the build translates it again, checks its cgo files as
	// written with package "C" faked, or drops them. Whatever the host's units hold of the C side may then be another
	// thing in the build, or nothing at all, with no file a list names saying so. Every unit of the host's that hangs
	// on the C side is then build-dependent (unitRecord.fromCgo).
	KeepsCgo bool
}

// The two spellings that open a line directive, which a file holding neither can hold none of.
var (
	lineCommentDirective  = []byte("//line ")
	blockCommentDirective = []byte("/*line ")
)

// HoldsLineDirective reports whether file, as read reads it, holds a line directive as go/scanner applies one, the
// scanner go/parser and so the load run (#mtrjfyn): a comment token, never text in a string or in a comment's prose,
// either `//line ` at the very start of its line or `/*line ` anywhere, whose text after the spelling ends in `:N` or
// `:N:M`, N above 0 (isLineDirective). read is the caller's, so its bytes are the ones the graph was built from, which
// in the engine are the run's held bytes rather than the disk's again (pipeline.holdsLineDirective). A file read cannot
// read is said to hold one, failing wide. It still over-approximates in one way, on purpose: a directive counts
// whatever file it names, the file's own name and a file the builds share among them, since only the caller knows
// which files a build compiles.
func HoldsLineDirective(file string, read func(file string) ([]byte, error)) bool {
	source, readError := read(file)
	if readError != nil {
		return true
	}
	// A file holding neither spelling holds no directive, and is never scanned.
	if !bytes.Contains(source, lineCommentDirective) && !bytes.Contains(source, blockCommentDirective) {
		return false
	}
	fileSet := token.NewFileSet()
	tokenFile := fileSet.AddFile(file, -1, len(source))
	var scanning scanner.Scanner
	// Ignored: a file that does not scan clean is still scanned to its end, every comment of it read, as go/scanner
	// reads it for the parser, which reports the error itself.
	scanning.Init(tokenFile, source, nil, scanner.ScanComments)
	for {
		position, scanned, literal := scanning.Scan()
		if scanned == token.EOF {
			return false
		}
		// The column as the file has it, before any directive scanned earlier moves the positions after it.
		if scanned == token.COMMENT && isLineDirective(literal, tokenFile.PositionFor(position, false).Column == 1) {
			return true
		}
	}
}

// isLineDirective reports whether comment, a comment token's text, is a line directive as go/scanner reads one
// (go/scanner's scanComment and updateLineInfo): `/*line ` anywhere, or `//line ` when atLineStart, the comment
// starting its line, whose text past the spelling, a block comment's closing `*/` taken off, ends in `:N`, or in
// `:N:M` with M above 0, N above 0 either way. Text that ends otherwise the scanner ignores, or reports as an invalid
// line or column, and moves no position.
func isLineDirective(comment string, atLineStart bool) bool {
	var text string
	switch {
	case strings.HasPrefix(comment, "/*line "):
		text = strings.TrimSuffix(comment[len("/*line "):], "*/")
	case strings.HasPrefix(comment, "//line ") && atLineStart:
		text = comment[len("//line "):]
	default:
		return false
	}
	before, last, isNumber := trailingNumber(text)
	if !isNumber {
		return false
	}
	// The `:N:M` form, the line before the column.
	if _, line, isLine := trailingNumber(before); isLine {
		return last > 0 && line > 0
	}
	return last > 0
}

// trailingNumber is text cut at its last colon: what comes before it, and the decimal number after it, false where
// text holds no colon or what follows the last one is no number.
func trailingNumber(text string) (string, uint64, bool) {
	colon := strings.LastIndexByte(text, ':')
	if colon < 0 {
		return "", 0, false
	}
	number, parseError := strconv.ParseUint(text[colon+1:], 10, 32)
	if parseError != nil {
		return "", 0, false
	}
	return text[:colon], number, true
}
