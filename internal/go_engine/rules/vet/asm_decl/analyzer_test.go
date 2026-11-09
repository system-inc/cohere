package asm_decl

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/asmdecl"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamTargets are the targets x/tools' test runs its fixtures on, each a `GOOS/GOARCH`, and three more. x/tools
// runs linux/mips although no fixture of its loads there. It leaves out linux/loong64, since its test sets only the
// environment, which go/build's default context has read before the test starts, so asm10.s, which names its
// architecture in a `//go:build` line the pass does not read, would be judged as the machine running the test;
// onTarget sets the default context too, so asm10.s is judged as loong64. And no target of x/tools' loads asm8.s,
// mipsle's, so linux/mipsle is added, and linux/ppc64le, the second architecture asm7.s's build line names.
var upstreamTargets = []string{
	"linux/amd64",
	"linux/386",
	"linux/arm",
	"linux/mips64",
	"linux/s390x",
	"linux/ppc64",
	"linux/mips",
	"js/wasm",
	"linux/riscv64",
	"linux/mipsle",
	"linux/ppc64le",
	"linux/loong64",
}

// architectureTargets are a target for each architecture the pass judges, in its table's order.
var architectureTargets = []string{
	"linux/386",
	"linux/arm",
	"linux/arm64",
	"linux/amd64",
	"linux/mips",
	"linux/mipsle",
	"linux/mips64",
	"linux/mips64le",
	"linux/ppc64",
	"linux/ppc64le",
	"linux/riscv64",
	"linux/s390x",
	"js/wasm",
	"linux/loong64",
}

// fixturePackages are the packages the differential runs over: x/tools' and parity's.
var fixturePackages = []string{"a", "parity", "parity/nested"}

// x/tools' own fixtures for asmdecl, copied unchanged into testdata/src and run as x/tools' test runs them, with no
// flags, on each target its test runs (see upstreamTargets): every `// want` the original expects, the port reports
// at the same line with the same message, and analysistest fails on a finding no want expects as much as on a want
// nothing found, so the port adds nothing there either.
//
// Not parallel: each target is set in go/build's default context and the environment, which are the process's own.
func TestPortMatchesUpstreamWants(test *testing.T) {
	for _, target := range upstreamTargets {
		// Not parallel: onTarget sets the target in go/build's default context and the environment, the process's own.
		test.Run(target, func(subtest *testing.T) {
			onTarget(subtest, target)
			parity.RequireUpstreamWants(subtest, Analyzer, analysistest.TestData(), "a")
		})
	}
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and an assembly file for each
// architecture, the original and the port find the same findings, each the same in every field, on a target of every
// architecture the pass judges, each target a subtest of its own, named for it.
//
// Not parallel: each target is set in go/build's default context and the environment, which are the process's own.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	for _, target := range architectureTargets {
		// Not parallel: onTarget sets the target in go/build's default context and the environment, the process's own.
		test.Run(target, func(subtest *testing.T) {
			onTarget(subtest, target)
			parity.RequireIdentical(subtest, asmdecl.Analyzer, Analyzer, analysistest.TestData(), fixturePackages...)
		})
	}
}

// unknownArchitectureFiles are the fixture files whose architecture the pass cannot tell when the default context
// names one it does not know: each names none of its own, in its name or in a build line the pass reads, and each holds
// findings on every target that judges it.
var unknownArchitectureFiles = []string{
	"parity/parity.s",
	"parity/parity_terms.s",
	"parity/parity_go_build.s",
	"parity/nested/nested.s",
}

// When go/build's default context names an architecture the pass does not know, a file whose name and build lines
// name none cannot be judged, and both leave it whole, the original after logging that it cannot tell the file's
// architecture, while both still judge the files that name theirs, each finding the same in every field. The load is
// for linux/amd64 and the default context says sparc64, which no Go release builds for, so the load hands over
// parity_go_build.s, built for amd64 by a `//go:build` line the pass does not read, beside the files that name no
// architecture at all (see unknownArchitectureFiles).
//
// Leaving those files alone proves only that nothing was found there, so the tier pins why beside it: the same load
// judged as amd64, the default context naming the load's own target, where both sides find the same in every field and
// each of those files holds findings of each side's, as parity_go_build.s holds `[amd64] quads: wrong argument size
// 28; expected $...-32` at its `TEXT` line and `invalid MOVW of x+0(FP); int64 is 8-byte value` at line 8. So the
// sparc64 half is green because the architecture is unknown, and a sparc64 half that went on judging as amd64 fails.
//
// Not parallel: the differences and shape tiers run asmdecl by hand, outside the harness, where it reads go/build's
// default context, which this call sets.
func TestNeitherJudgesAFileOfAnUnknownArchitecture(test *testing.T) {
	onAmd64 := parity.On("GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	// Not parallel: the subtests are this tier's two halves, each a harness call that sets the default context, run
	// one after the other for the reason the tier itself is not parallel.
	test.Run("judged as amd64", func(subtest *testing.T) {
		onAmd64.RequireIdentical(subtest, asmdecl.Analyzer, Analyzer, analysistest.TestData(), fixturePackages...)
		original, port := onAmd64.Findings(subtest, asmdecl.Analyzer, Analyzer, analysistest.TestData(),
			fixturePackages...)
		for _, file := range unknownArchitectureFiles {
			isIn := func(found parity.Finding) bool { return found.File == file }
			if !slices.ContainsFunc(original, isIn) || !slices.ContainsFunc(port, isIn) {
				subtest.Errorf("judged as amd64, %s holds no finding of one side's, so leaving it alone where the "+
					"architecture is unknown says nothing of the architecture", file)
			}
		}
	})
	// Not parallel: it sets the default context, after the amd64 half has run.
	test.Run("judged as sparc64", func(subtest *testing.T) {
		onAmd64.JudgingBy("GOARCH=sparc64").RequireIdenticalLeavingAlone(subtest, asmdecl.Analyzer, Analyzer,
			analysistest.TestData(), unknownArchitectureFiles, fixturePackages...)
	})
}

// differencesDirectory is the differences fixture, whose declarations crash the original, and whose assembly file the
// shape tier hands the original beside each shape.
var differencesDirectory = filepath.Join(analysistest.TestData(), "src", "differences")

// The original lays out every bodiless declaration's frame before it reads a line of assembly, and crashes in
// go/types' sizes on one holding a type parameter by value, which go/types type-checks (the assertion at
// go/types/gcsizes.go:54 fails, and the panic recovered names the result check at line 17 that runs after it): a
// method of a generic type taking a T, a [2]T or a struct of T, or returning a T. The port judges nothing of such a
// function, since without its frame nothing true can be said of its assembly, and goes on judging the rest of the
// package. Loaded for linux/amd64, the original must panic on the differences fixture, and the port must report there
// exactly the `port finds:` records of differences_amd64.s, each reasoned on the line above it: none in the functions
// it cannot measure, and every finding of the measured ones beside and after them.
//
// Not parallel: the shape tier runs the original by hand, outside the harness, where it reads go/build's default
// context, which this call sets.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	parity.On("GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0").RequirePortWhereOriginalPanics(test, asmdecl.Analyzer,
		Analyzer, analysistest.TestData(), "differences")
}

// Each shape the port leaves unjudged crashes the original on its own, and each shape that holds a type parameter
// only behind a word does not, so the port's skip is exactly as wide as the original's crash.
func TestPortLeavesUnjudgedExactlyTheFramesTheOriginalCrashesOn(test *testing.T) {
	test.Parallel()
	cases := []struct {
		declaration string
		crashes     bool
	}{
		{"func (box[T]) value(x T)", true},
		{"func (box[T]) array(x [2]T)", true},
		{"func (box[T]) empty(x [0]T)", true},
		{"func (box[T]) wrapped(x pair[T])", true},
		{"func (box[T]) inline(x struct{ first T })", true},
		{"func (box[T]) result() T", true},
		{"func (box[T]) variadic(x ...T)", false},
		{"func (box[T]) pointer(x *T)", false},
		{"func (box[T]) slice(x []T)", false},
		{"func (box[T]) mapped(x map[string]T)", false},
		{"func (box[T]) function(x func(T) T)", false},
		{"func (box[T]) channel(x chan T)", false},
		{"func (box[T]) arrayOfPointers(x [2]*T)", false},
	}
	assemblyFilename := filepath.Join(differencesDirectory, "differences_amd64.s")
	for _, shape := range cases {
		test.Run(shape.declaration, func(subtest *testing.T) {
			subtest.Parallel()
			source := "package shape\n\ntype box[T any] struct{}\n\ntype pair[T any] struct{ first T }\n\n" +
				shape.declaration + "\n"
			checked := checkedByHand(subtest, "shape.go", source)
			crashed := originalCrashes(subtest, checked, assemblyFilename)
			if (crashed != nil) != shape.crashes {
				subtest.Fatalf("the original crashing is %v, not the %v the case is written for: %v",
					crashed != nil, shape.crashes, crashed)
			}
			declaration, isFunction := checked.file.Decls[len(checked.file.Decls)-1].(*ast.FuncDecl)
			if !isFunction {
				subtest.Fatal("the shape's last declaration is not its function")
			}
			pass := &analysis.Pass{TypesInfo: checked.information}
			if frameIsMeasurable(pass, declaration) == shape.crashes {
				subtest.Errorf("the port measuring the frame is %v, where the original crashing is %v",
					!shape.crashes, shape.crashes)
			}
		})
	}
}

// originalCrashes runs x/tools' asmdecl by hand on a pass over the Go file checked holds and the assembly file named
// assemblyFilename, and returns what it panicked with, or nil when it did not. What it reports is not read: a shape is
// a frame, and the question is whether laying it out crashes.
func originalCrashes(test *testing.T, checked handChecked, assemblyFilename string) (recovered any) {
	test.Helper()
	defer func() {
		recovered = recover()
	}()
	pass := &analysis.Pass{
		Analyzer:   asmdecl.Analyzer,
		Fset:       checked.fileSet,
		Files:      []*ast.File{checked.file},
		OtherFiles: []string{assemblyFilename},
		Pkg:        checked.typesPackage,
		TypesInfo:  checked.information,
		TypesSizes: types.SizesFor("gc", "amd64"),
		ResultOf:   map[*analysis.Analyzer]any{},
		ReadFile:   os.ReadFile,
		Report: func(analysis.Diagnostic) {
			// What the original reports is not read: the question is whether laying out the frame crashes.
		},
	}
	if _, runError := asmdecl.Analyzer.Run(pass); runError != nil {
		test.Fatalf("running %s: %v", asmdecl.Analyzer.Name, runError)
	}
	return nil
}

// handChecked is one Go file type-checked by hand, as a package of its own.
type handChecked struct {
	fileSet      *token.FileSet
	file         *ast.File
	typesPackage *types.Package
	information  *types.Info
}

// checkedByHand parses source as the file named filename, or reads the file when source is empty, and type-checks
// it as a package at the path its package clause names.
func checkedByHand(test *testing.T, filename string, source string) handChecked {
	test.Helper()
	fileSet := token.NewFileSet()
	content := []byte(source)
	if source == "" {
		read, readError := os.ReadFile(filename)
		if readError != nil {
			test.Fatal(readError)
		}
		content = read
	}
	file, parseError := parser.ParseFile(fileSet, filename, content, parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	information := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
	}
	configuration := types.Config{}
	checked, checkError := configuration.Check(file.Name.Name, fileSet, []*ast.File{file}, information)
	if checkError != nil {
		test.Fatal(checkError)
	}
	return handChecked{fileSet: fileSet, file: file, typesPackage: checked, information: information}
}

// onTarget makes test run as on a machine building for target, `GOOS/GOARCH`: go/build's default context, which
// the original and the port read the architecture from, and the environment, which the load runs go list under, so
// a package holds the files it holds there. Both are put back when test ends. Setenv also refuses a parallel test, and
// none may run beside this one, since the default context is the process's own.
func onTarget(test *testing.T, target string) {
	test.Helper()
	operatingSystem, architectureName, _ := strings.Cut(target, "/")
	test.Setenv("GOOS", operatingSystem)
	test.Setenv("GOARCH", architectureName)
	savedOperatingSystem, savedArchitecture := build.Default.GOOS, build.Default.GOARCH
	build.Default.GOOS, build.Default.GOARCH = operatingSystem, architectureName
	test.Cleanup(func() {
		build.Default.GOOS, build.Default.GOARCH = savedOperatingSystem, savedArchitecture
	})
}
