package intermediate_representation_test

// A package created from its types alone creates each member on its first lookup, once (#npb86tz).

import (
	"go/importer"
	"go/token"
	"go/types"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// TestAPackageFromItsTypesCreatesEachMemberOnFirstLookup creates strings from its types alone and holds it to no
// function but its initializer until one is looked up, then to exactly one more however many goroutines look it up
// at once, the same one by name, and to go/ir's every member once Members walks them. A CreatePackage that created
// every member, as go/ir's does, fails the first count.
func TestAPackageFromItsTypesCreatesEachMemberOnFirstLookup(test *testing.T) {
	test.Parallel()
	stringsTypes, importError := importer.Default().Import("strings")
	if importError != nil {
		test.Fatal(importError)
	}
	program := intermediate_representation.NewProgram(token.NewFileSet())
	created := program.CreatePackage(stringsTypes, nil, nil, true)
	if len(created.Functions) != 1 {
		test.Fatalf("strings was created with %d functions, where only its initializer was wanted before a lookup",
			len(created.Functions))
	}

	title, isFunction := stringsTypes.Scope().Lookup("Title").(*types.Func)
	if !isFunction {
		test.Fatal("strings.Title is no function")
	}
	found := make([]*intermediate_representation.Function, 16)
	var waitGroup sync.WaitGroup
	for index := range found {
		waitGroup.Go(func() {
			found[index] = program.FunctionValue(title)
		})
	}
	waitGroup.Wait()
	if len(created.Functions) != 2 {
		test.Fatalf("strings holds %d functions after %d lookups of Title, want its initializer and Title",
			len(created.Functions), len(found))
	}
	for index, each := range found {
		if each == nil || each != found[0] || each.Object() != types.Object(title) {
			test.Fatalf("lookup %d of strings.Title found %v, where every lookup should find the one member", index,
				each)
		}
	}
	if created.Function("Title") != found[0] {
		test.Fatal("strings.Function(\"Title\") isn't the member FunctionValue created")
	}

	builder, isTypeName := stringsTypes.Scope().Lookup("Builder").(*types.TypeName)
	if !isTypeName {
		test.Fatal("strings.Builder is no type name")
	}
	named, isNamed := builder.Type().(*types.Named)
	if !isNamed {
		test.Fatal("strings.Builder is no named type")
	}
	var writeString *types.Func
	for index := range named.NumMethods() {
		if named.Method(index).Name() == "WriteString" {
			writeString = named.Method(index)
		}
	}
	if writeString == nil {
		test.Fatal("strings.Builder has no WriteString")
	}
	method := program.FunctionValue(writeString)
	if method == nil || program.FunctionValue(writeString) != method || len(created.Functions) != 3 {
		test.Fatalf("looking up (*strings.Builder).WriteString twice found %v and left %d functions, want one "+
			"member and 3", method, len(created.Functions))
	}

	// go/ir's count: the initializer, then every function of the scope and every method of its named types.
	functions := 1
	scope := stringsTypes.Scope()
	for _, name := range scope.Names() {
		switch object := scope.Lookup(name).(type) {
		case *types.Func:
			functions++
		case *types.TypeName:
			if objectNamed, isObjectNamed := object.Type().(*types.Named); isObjectNamed {
				functions += objectNamed.NumMethods()
			}
		}
	}
	members := created.Members()
	if len(created.Functions) != functions {
		test.Fatalf("strings holds %d functions after Members, where go/ir creates %d", len(created.Functions),
			functions)
	}
	// Each object of the scope, the initializer and its guard.
	if len(members) != len(scope.Names())+2 {
		test.Fatalf("strings holds %d members, where go/ir creates %d", len(members), len(scope.Names())+2)
	}
	if members["Title"] != found[0] || program.FunctionValue(writeString) != method {
		test.Fatal("Members created again a member a lookup had created")
	}
}

// TestABuildCreatesOnlyTheImportMembersItReaches builds a package whose one function calls strings.ToUpper, and
// holds strings, created from its types alone, to its initializer and ToUpper. A CreatePackage that created every
// member fails the count.
func TestABuildCreatesOnlyTheImportMembersItReaches(test *testing.T) {
	test.Parallel()
	built, _ := buildPackage(test, `package main

import "strings"

func Shout(text string) string { return strings.ToUpper(text) }
`, importer.Default())
	imported := built.Program.ImportedPackage("strings")
	if imported == nil {
		test.Fatal("strings wasn't created")
	}
	if len(imported.Functions) != 2 || imported.Functions[1].Name() != "ToUpper" {
		names := make([]string, 0, len(imported.Functions))
		for _, function := range imported.Functions {
			names = append(names, function.Name())
		}
		test.Fatalf("building Shout created %q in strings, want its initializer and ToUpper", names)
	}
}
