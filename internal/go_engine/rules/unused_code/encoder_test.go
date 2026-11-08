package unused_code

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/unused"
)

// The vendored original stays the oracle the port is held to, so the one change cohere's copy makes to it is held
// here too, moved from package rules with the registry's U1000.

// encoderTypes is how many struct types the smaller generated package declares, each with one exported field, and
// the larger declares twice as many. A field is an object U1000 records an object path for, and its path is found
// by walking the package's types.
const encoderTypes = 400

// encoderGrowthBound is the most U1000's allocated bytes may grow when the package doubles. With one encoder for the
// package, as upstream's fa9ae6d1a5 made it, the bytes grow with the package, a ratio near 2. With a fresh encoder
// per object, as v0.8.1 shipped, each field's path lists and sorts the whole package scope again, so the bytes grow
// with its square and the ratio nears 4. Measured when this was written: 2.29 shared, 3.44 with the release's line
// put back. The count holds still between runs because the copy is pinned and the generated package is the same every
// time, so the same code allocates the same bytes, and the lowest of three runs sets aside a collection landing
// inside one.
const encoderGrowthBound = 2.8

// TestU1000SharesOneObjectPathEncoderAcrossAPackage holds the one change cohere's copy of staticcheck makes to its
// release (third_party/honnef.co/go/tools/README.cohere.md, #j3bfnyt): U1000 finds every object path in a package
// through one objectpath.Encoder. The encoder's memo is unexported, so this counts what it saves instead: the bytes
// U1000 allocates over a generated package, against a package twice its size. A count, never a time, so a loaded
// machine reads the same as an idle one.
//
// Not parallel: it reads the process's allocation counters, which a test running beside it would add to.
func TestU1000SharesOneObjectPathEncoderAcrossAPackage(test *testing.T) {
	small := bytesU1000Allocates(test, encoderTypes)
	large := bytesU1000Allocates(test, 2*encoderTypes)
	ratio := float64(large) / float64(small)
	test.Logf("U1000 allocated %d bytes over %d types and %d over %d, a ratio of %.2f",
		small, encoderTypes, large, 2*encoderTypes, ratio)
	if ratio > encoderGrowthBound {
		test.Errorf("doubling the package grew U1000's allocations %.2f times, above %.1f: its object paths are "+
			"found by a fresh traversal per object, not one encoder per package", ratio, encoderGrowthBound)
	}
}

// bytesU1000Allocates type-checks a generated package of count struct types, runs U1000 over it, and returns the
// fewest bytes one run allocated in three, the type check and U1000's prerequisites outside the count. It fails the
// test unless every field came back with an object path, so the count is of the path finding it means to hold.
func bytesU1000Allocates(test *testing.T, count int) uint64 {
	test.Helper()
	pass := generatedPass(test, count)
	fewest := uint64(0)
	for run := range 3 {
		var before runtime.MemStats
		var after runtime.MemStats
		runtime.ReadMemStats(&before)
		result, runError := unused.Analyzer.Analyzer.Run(pass)
		runtime.ReadMemStats(&after)
		if runError != nil {
			test.Fatalf("running U1000 over %d types: %v", count, runError)
		}
		allocated := after.TotalAlloc - before.TotalAlloc
		if run == 0 || allocated < fewest {
			fewest = allocated
		}
		usedResult, isResult := result.(unused.Result)
		if !isResult {
			test.Fatalf("U1000's result is %T", result)
		}
		fieldsWithPaths := 0
		for _, object := range usedResult.Used {
			if object.Kind == "field" && object.Path.ObjPath != "" {
				fieldsWithPaths++
			}
		}
		if fieldsWithPaths != count {
			test.Fatalf("U1000 found an object path for %d of the %d fields", fieldsWithPaths, count)
		}
	}
	return fewest
}

// generatedPass is an analysis pass over a package declaring count struct types, `Type0` on, each with the exported
// field `Field`, type-checked in process, with the results of U1000's two prerequisites already in it.
func generatedPass(test *testing.T, count int) *analysis.Pass {
	test.Helper()
	var source strings.Builder
	source.WriteString("package generated\n\n")
	for index := range count {
		fmt.Fprintf(&source, "type Type%d struct{ Field int }\n", index)
	}
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "generated.go", source.String(), parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing the generated package: %v", parseError)
	}
	information := &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Instances:    map[*ast.Ident]types.Instance{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		Scopes:       map[ast.Node]*types.Scope{},
		FileVersions: map[*ast.File]string{},
	}
	checked, checkError := (&types.Config{}).Check("example.com/generated", fileSet, []*ast.File{file}, information)
	if checkError != nil {
		test.Fatalf("type-checking the generated package: %v", checkError)
	}
	pass := &analysis.Pass{
		Fset:      fileSet,
		Files:     []*ast.File{file},
		Pkg:       checked,
		TypesInfo: information,
		ResultOf:  map[*analysis.Analyzer]any{},
	}
	for _, prerequisite := range unused.Analyzer.Analyzer.Requires {
		result, runError := prerequisite.Run(pass)
		if runError != nil {
			test.Fatalf("running U1000's prerequisite %s: %v", prerequisite.Name, runError)
		}
		pass.ResultOf[prerequisite] = result
	}
	return pass
}
