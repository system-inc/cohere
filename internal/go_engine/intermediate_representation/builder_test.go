// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation_test

// These are go/ir's builder tests that check lowering without lifting or printing, adapted to our names.
// go/ir's tests load through go/packages and its irutil package; ours type-check in memory and create the
// packages as irutil.BuildPackage does, with buildPackage below. Where a test read an instruction's String,
// which ours doesn't carry, it reads the same fact off the instruction instead, and each such change is said
// where it is made.

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// TestMain points go/build at the toolchain's `GOROOT`, as internal/go_rename/rename's tests do: under
// `GOFLAGS=-trimpath` the test binary has none baked in, and importer.Default finds the standard library
// through go/build.
func TestMain(main *testing.M) {
	output, goEnvironmentError := exec.Command("go", "env", "GOROOT").Output()
	if goEnvironmentError != nil {
		panic(goEnvironmentError)
	}
	build.Default.GOROOT = strings.TrimSpace(string(output))
	os.Exit(main.Run())
}

// buildPackage type-checks the single file input, creates every package it imports from type information,
// then creates and builds its own from syntax, as go/ir's irutil.BuildPackage does. It returns the built
// package and the parsed file.
func buildPackage(
	t testing.TB,
	input string,
	importer types.Importer,
) (*intermediate_representation.Package, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, parseError := parser.ParseFile(fset, "input.go", input, parser.ParseComments)
	if parseError != nil {
		t.Fatal(parseError)
	}
	pkg, buildError := buildFiles(fset, file.Name.Name, []*ast.File{file}, importer)
	if buildError != nil {
		t.Fatal(buildError)
	}
	return pkg, file
}

// buildFiles is irutil.BuildPackage: type-check files as the package at path, create its imports
// transitively from type information, and create and build it from syntax.
func buildFiles(
	fset *token.FileSet,
	path string,
	files []*ast.File,
	importer types.Importer,
) (*intermediate_representation.Package, error) {
	info := newInfo()
	pkg := types.NewPackage(path, "")
	if checkError := types.NewChecker(&types.Config{Importer: importer}, fset, pkg, info).Files(
		files,
	); checkError != nil {
		return nil, checkError
	}
	prog := intermediate_representation.NewProgram(fset)
	created := make(map[*types.Package]bool)
	var createAll func(pkgs []*types.Package)
	createAll = func(pkgs []*types.Package) {
		for _, p := range pkgs {
			if !created[p] {
				created[p] = true
				prog.CreatePackage(p, nil, nil, true)
				createAll(p.Imports())
			}
		}
	}
	createAll(pkg.Imports())
	irpkg := prog.CreatePackage(pkg, files, info, false)
	irpkg.Build()
	return irpkg, nil
}

func newInfo() *types.Info {
	return &types.Info{
		Types:        make(map[ast.Expr]types.TypeAndValue),
		Defs:         make(map[*ast.Ident]types.Object),
		Uses:         make(map[*ast.Ident]types.Object),
		Implicits:    make(map[ast.Node]types.Object),
		Scopes:       make(map[ast.Node]*types.Scope),
		Selections:   make(map[*ast.SelectorExpr]*types.Selection),
		Instances:    make(map[*ast.Ident]types.Instance),
		FileVersions: make(map[*ast.File]string),
	}
}

// sourcePackages type-checks each package of sources, keyed by import path, in the order given, each
// importing those before it. It stands in for go/ir's go/packages loads of a module.
type sourcePackages struct {
	fset     *token.FileSet
	packages map[string]*types.Package
	files    map[string][]*ast.File
	infos    map[string]*types.Info
}

func checkSources(t testing.TB, order []string, sources map[string]string) *sourcePackages {
	t.Helper()
	loaded := &sourcePackages{fset: token.NewFileSet(), packages: map[string]*types.Package{},
		files: map[string][]*ast.File{}, infos: map[string]*types.Info{}}
	for _, path := range order {
		file, parseError := parser.ParseFile(loaded.fset, path+"/source.go", sources[path], 0)
		if parseError != nil {
			t.Fatal(parseError)
		}
		info := newInfo()
		configuration := &types.Config{Importer: importerFunc(func(imported string) (*types.Package, error) {
			if pkg, ok := loaded.packages[imported]; ok {
				return pkg, nil
			}
			return nil, fmt.Errorf("no package %q among the sources", imported)
		})}
		pkg, checkError := configuration.Check(path, loaded.fset, []*ast.File{file}, info)
		if checkError != nil {
			t.Fatal(checkError)
		}
		loaded.packages[path] = pkg
		loaded.files[path] = []*ast.File{file}
		loaded.infos[path] = info
	}
	return loaded
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

func isEmpty(f *intermediate_representation.Function) bool { return f.Blocks == nil }

// callsOf are fn's calls in block order, but for calls to builtins. Unlifted, every function opens with a
// call to the ssa:deferstack builtin, which lifting removes when nothing defers (#ee33njz); go/ir's tests
// read the calls after lifting, so the ones they count are these.
func callsOf(fn *intermediate_representation.Function) []*intermediate_representation.CallCommon {
	var calls []*intermediate_representation.CallCommon
	for _, b := range fn.Blocks {
		for _, instr := range b.Instructions {
			if call, ok := instr.(intermediate_representation.CallInstruction); ok {
				if _, isBuiltin := call.Common().Value.(*intermediate_representation.Builtin); !isBuiltin {
					calls = append(calls, call.Common())
				}
			}
		}
	}
	return calls
}

// Tests that programs partially loaded from gc object files contain
// functions with no code for the external portions, but are otherwise ok.
//
// go/ir's test also checks the method sets of the external types through Program.MethodValue, which ours
// doesn't carry, so that half is left out.
func TestBuildPackage(t *testing.T) {
	input := `
package main

import (
	"bytes"
	"io"
	"testing"
)

func main() {
	var t testing.T
	t.Parallel()    // static call to external declared method
	t.Fail()        // static call to promoted external declared method
	testing.Short() // static call to external package-level function

	var w io.Writer = new(bytes.Buffer)
	w.Write(nil)    // interface invoke of external declared method
}
`
	// Build an IR program from the parsed file.
	// Load its dependencies from gc binary export data.
	mainPackage, _ := buildPackage(t, input, importer.Default())

	// The main package, its direct and indirect dependencies are loaded.
	deps := []string{
		// directly imported dependencies:
		"bytes", "io", "testing",
		// indirect dependencies mentioned by
		// the direct imports' export data
		"sync", "unicode", "time",
	}

	prog := mainPackage.Program
	all := prog.AllPackages()
	if len(all) <= len(deps) {
		// go/ir prints the packages with %q through Package.String, which ours doesn't carry.
		var paths []string
		for _, pkg := range all {
			paths = append(paths, pkg.Types.Path())
		}
		t.Errorf("unexpected set of loaded packages: %q", paths)
	}
	for _, path := range deps {
		pkg := prog.ImportedPackage(path)
		if pkg == nil {
			t.Errorf("package not loaded: %q", path)
			continue
		}

		// External packages should have no function bodies (except for wrappers).
		isExt := pkg != mainPackage

		// init()
		if isExt && !isEmpty(pkg.Function("init")) {
			t.Errorf("external package %s has non-empty init", pkg.Types.Path())
		}

		for _, mem := range pkg.Members {
			if mem, ok := mem.(*intermediate_representation.Function); ok {
				// Functions at package level.
				if isExt && !isEmpty(mem) {
					t.Errorf("external function %s is non-empty", mem)
				}
			}
		}
	}
	if isEmpty(mainPackage.Function("init")) {
		t.Errorf("main package has empty init")
	}
	for _, mem := range mainPackage.Members {
		switch mem := mem.(type) {
		case *intermediate_representation.Function:
			if isEmpty(mem) {
				t.Errorf("function %s is empty", mem)
			}
		case *intermediate_representation.Type:
			t.Fatalf("unexpected name type in main package: %s", mem)
		}
	}

	expectedCallee := []string{
		"(*testing.T).Parallel",
		"(*testing.common).Fail",
		"testing.Short",
		"N/A",
	}
	callNumber := 0
	for _, call := range callsOf(mainPackage.Function("main")) {
		if want := expectedCallee[callNumber]; want != "N/A" {
			got := call.StaticCallee().String()
			if want != got {
				t.Errorf("call #%d from main.main: got callee %s, want %s",
					callNumber, got, want)
			}
		}
		callNumber++
	}
	if callNumber != 4 {
		t.Errorf("in main.main: got %d calls, want %d", callNumber, 4)
	}
}

// Tests that methods from indirect dependencies not subject to
// CreatePackage are created as needed.
func TestNoIndirectCreatePackage(t *testing.T) {
	loaded := checkSources(t, []string{"testdata/c", "testdata/b", "testdata/a"}, map[string]string{
		"testdata/a": `package a

import "testdata/b"

func A() {
	var x b.B
	x.F()
}
`,
		"testdata/b": `package b

import "testdata/c"

type B struct { c.C }
`,
		"testdata/c": `package c

type C int
func (C) F() {}
`,
	})
	a := loaded.packages["testdata/a"]

	// Create a from syntax, its direct deps b from types, but not indirect deps c.
	prog := intermediate_representation.NewProgram(loaded.fset)
	aSsa := prog.CreatePackage(a, loaded.files["testdata/a"], loaded.infos["testdata/a"], false)
	for _, p := range a.Imports() {
		prog.CreatePackage(p, nil, nil, true)
	}

	// Build SSA for package a.
	aSsa.Build()

	// Find the function in the sole call in the sole block of function a.A. Unlifted, the block opens with
	// the call to the ssa:deferstack builtin that every function makes and lifting removes when nothing
	// defers (#ee33njz), so the call sought is the sole one to a function.
	var got string
	for _, instr := range aSsa.Members["A"].(*intermediate_representation.Function).Blocks[0].Instructions {
		if call, ok := instr.(*intermediate_representation.Call); ok {
			f, isFunction := call.Call.Value.(*intermediate_representation.Function)
			if !isFunction {
				continue
			}
			got = fmt.Sprintf("%v # %s", f, f.Synthetic)
			break
		}
	}
	want := "(testdata/c.C).F # from type information (on demand)"
	if got != want {
		t.Errorf("for sole call in a.A, got: <<%s>>, want <<%s>>", got, want)
	}
}

// TestOrderOfOperations reads each call's callee where go/ir's test read the call's printed form, a()
// and b(), so the sequence it wants is the same.
func TestOrderOfOperations(t *testing.T) {
	// Testing for the order of operations within an expression is done
	// by collecting the sequence of direct function calls within a *Function.
	// Callees are all external functions so they cannot be safely re-ordered by ssa.
	const input = `
package p

func a() int
func b() int
func c() int

func slice(s []int) []int { return s[a():b()] }
func sliceMax(s []int) []int { return s[a():b():c()] }

`

	p, _ := buildPackage(t, input, nil)

	for _, item := range []struct {
		fn   string
		want string // sequence of calls within the function.
	}{
		{"sliceMax", "[a() b() c()]"},
		{"slice", "[a() b()]"},
	} {
		fn := p.Function(item.fn)
		want := item.want
		t.Run(item.fn, func(t *testing.T) {
			t.Parallel()

			var calls []string
			for _, call := range callsOf(fn) {
				calls = append(calls, call.StaticCallee().Name()+"()")
			}
			if got := fmt.Sprint(calls); got != want {
				t.Errorf("Expected sequence of function calls in %s was %s. got %s", fn, want, got)
			}
		})
	}
}

// TestGenericFunctionSelector ensures generic functions from other packages can be selected. go/ir's test
// runs it once more with InstantiateGenerics, which ours doesn't have; every callee is an instantiation
// wrapper, as in its first run.
func TestGenericFunctionSelector(t *testing.T) {
	loaded := checkSources(t, []string{"example.com/a", "example.com"}, map[string]string{
		"example.com":   `package main; import "example.com/a"; func main() { a.F[int](); a.G[int,string](); a.H(0) }`,
		"example.com/a": `package a; func F[T any](){}; func G[S, T any](){}; func H[T any](a T){} `,
	})

	prog := intermediate_representation.NewProgram(loaded.fset)
	for _, path := range []string{"example.com/a", "example.com"} {
		prog.CreatePackage(loaded.packages[path], loaded.files[path], loaded.infos[path], true)
	}
	p := prog.Package(loaded.packages["example.com"])
	p.Build()

	if p.Types.Name() != "main" {
		t.Fatalf("Expected the second package is main but got %s", p.Types.Name())
	}

	var callees []string // callees of the CallInstruction in main().
	for _, call := range callsOf(p.Function("main")) {
		if callee := call.StaticCallee(); callee != nil {
			callees = append(callees, callee.String())
		} else {
			t.Errorf("CallInstruction without StaticCallee() in %s", p.Function("main"))
		}
	}
	sort.Strings(callees) // ignore the order in the code.

	want := "[example.com/a.F[int] example.com/a.G[int, string] example.com/a.H[int]]"
	if got := fmt.Sprint(callees); got != want {
		t.Errorf("Expected main() to contain calls %v. got %v", want, got)
	}
}

func TestGo117Builtins(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		importer types.Importer
	}{
		{"slice to array pointer", "package p; var s []byte; var _ = (*[4]byte)(s)", nil},
		{"unsafe slice", `package p; import "unsafe"; var _ = unsafe.Add(nil, 0)`, importer.Default()},
		{"unsafe add", `package p; import "unsafe"; var _ = unsafe.Slice((*int)(nil), 0)`, importer.Default()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buildPackage(t, tc.src, tc.importer)
		})
	}
}

// TestLabels just tests that anonymous labels are handled.
func TestLabels(t *testing.T) {
	tests := []string{
		`package main
		  func main() { _:println(1) }`,
		`package main
		  func main() { _:println(1); _:println(2)}`,
	}
	for _, test := range tests {
		buildPackage(t, test, nil)
	}
}

// TestFixedBugs builds go/ir's fixed-bug programs. go/ir builds them with InstantiateGenerics and its sanity
// checker; ours builds the generic bodies in generic form, and the test holds that building them doesn't
// panic.
func TestFixedBugs(t *testing.T) {
	for _, name := range []string{
		"issue66783a",
		"issue66783b",
		"issue73594",
		"issue78110",
		"issue73871",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", "fixedbugs", name+".go")
			fset := token.NewFileSet()
			f, parseFileError := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if parseFileError != nil {
				t.Fatal(parseFileError)
			}
			if _, buildFilesError := buildFiles(fset, name, []*ast.File{f}, nil); buildFilesError != nil {
				t.Error(buildFilesError)
			}
		})
	}
}

func TestBuildPackageGo120(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		importer types.Importer
	}{
		{"slice to array", "package p; var s []byte; var _ = ([4]byte)(s)", nil},
		{"slice to zero length array", "package p; var s []byte; var _ = ([0]byte)(s)", nil},
		{
			"slice to zero length array type parameter",
			"package p; var s []byte; func f[T ~[0]byte]() { tmp := (T)(s); var z T; _ = tmp == z}",
			nil,
		},
		{
			"slice to non-zero length array type parameter",
			"package p; var s []byte; func h[T ~[1]byte | [4]byte]() { tmp := T(s); var z T; _ = tmp == z}",
			nil,
		},
		{
			"slice to maybe-zero length array type parameter",
			"package p; var s []byte; func g[T ~[0]byte | [4]byte]() { tmp := T(s); var z T; _ = tmp == z}",
			nil,
		},
		{
			"rune sequence to sequence cast patterns", `
			package p
			// Each of fXX functions describes a 1.20 legal cast between sequences of runes
			// as []rune, pointers to rune arrays, rune arrays, or strings.
			//
			// Comments listed given the current emitted instructions [approximately].
			// If multiple conversions are needed, these are separated by |.
			// rune was selected as it leads to string casts (byte is similar).
			// The length 2 is not significant.
			// Multiple array lengths may occur in a cast in practice (including 0).
			func f00[S string, D string](s S)                               { _ = D(s) } // ChangeType
			func f01[S string, D []rune](s S)                               { _ = D(s) } // Convert
			func f02[S string, D []rune | string](s S)                      { _ = D(s) } // ChangeType | Convert
			func f03[S [2]rune, D [2]rune](s S)                             { _ = D(s) } // ChangeType
			func f04[S *[2]rune, D *[2]rune](s S)                           { _ = D(s) } // ChangeType
			func f05[S []rune, D string](s S)                               { _ = D(s) } // Convert
			func f06[S []rune, D [2]rune](s S)                              { _ = D(s) } // SliceToArray
			func f07[S []rune, D [2]rune | string](s S)                     { _ = D(s) } // SliceToArray | Convert
			func f08[S []rune, D *[2]rune](s S)                             { _ = D(s) } // SliceToArrayPointer
			func f09[S []rune, D *[2]rune | string](s S)                    { _ = D(s) } // SliceToArray | Convert
			func f10[S []rune, D *[2]rune | [2]rune](s S)                   { _ = D(s) } // SliceToArrayPointer | SliceToArray
			func f11[S []rune, D *[2]rune | [2]rune | string](s S)          { _ = D(s) } // SliceToArrayPointer | SliceToArray | Convert
			func f12[S []rune, D []rune](s S)                               { _ = D(s) } // ChangeType
			func f13[S []rune, D []rune | string](s S)                      { _ = D(s) } // Convert | ChangeType
			func f14[S []rune, D []rune | [2]rune](s S)                     { _ = D(s) } // ChangeType | SliceToArray
			func f15[S []rune, D []rune | [2]rune | string](s S)            { _ = D(s) } // ChangeType | SliceToArray | Convert
			func f16[S []rune, D []rune | *[2]rune](s S)                    { _ = D(s) } // ChangeType | SliceToArrayPointer
			func f17[S []rune, D []rune | *[2]rune | string](s S)           { _ = D(s) } // ChangeType | SliceToArrayPointer | Convert
			func f18[S []rune, D []rune | *[2]rune | [2]rune](s S)          { _ = D(s) } // ChangeType | SliceToArrayPointer | SliceToArray
			func f19[S []rune, D []rune | *[2]rune | [2]rune | string](s S) { _ = D(s) } // ChangeType | SliceToArrayPointer | SliceToArray | Convert
			func f20[S []rune | string, D string](s S)                      { _ = D(s) } // Convert | ChangeType
			func f21[S []rune | string, D []rune](s S)                      { _ = D(s) } // Convert | ChangeType
			func f22[S []rune | string, D []rune | string](s S)             { _ = D(s) } // ChangeType | Convert | Convert | ChangeType
			func f23[S []rune | [2]rune, D [2]rune](s S)                    { _ = D(s) } // SliceToArray | ChangeType
			func f24[S []rune | *[2]rune, D *[2]rune](s S)                  { _ = D(s) } // SliceToArrayPointer | ChangeType
			`, nil,
		},
		{
			"matching named and underlying types", `
			package p
			type a string
			type b string
			func g0[S []rune | a | b, D []rune | a | b](s S)      { _ = D(s) }
			func g1[S []rune | ~string, D []rune | a | b](s S)    { _ = D(s) }
			func g2[S []rune | a | b, D []rune | ~string](s S)    { _ = D(s) }
			func g3[S []rune | ~string, D []rune |~string](s S)   { _ = D(s) }
			`, nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buildPackage(t, tc.src, tc.importer)
		})
	}
}

// TestRangeIndexLoop holds the one field that replaces a block label readers compared: a range over a
// slice marks its loop header RangeIndexLoop, where go/ir writes the label "rangeindex.loop", and no other
// block is marked. This one is ours; go/ir has no test of the label.
func TestRangeIndexLoop(t *testing.T) {
	const input = `
package p

func f(s []int, a [3]int, m map[int]int) (n int) {
	for i := range s {
		n += i
	}
	for _, v := range a {
		n += v
	}
	for k := range m {
		n += k
	}
	for i := 0; i < n; i++ {
		n--
	}
	return n
}
`
	p, _ := buildPackage(t, input, nil)
	var marked []string
	for _, b := range p.Function("f").Blocks {
		if b.RangeIndexLoop {
			// Lifted, the header merges the index and n in two phis, increments the index and branches on it
			// against the length.
			var kinds []string
			for _, instr := range b.Instructions {
				kinds = append(kinds, strings.TrimPrefix(fmt.Sprintf("%T", instr), "*intermediate_representation."))
			}
			marked = append(marked, strings.Join(kinds, " "))
		}
	}
	want := []string{
		"Phi Phi BinaryOperation BinaryOperation If",
		"Phi Phi BinaryOperation BinaryOperation If",
	}
	if fmt.Sprint(marked) != fmt.Sprint(want) {
		t.Errorf("the RangeIndexLoop blocks hold %q, want %q", marked, want)
	}
}
