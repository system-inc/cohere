package rule

import (
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// ProgramRead is what a rule reads through ctx.Program beyond the file it was handed, declared so the
// findings cache can tell which reads its key already covers (#b8k3bp6).
//
// It replaced one flag, ReadsProgram, that said "reaches the program" and so made 45 rules uncacheable,
// about a third of the rule time on ahra, when most of them read nothing the key does not already
// hold. The kinds are split by what covers them:
//
//	ReadsCompilerOptions   the tsconfig's options and the run's directory: the findings key hashes
//	                       the tsconfig chain and the root
//	ReadsDefaultLibrary    whether a declaration lives in a lib file: the libs are embedded in the
//	                       binary, which is in the key
//	ReadsModuleResolution  where this file's own imports resolve: the type fingerprint's edges come
//	                       from resolution, and every file it can reach is in the closure or the
//	                       global component
//	ReadsOtherFiles        anything else: the program's file list, a file by name or by resolution,
//	                       another file's imports, the file system. Nothing per file covers it, so a
//	                       rule declaring it is never cached
//	ReadsDesignSystem      the files the Tailwind design system is built from: its entry point, the
//	                       stylesheets it imports, and the paths it probed and did not find. Read
//	                       through DesignSystemFS, which records every one, so the set is observed
//	                       rather than listed and can key a cache of its own (#pyhm2t2)
//
// A rule reads ctx.Program through a view that refuses every method outside its declaration, with a
// panic naming the rule and the read. The walk contains that panic to the rule and names it, so a rule
// cannot read more than it declared without every run, fixture and crash guard saying so. Declaring
// more than a rule reads costs a cache miss and nothing else, the same asymmetry NeedsTypeChecker has.
type ProgramRead uint8

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

// String lists the reads a declaration holds, for refusals and tests.
func (reads ProgramRead) String() string {
	names := []string{}
	for _, read := range []ProgramRead{ReadsCompilerOptions, ReadsDefaultLibrary, ReadsModuleResolution, ReadsOtherFiles, ReadsDesignSystem} {
		if reads&read != 0 {
			names = append(names, programReadNames[read])
		}
	}
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, " | ")
}

// ResolvedModule is what a rule may read of where one of its file's imports resolved.
type ResolvedModule struct {
	ResolvedFileName        string
	IsExternalLibraryImport bool
	// PackageName is the resolved package's name, or empty when the import is not a package's.
	PackageName string
}

// IsResolved reports whether the import resolved to a file.
func (resolved ResolvedModule) IsResolved() bool {
	return resolved.ResolvedFileName != ""
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
	IsSourceFileDefaultLibrary(path tspath.Path) bool
	DefaultLibraryPath() string

	// ReadsModuleResolution for the file the rule was handed, ReadsOtherFiles for any other.
	ResolveModule(file ast.HasFileName, specifier *ast.StringLiteralLike) ResolvedModule

	// ReadsOtherFiles.
	GetSourceFileForResolvedModule(fileName string) *ast.SourceFile
	SourceFiles() []*ast.SourceFile
	GetSourceFile(fileName string) *ast.SourceFile
	FS() vfs.FS

	// ReadsDesignSystem: a file system that records every path it is asked about and refuses writes.
	DesignSystemFS() *RecordingFS

	// Identity reads nothing.
	Identity() ProgramIdentity
}

// ViewProgram gives one rule its view of program while it runs on sourceFile: every method outside the
// rule's ProgramReads panics, naming the rule and the read it did not declare. A nil program is a nil
// Program, so `ctx.Program == nil` still says the program could not be built.
func ViewProgram(program *compiler.Program, sourceFile *ast.SourceFile, subject Rule) Program {
	if program == nil {
		return nil
	}
	return &programView{program: program, sourceFile: sourceFile, ruleName: subject.Name, reads: subject.ProgramReads}
}

// ViewProgramForEach is ViewProgram for every rule on one file, in one allocation, which is what the
// walk calls once per file.
func ViewProgramForEach(program *compiler.Program, sourceFile *ast.SourceFile, rules []Rule) []Program {
	views := make([]Program, len(rules))
	if program == nil {
		return views
	}
	backing := make([]programView, len(rules))
	for index, subject := range rules {
		backing[index] = programView{program: program, sourceFile: sourceFile, ruleName: subject.Name, reads: subject.ProgramReads}
		views[index] = &backing[index]
	}
	return views
}

type programView struct {
	program    *compiler.Program
	sourceFile *ast.SourceFile
	ruleName   string
	reads      ProgramRead
}

// require panics unless the rule declared read.
func (view *programView) require(read ProgramRead, method string) {
	if view.reads&read == 0 {
		panic(fmt.Sprintf("rule %s called Program.%s, which is %s, and declares ProgramReads %s; declare the read so the findings cache does not replay a stale verdict",
			view.ruleName, method, programReadNames[read], view.reads))
	}
}

func (view *programView) Options() *core.CompilerOptions {
	view.require(ReadsCompilerOptions, "Options")
	return view.program.Options()
}

func (view *programView) GetCurrentDirectory() string {
	view.require(ReadsCompilerOptions, "GetCurrentDirectory")
	return view.program.GetCurrentDirectory()
}

func (view *programView) UseCaseSensitiveFileNames() bool {
	view.require(ReadsCompilerOptions, "UseCaseSensitiveFileNames")
	return view.program.Host().FS().UseCaseSensitiveFileNames()
}

func (view *programView) IsSourceFileDefaultLibrary(path tspath.Path) bool {
	view.require(ReadsDefaultLibrary, "IsSourceFileDefaultLibrary")
	return view.program.IsSourceFileDefaultLibrary(path)
}

func (view *programView) DefaultLibraryPath() string {
	view.require(ReadsDefaultLibrary, "DefaultLibraryPath")
	return view.program.Host().DefaultLibraryPath()
}

func (view *programView) ResolveModule(file ast.HasFileName, specifier *ast.StringLiteralLike) ResolvedModule {
	// Another file's imports are not in this file's fingerprint.
	if view.sourceFile == nil || file.FileName() != view.sourceFile.FileName() {
		view.require(ReadsOtherFiles, "ResolveModule on another file")
	} else {
		view.require(ReadsModuleResolution, "ResolveModule")
	}
	resolved := view.program.GetResolvedModuleFromModuleSpecifier(file, specifier)
	if resolved == nil || !resolved.IsResolved() {
		return ResolvedModule{}
	}
	return ResolvedModule{
		ResolvedFileName:        resolved.ResolvedFileName,
		IsExternalLibraryImport: resolved.IsExternalLibraryImport,
		PackageName:             resolved.PackageId.Name,
	}
}

func (view *programView) GetSourceFileForResolvedModule(fileName string) *ast.SourceFile {
	view.require(ReadsOtherFiles, "GetSourceFileForResolvedModule")
	return view.program.GetSourceFileForResolvedModule(fileName)
}

func (view *programView) SourceFiles() []*ast.SourceFile {
	view.require(ReadsOtherFiles, "SourceFiles")
	return view.program.SourceFiles()
}

func (view *programView) GetSourceFile(fileName string) *ast.SourceFile {
	view.require(ReadsOtherFiles, "GetSourceFile")
	return view.program.GetSourceFile(fileName)
}

func (view *programView) FS() vfs.FS {
	view.require(ReadsOtherFiles, "FS")
	return view.program.Host().FS()
}

func (view *programView) DesignSystemFS() *RecordingFS {
	view.require(ReadsDesignSystem, "DesignSystemFS")
	recorder := NewRecordingFS(view.program.Host().FS())
	recordDesignSystemFS(view.program, recorder)
	return recorder
}

func (view *programView) Identity() ProgramIdentity {
	return ProgramIdentity{program: view.program}
}
