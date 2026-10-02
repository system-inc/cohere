package rule

import (
	"fmt"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// fileNamed is a file a rule may ask about, by name.
type fileNamed string

func (name fileNamed) FileName() string  { return string(name) }
func (name fileNamed) Path() tspath.Path { return tspath.Path(name) }

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
func TestAProgramViewAdmitsExactlyTheDeclaredReads(t *testing.T) {
	methods := []struct {
		name string
		read ProgramRead
		call func(Program)
	}{
		{"Options", ReadsCompilerOptions, func(view Program) { view.Options() }},
		{"GetCurrentDirectory", ReadsCompilerOptions, func(view Program) { view.GetCurrentDirectory() }},
		{"UseCaseSensitiveFileNames", ReadsCompilerOptions, func(view Program) { view.UseCaseSensitiveFileNames() }},
		{"IsSourceFileDefaultLibrary", ReadsDefaultLibrary, func(view Program) { view.IsSourceFileDefaultLibrary("lib.d.ts") }},
		{"DefaultLibraryPath", ReadsDefaultLibrary, func(view Program) { view.DefaultLibraryPath() }},
		{"GetSourceFileForResolvedModule", ReadsOtherFiles, func(view Program) { view.GetSourceFileForResolvedModule("other.ts") }},
		{"SourceFiles", ReadsOtherFiles, func(view Program) { view.SourceFiles() }},
		{"GetSourceFile", ReadsOtherFiles, func(view Program) { view.GetSourceFile("other.ts") }},
		{"FS", ReadsOtherFiles, func(view Program) { view.FS() }},
		{"DesignSystemFS", ReadsDesignSystem, func(view Program) { view.DesignSystemFS() }},
	}
	program := new(compiler.Program)
	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			refused := refusalOf(func() { method.call(ViewProgram(program, nil, Rule{Name: "probe"})) })
			if !strings.Contains(refused, "rule probe called Program."+method.name) || !strings.Contains(refused, programReadNames[method.read]) {
				t.Errorf("an undeclared %s was not refused by name and kind: %q", method.name, refused)
			}
			for _, other := range []ProgramRead{ReadsCompilerOptions, ReadsDefaultLibrary, ReadsModuleResolution, ReadsOtherFiles, ReadsDesignSystem} {
				refused := refusalOf(func() { method.call(ViewProgram(program, nil, Rule{Name: "probe", ProgramReads: other})) })
				if admitted := refused == ""; admitted != (other == method.read) {
					t.Errorf("%s under a declaration of %s: admitted %v, want %v", method.name, other, admitted, other == method.read)
				}
			}
		})
	}
}

// Resolving the linted file's own imports is a module-resolution read; resolving another file's is a
// read of other files, which no per-file key covers.
func TestResolvingAnotherFilesImportsIsAReadOfOtherFiles(t *testing.T) {
	program := new(compiler.Program)
	resolution := Rule{Name: "probe", ProgramReads: ReadsModuleResolution}
	refused := refusalOf(func() { ViewProgram(program, nil, resolution).ResolveModule(fileNamed("other.ts"), nil) })
	if !strings.Contains(refused, "ReadsOtherFiles") {
		t.Errorf("resolving another file's imports under ReadsModuleResolution alone was not refused: %q", refused)
	}
	both := Rule{Name: "probe", ProgramReads: ReadsModuleResolution | ReadsOtherFiles}
	if refused := refusalOf(func() { ViewProgram(program, nil, both).ResolveModule(fileNamed("other.ts"), nil) }); refused != "" {
		t.Errorf("resolving another file's imports was refused although ReadsOtherFiles is declared: %q", refused)
	}
}

// A program that could not be built is a nil Program, so the rules' `ctx.Program == nil` checks keep
// meaning what they meant.
func TestANilProgramViewsAsNil(t *testing.T) {
	if ViewProgram(nil, nil, Rule{Name: "probe", ProgramReads: ReadsOtherFiles}) != nil {
		t.Error("a nil program gave a non-nil view")
	}
	for _, view := range ViewProgramForEach(nil, nil, []Rule{{Name: "a"}, {Name: "b"}}) {
		if view != nil {
			t.Error("a nil program gave a non-nil view in a batch")
		}
	}
}
