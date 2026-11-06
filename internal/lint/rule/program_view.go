package rule

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

type programView struct {
	program    *compiler.Program
	sourceFile *ast.SourceFile
	ruleName   string
	reads      ProgramRead

	// ended is set once the file the view was given is over (ProgramViewSlot.Release).
	ended bool
}

// live panics if the view's file is over.
func (view *programView) live(method string) {
	if view.ended {
		// Invariant: a rule keeps what a Program answered, never the Program, so no call reaches a view
		// after its file ends; one that does is a rule bug, made loud here rather than read from a freed file.
		panic(fmt.Sprintf(
			"rule %s called Program.%s after the file it was given ended; a Program is good for its file only, so keep what it answered rather than the Program",
			view.ruleName,
			method,
		))
	}
}

// require panics unless the view is live and the rule declared read.
func (view *programView) require(read ProgramRead, method string) {
	view.live(method)
	if view.reads&read == 0 {
		// Invariant: every rule declares the Program reads it makes, which is what keys its cached findings;
		// an undeclared read would replay a stale verdict, so it stops the rule here instead.
		panic(fmt.Sprintf(
			"rule %s called Program.%s, which is %s, and declares ProgramReads %s; declare the read so the findings cache does not replay a stale verdict",
			view.ruleName,
			method,
			programReadNames[read],
			view.reads,
		))
	}
}

func (view *programView) Options() *core.CompilerOptions {
	view.require(ReadsCompilerOptions, "Options")
	return view.program.Options()
}

func (view *programView) GetCurrentDirectory() string {
	view.require(ReadsCompilerOptions, "GetCurrentDirectory")
	return view.program.GetCurrentDirectory().AsString()
}

func (view *programView) UseCaseSensitiveFileNames() bool {
	view.require(ReadsCompilerOptions, "UseCaseSensitiveFileNames")
	return view.program.UseCaseSensitiveFileNames()
}

func (view *programView) IsSourceFileDefaultLibrary(path tspath.PathKey) bool {
	view.require(ReadsDefaultLibrary, "IsSourceFileDefaultLibrary")
	return view.program.IsSourceFileDefaultLibrary(path)
}

func (view *programView) DefaultLibraryPath() string {
	view.require(ReadsDefaultLibrary, "DefaultLibraryPath")
	return view.program.Host().DefaultLibraryPath().AsString()
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
		ResolvedFileName:        resolved.ResolvedFileName.AsString(),
		IsExternalLibraryImport: resolved.IsExternalLibraryImport,
		PackageName:             resolved.PackageId.Name,
	}
}

func (view *programView) GetSourceFileForResolvedModule(fileName string) *ast.SourceFile {
	view.require(ReadsOtherFiles, "GetSourceFileForResolvedModule")
	// Upstream's own takes the compiler's resolved module, which a rule never holds (it holds this
	// package's ResolvedModule). This is the lookup it made by name before typed file paths: the file,
	// else the file a project reference redirects the name to.
	name := tspath.RootedFilePath(fileName)
	if file := view.program.GetSourceFile(name); file != nil {
		return file
	}
	if redirect := view.program.GetParseFileRedirect(name); redirect != "" {
		return view.program.GetSourceFile(redirect)
	}
	return nil
}

func (view *programView) SourceFiles() []*ast.SourceFile {
	view.require(ReadsOtherFiles, "SourceFiles")
	return view.program.SourceFiles()
}

func (view *programView) GetSourceFile(fileName string) *ast.SourceFile {
	view.require(ReadsOtherFiles, "GetSourceFile")
	return view.program.GetSourceFile(tspath.RootedFilePath(fileName))
}

func (view *programView) FS() vfs.FS {
	view.require(ReadsOtherFiles, "FS")
	return view.program.Host().FS()
}

func (view *programView) DesignSystemFS() *RecordingFileSystem {
	view.require(ReadsDesignSystem, "DesignSystemFS")
	recorder := NewRecordingFileSystem(view.program.Host().FS())
	recordDesignSystemFileSystem(view.program, recorder)
	return recorder
}

func (view *programView) Identity() ProgramIdentity {
	view.live("Identity")
	return ProgramIdentity{program: view.program}
}
