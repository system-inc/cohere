package consistency_no_any

import (
	"fmt"
	"go/token"
	"go/types"
	"runtime"
	"testing"
)

// declaringPackage is a package declaring count named types, each with one exported method of its own name, imported
// by the package importer returns.
func declaringPackage(count int) *types.Package {
	declaring := types.NewPackage("example.com/foreign", "foreign")
	for index := range count {
		typeName := types.NewTypeName(token.NoPos, declaring, fmt.Sprintf("Type%d", index), nil)
		named := types.NewNamed(typeName, types.NewStruct(nil, nil), nil)
		receiver := types.NewVar(token.NoPos, declaring, "receiver", named)
		named.AddMethod(types.NewFunc(token.NoPos, declaring, fmt.Sprintf("Store%d", index),
			types.NewSignatureType(receiver, nil, nil, nil, nil, false)))
		declaring.Scope().Insert(typeName)
	}
	declaring.MarkComplete()
	return declaring
}

// importer is a package importing declaring alone.
func importer(declaring *types.Package) *types.Package {
	importing := types.NewPackage("example.com/importing", "importing")
	importing.SetImports([]*types.Package{declaring})
	return importing
}

// A load reads each package's methods once, however many of its packages reach it, so a package reaching one that
// declares a single type allocates as much as one reaching a package that declares hundreds, once the load has read
// each. A rule that indexed the closure afresh for each package would read every type again, and grow its index.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestALoadReadsEachPackagesMethodsOnce(test *testing.T) {
	fileSet := token.NewFileSet()
	few := importer(declaringPackage(1))
	many := importer(declaringPackage(256))
	reached := reachedMethodsOf(methodsByLoad.Load(fileSet), many)
	if len(reached) != 1 {
		test.Fatalf("want the one package reached, got %d", len(reached))
	}
	if len(reached[0].byName) != 256 {
		test.Fatalf("want the package's 256 methods indexed, got %d", len(reached[0].byName))
	}
	if again := reachedMethodsOf(methodsByLoad.Load(fileSet), many); again[0] != reached[0] {
		test.Error("the load read the package's methods again, where it must hand back what it read first")
	}
	fewAllocations := testing.AllocsPerRun(20, func() { reachedMethodsOf(methodsByLoad.Load(fileSet), few) })
	manyAllocations := testing.AllocsPerRun(20, func() { reachedMethodsOf(methodsByLoad.Load(fileSet), many) })
	if manyAllocations != fewAllocations {
		test.Errorf("reaching 256 types allocated %.0f times, one type %.0f, where the two must be the same",
			manyAllocations, fewAllocations)
	}
	// The memo holds the file set weakly, so the test keeps it alive until it has counted.
	runtime.KeepAlive(fileSet)
}
