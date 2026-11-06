package rule

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// fileNamed is a file a rule may ask about, by name.
type fileNamed string

func (name fileNamed) FileName() tspath.RootedFilePath { return tspath.RootedFilePath(name) }
func (name fileNamed) PathKey() tspath.PathKey         { return tspath.PathKey(name) }

// methodName is the name of the Program method a method expression names, as the compiler spells the
// function, so a table keyed by it is held to the interface and renamed with it.
func methodName[Method any](method Method) string {
	function := runtime.FuncForPC(reflect.ValueOf(method).Pointer()).Name()
	return function[strings.LastIndex(function, ".")+1:]
}

// refusalOf calls one method and returns the view's refusal, or "" when the view let the call through.
// A call the view admits reaches an empty program and may panic there, which is not a refusal.
func refusalOf(call func()) (refusal string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if message := fmt.Sprint(recovered); strings.Contains(message, "declares ProgramReads") {
				refusal = message
			}
		}
	}()
	call()
	return ""
}

// ctx.Program refuses every read a rule did not declare, by name, and admits every read it did
// (#b8k3bp6). Each method is tried under a declaration of nothing and under one of its own kind only,
// so a method checking the wrong kind, or none, fails here by name.
func TestAProgramViewAdmitsExactlyTheDeclaredReads(test *testing.T) {
	test.Parallel()
	methods := []struct {
		name string
		read ProgramRead
		call func(Program)
	}{
		{methodName(Program.Options), ReadsCompilerOptions, func(view Program) { view.Options() }},
		{
			methodName(Program.GetCurrentDirectory),
			ReadsCompilerOptions,
			func(view Program) { view.GetCurrentDirectory() },
		},
		{
			methodName(Program.UseCaseSensitiveFileNames),
			ReadsCompilerOptions,
			func(view Program) { view.UseCaseSensitiveFileNames() },
		},
		{
			methodName(Program.IsSourceFileDefaultLibrary),
			ReadsDefaultLibrary,
			func(view Program) { view.IsSourceFileDefaultLibrary("lib.d.ts") },
		},
		{methodName(Program.DefaultLibraryPath), ReadsDefaultLibrary, func(view Program) { view.DefaultLibraryPath() }},
		{
			methodName(Program.GetSourceFileForResolvedModule),
			ReadsOtherFiles,
			func(view Program) { view.GetSourceFileForResolvedModule("other.ts") },
		},
		{methodName(Program.SourceFiles), ReadsOtherFiles, func(view Program) { view.SourceFiles() }},
		{methodName(Program.GetSourceFile), ReadsOtherFiles, func(view Program) { view.GetSourceFile("other.ts") }},
		{methodName(Program.FileSystem), ReadsOtherFiles, func(view Program) { view.FileSystem() }},
		{
			methodName(Program.DesignSystemFileSystem),
			ReadsDesignSystem,
			func(view Program) { view.DesignSystemFileSystem() },
		},
	}
	program := new(compiler.Program)
	for _, method := range methods {
		test.Run(method.name, func(test *testing.T) {
			test.Parallel()
			refused := refusalOf(func() { method.call(ViewProgram(program, nil, Traits{Name: "probe"})) })
			if !strings.Contains(refused, "rule probe called Program."+method.name) ||
				!strings.Contains(refused, programReadNames[method.read]) {
				test.Errorf("an undeclared %s was not refused by name and kind: %q", method.name, refused)
			}
			for _, other := range []ProgramRead{
				ReadsCompilerOptions,
				ReadsDefaultLibrary,
				ReadsModuleResolution,
				ReadsOtherFiles,
				ReadsDesignSystem,
			} {
				refused := refusalOf(
					func() { method.call(ViewProgram(program, nil, Traits{Name: "probe", ProgramReads: other})) },
				)
				if admitted := refused == ""; admitted != (other == method.read) {
					test.Errorf(
						"%s under a declaration of %s: admitted %v, want %v",
						method.name,
						other,
						admitted,
						other == method.read,
					)
				}
			}
		})
	}
}

// Resolving the linted file's own imports is a module-resolution read; resolving another file's is a
// read of other files, which no per-file key covers.
func TestResolvingAnotherFilesImportsIsAReadOfOtherFiles(test *testing.T) {
	test.Parallel()
	program := new(compiler.Program)
	resolution := Traits{Name: "probe", ProgramReads: ReadsModuleResolution}
	refused := refusalOf(func() { ViewProgram(program, nil, resolution).ResolveModule(fileNamed("other.ts"), nil) })
	if !strings.Contains(refused, "ReadsOtherFiles") {
		test.Errorf("resolving another file's imports under ReadsModuleResolution alone was not refused: %q", refused)
	}
	both := Traits{Name: "probe", ProgramReads: ReadsModuleResolution | ReadsOtherFiles}
	if refused := refusalOf(
		func() { ViewProgram(program, nil, both).ResolveModule(fileNamed("other.ts"), nil) },
	); refused != "" {
		test.Errorf("resolving another file's imports was refused although ReadsOtherFiles is declared: %q", refused)
	}
}

// A program that could not be built is a nil Program, so the rules' `ctx.Program == nil` checks keep
// meaning what they meant.
func TestANilProgramViewsAsNil(test *testing.T) {
	test.Parallel()
	if ViewProgram(nil, nil, Traits{Name: "probe", ProgramReads: ReadsOtherFiles}) != nil {
		test.Error("a nil program gave a non-nil view")
	}
	var slot ProgramViewSlot
	if slot.Point(nil, nil, Traits{Name: "probe", ProgramReads: ReadsOtherFiles}) != nil {
		test.Error("a nil program gave a non-nil view from a slot")
	}
	slot.Release()
}

// endedOf calls one method and returns the view's "file ended" panic, or "" when the view let the call through.
func endedOf(call func()) (ended string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if message := fmt.Sprint(recovered); strings.Contains(message, "after the file it was given ended") {
				ended = message
			}
		}
	}()
	call()
	return ""
}

// A slot's view is good for its file only (#9xfg09f). Once released, every read through it panics by the
// rule's name, including the reads a rule declared, so a rule that kept its Program past the file fails
// loudly instead of reading through the file the slot was pointed at next. The view a rule kept from one
// file stays ended while the next file runs, because the slot alternates between two views.
func TestASlotViewKeptPastItsFileFailsOnEveryRead(test *testing.T) {
	test.Parallel()
	program := new(compiler.Program)
	everything := Traits{
		Name: "keeps-its-program",
		ProgramReads: ReadsCompilerOptions | ReadsDefaultLibrary | ReadsModuleResolution | ReadsOtherFiles |
			ReadsDesignSystem,
	}
	reads := []struct {
		name string
		call func(Program)
	}{
		{methodName(Program.Options), func(view Program) { view.Options() }},
		{methodName(Program.GetCurrentDirectory), func(view Program) { view.GetCurrentDirectory() }},
		{methodName(Program.UseCaseSensitiveFileNames), func(view Program) { view.UseCaseSensitiveFileNames() }},
		{
			methodName(Program.IsSourceFileDefaultLibrary),
			func(view Program) { view.IsSourceFileDefaultLibrary("lib.d.ts") },
		},
		{methodName(Program.DefaultLibraryPath), func(view Program) { view.DefaultLibraryPath() }},
		{
			methodName(Program.ResolveModule),
			func(view Program) { view.ResolveModule(fileNamed("other.ts"), nil) },
		},
		{
			methodName(Program.GetSourceFileForResolvedModule),
			func(view Program) { view.GetSourceFileForResolvedModule("other.ts") },
		},
		{methodName(Program.SourceFiles), func(view Program) { view.SourceFiles() }},
		{methodName(Program.GetSourceFile), func(view Program) { view.GetSourceFile("other.ts") }},
		{methodName(Program.FileSystem), func(view Program) { view.FileSystem() }},
		{methodName(Program.DesignSystemFileSystem), func(view Program) { view.DesignSystemFileSystem() }},
		{methodName(Program.Identity), func(view Program) { view.Identity() }},
	}

	var slot ProgramViewSlot
	first := slot.Point(program, nil, everything)
	if ended := endedOf(func() { first.Identity() }); ended != "" {
		test.Fatalf("a view refused a read during its own file: %s", ended)
	}
	slot.Release()
	second := slot.Point(program, nil, everything)

	for _, read := range reads {
		ended := endedOf(func() { read.call(first) })
		if ended == "" {
			test.Errorf("Program.%s read through a view whose file had ended", read.name)
			continue
		}
		if !strings.Contains(ended, everything.Name) || !strings.Contains(ended, read.name) {
			test.Errorf("Program.%s's refusal names neither the rule nor the read: %s", read.name, ended)
		}
	}
	if ended := endedOf(func() { second.Identity() }); ended != "" {
		test.Errorf("the next file's view was ended along with the last one: %s", ended)
	}
	slot.Release()
	if ended := endedOf(func() { second.Identity() }); ended == "" {
		test.Error("the second file's view still read after its own release")
	}
}
