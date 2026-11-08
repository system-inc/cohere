package platform

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// keyedSource declares one of each kind of object KeyOf keys and one of each it refuses.
const keyedSource = `package keyed

type Record struct{ Field int }

func (Record) Method() {}

func Exported() { local := 1; _ = local }

var unexported = 1
`

// checkKeyed type-checks keyedSource on its own, as one build's load does.
func checkKeyed(test *testing.T) *types.Package {
	test.Helper()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "keyed.go", keyedSource, 0)
	if parseError != nil {
		test.Fatalf("parsing: %v", parseError)
	}
	configuration := types.Config{Importer: importer.Default()}
	checked, checkError := configuration.Check("example.com/keyed", fileSet, []*ast.File{file}, nil)
	if checkError != nil {
		test.Fatalf("type-checking: %v", checkError)
	}
	return checked
}

// TestAnObjectKeysTheSameUnderTwoChecks holds KeyOf to naming an object the same way in two separate type checks of
// the same source, as the host's build and an extra build each check a dependency, for a function, a type, a method
// and a field, and to refusing what another package cannot name: an unexported variable and a local.
func TestAnObjectKeysTheSameUnderTwoChecks(test *testing.T) {
	test.Parallel()
	first, second := checkKeyed(test), checkKeyed(test)
	objectsOf := func(checked *types.Package) map[string]types.Object {
		record := checked.Scope().Lookup("Record")
		named, isNamed := record.Type().(*types.Named)
		if !isNamed {
			test.Fatalf("Record is %T, where a named type was expected", record.Type())
		}
		structure, isStruct := named.Underlying().(*types.Struct)
		if !isStruct {
			test.Fatalf("Record's underlying type is %T, where a struct was expected", named.Underlying())
		}
		return map[string]types.Object{
			"Exported": checked.Scope().Lookup("Exported"),
			"Record":   record,
			"Method":   named.Method(0),
			"Field":    structure.Field(0),
		}
	}
	firstObjects, secondObjects := objectsOf(first), objectsOf(second)
	for name, object := range firstObjects {
		firstKey, firstKeyed := KeyOf(object)
		secondKey, secondKeyed := KeyOf(secondObjects[name])
		if !firstKeyed || !secondKeyed {
			test.Errorf("%s has no key, where every object another package can name has one", name)
			continue
		}
		if firstKey != secondKey {
			test.Errorf("%s keys as %v in one check and %v in the other", name, firstKey, secondKey)
		}
		if firstKey.PackagePath != "example.com/keyed" {
			test.Errorf("%s keys under package %q", name, firstKey.PackagePath)
		}
	}
	if _, keyed := KeyOf(first.Scope().Lookup("unexported")); keyed {
		test.Error("an unexported variable has a key, which no other package can name")
	}
	exported, isFunction := first.Scope().Lookup("Exported").(*types.Func)
	if !isFunction {
		test.Fatal("Exported is not a function")
	}
	local := exported.Scope().Lookup("local")
	if local == nil {
		test.Fatal("Exported's scope holds no local, so the local case checks nothing")
	}
	if _, keyed := KeyOf(local); keyed {
		test.Error("a local has a key, which no other package can name")
	}
	if _, keyed := KeyOf(nil); keyed {
		test.Error("a nil object has a key")
	}
}
