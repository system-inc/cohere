// Package asm_decl is vet's asmdecl pass, assembly that disagrees with the Go declaration of the function it
// implements. A Go function declared without a body has its arguments and results laid out in a frame its assembly
// reads by name and offset, `x+8(FP)`, and the pass lays that frame out from the declaration's types for every
// architecture, then reads each assembly file of the package line by line against it: a `TEXT` line whose argument
// size is not the frame's, or that names a function with no declaration; a reference whose name is not an argument,
// whose offset is not its argument's, or whose instruction moves more or fewer bytes than the argument holds; a
// reference to the arguments through the stack pointer, or by offset alone; and a `RET` in a function that never
// wrote its result.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/asmdecl/asmdecl.go, whose logic
// it keeps, its table of architectures and its patterns among it, and whose messages it reports word for word, at
// the same position, the start of the line. Like the original it takes a file's architecture from its name, from a
// `// +build` line, or else from go/build's default context, the `GOARCH` the process runs under, so it judges the
// same files the same way on whatever target the load is for, and it requires no other analyzer. The two differ in
// one way no finding shows: where the original logs that it cannot tell a file's architecture, the port is silent,
// since a rule speaks only through its findings, and both then judge none of that file. The registry runs the port in
// the original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages on every architecture the pass judges. The two agree everywhere, so there is no differences fixture.
package asm_decl

import (
	"go/ast"
	"go/token"
	"os"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "asmdecl",
	Doc:  "report mismatches between assembly files and Go declarations",
	URL:  "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/asmdecl",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	// OtherFiles holds C headers and other sources beside the assembly, and only Go assembly is judged.
	assemblyFilenames := []string{}
	for _, filename := range pass.OtherFiles {
		if strings.HasSuffix(filename, ".s") {
			assemblyFilenames = append(assemblyFilenames, filename)
		}
	}
	if len(assemblyFilenames) == 0 {
		return nil, nil
	}

	// Every declaration without a body, by its name, in the order the files declare them, so a later one of the same
	// name, a method's, takes the place of an earlier one as in the original.
	frames := make(map[string]map[string]*argumentFrame)
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Body == nil {
				frames[function.Name.Name] = argumentFramesOf(pass, function)
			}
		}
	}

	for _, filename := range assemblyFilenames {
		content, file, readError := readFile(pass, filename)
		if readError != nil {
			// Unreachable from a load: every file the package lists is on disk and the driver allows reading it. Kept
			// as the original keeps it, ending the pass with the error.
			return nil, readError
		}
		newAssemblyFile(pass, file, filename, frames).judge(string(content))
	}
	return nil, nil
}

// readFile reads filename through the pass, falling back to the file system when the driver offers no reader, and
// adds it to the pass's file set so a finding can be placed in it. x/tools' analyzerutil.ReadFile.
func readFile(pass *analysis.Pass, filename string) ([]byte, *token.File, error) {
	reader := pass.ReadFile
	// The checker always offers a reader, so the fallback is kept only as the original's helper keeps it, for a
	// driver that offers none.
	if reader == nil {
		reader = os.ReadFile
	}
	content, readError := reader(filename)
	if readError != nil {
		return nil, nil, readError
	}
	file := pass.Fset.AddFile(filename, -1, len(content))
	file.SetLinesForContent(content)
	return content, file, nil
}
