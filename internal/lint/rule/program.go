package rule

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

const (
	ReadsCompilerOptions ProgramRead = 1 << iota
	ReadsDefaultLibrary
	ReadsModuleResolution
	ReadsOtherFiles
	ReadsDesignSystem
)

// programReadNames names each read in a refusal.
var programReadNames = map[ProgramRead]string{
	ReadsCompilerOptions:  "ReadsCompilerOptions",
	ReadsDefaultLibrary:   "ReadsDefaultLibrary",
	ReadsModuleResolution: "ReadsModuleResolution",
	ReadsOtherFiles:       "ReadsOtherFiles",
	ReadsDesignSystem:     "ReadsDesignSystem",
}

// ProgramIdentity tells one program from another without handing out the program, so a rule may cache
// what it derived from a run and notice the next run, and cannot reach past its declaration through it.
type ProgramIdentity struct {
	program *compiler.Program
}

// Program is ctx.Program: the program, as far as a rule declared it reads it. Each method says which
// read it is.
type Program interface {
	// ReadsCompilerOptions.
	Options() *core.CompilerOptions
	GetCurrentDirectory() string
	UseCaseSensitiveFileNames() bool

	// ReadsDefaultLibrary.
	IsSourceFileDefaultLibrary(path tspath.PathKey) bool
	DefaultLibraryPath() string

	// ReadsModuleResolution for the file the rule was handed, ReadsOtherFiles for any other.
	ResolveModule(file ast.HasFileName, specifier *ast.StringLiteralLike) ResolvedModule

	// ReadsOtherFiles.
	GetSourceFileForResolvedModule(fileName string) *ast.SourceFile
	SourceFiles() []*ast.SourceFile
	GetSourceFile(fileName string) *ast.SourceFile
	FileSystem() vfs.FS

	// ReadsDesignSystem: a file system that records every path it is asked about and refuses writes.
	DesignSystemFileSystem() *RecordingFileSystem

	// Identity reads nothing.
	Identity() ProgramIdentity
}

// ViewProgram gives one rule its view of program while it runs on sourceFile: every method outside the
// rule's ProgramReads panics, naming the rule and the read it did not declare. A nil program is a nil
// Program, so `ctx.Program == nil` still says the program could not be built.
func ViewProgram(program *compiler.Program, sourceFile *ast.SourceFile, subject Traits) Program {
	if program == nil {
		return nil
	}
	return &programView{program: program, sourceFile: sourceFile, ruleName: subject.Name, reads: subject.ProgramReads}
}

// FingerprintPath is how a ProgramFingerprint names a file or directory: relative to the program's
// current directory, so the hash holds when the same tree is checked out somewhere else, and the same
// across fixtures built in different directories. Reading the current directory is ReadsCompilerOptions,
// so a rule calling this declares it, which costs nothing: the findings key already hashes the root.
//
// A path under the current directory is named by slicing off the directory and its separator, which
// allocates nothing. tspath's general answer splits both paths into components and joins them again, and
// a fingerprint names every file of the program, once per computation: on an edit run of ahra that was
// 1.6 GB (#9prvp67). The slice is the string tspath gives for a descendant, which
// TestFingerprintPathSlicesWhatTspathWouldJoin holds over every file of this repository, both ways a host
// can treat case. On a case-insensitive host a program's paths are canonical, lowercased, while the
// directory keeps its case, so the directory is compared as tspath lowers it; only an ASCII directory takes
// the slice, since ToFileNameLowerCase treats a few other characters specially. Anything else, a path
// outside the directory or a directory spelled otherwise, takes tspath's answer.
func FingerprintPath(program Program, path string) string {
	directory := program.GetCurrentDirectory()
	if inside, under := fingerprintPathUnder(directory, path, program.UseCaseSensitiveFileNames()); under {
		return inside
	}
	return tspath.GetRelativePathFromDirectory(directory, path, caseSensitivity(program.UseCaseSensitiveFileNames()))
}

// fingerprintPathUnder is the part of path below directory, when path is a descendant and the bytes say so:
// exactly, or on a case-insensitive host against the directory lowered as tspath lowers ASCII.
func fingerprintPathUnder(directory string, path string, caseSensitive bool) (string, bool) {
	if directory == "" || len(path) <= len(directory) {
		return "", false
	}
	for index := 0; index < len(directory); index++ {
		character := directory[index]
		if character >= 0x80 {
			return "", false
		}
		if !caseSensitive && 'A' <= character && character <= 'Z' {
			character += 'a' - 'A'
		}
		if path[index] != character {
			return "", false
		}
	}
	rest := path[len(directory):]
	if strings.HasSuffix(directory, "/") {
		return rest, true
	}
	if inside, separated := strings.CutPrefix(rest, "/"); separated && inside != "" {
		return inside, true
	}
	return "", false
}

// caseSensitivity is a program's answer to UseCaseSensitiveFileNames as the CaseSensitivity tspath takes.
func caseSensitivity(caseSensitive bool) tspath.CaseSensitivity {
	if caseSensitive {
		return tspath.CaseSensitive
	}
	return tspath.CaseInsensitive
}
