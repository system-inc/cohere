package build_reads

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// The unplaced fixture's paths: dependency, whose two structs a value of the user's own reaches through a pointer and
// a slice, and the user.
const (
	unplacedDependency = "example.com/unplaced/dependency"
	unplacedUser       = "example.com/unplaced/user"
)

// unplacedHolder is the user's own type, whose fields reach dependency's Record through a pointer and its Inner
// through a slice, so no copy of a holder, or of a pointer to one, holds either by value: only a look through the
// value reaches them.
const unplacedHolder = "type holder struct {\n\trecord *dependency.Record\n\titems  []dependency.Inner\n}\n\n"

// unplacedUsers are the users, each handing a pointer to a holder to a place the walk cannot see, one for every path
// that leaves a moved value's destination unknown (useWalk.movedTo with no destination):
//   - a call of a value whose type is a type parameter, whose parameters the walk cannot read (placeCall);
//   - an argument of print or println, the builtins placeBuiltin does not name (recover takes none);
//   - an assignment to the blank identifier, whose type the type checker records none of, alone and as one of a
//     call's several results (placeAssignment, placeValues, and value's tuple);
//   - an element appended to, a key deleted from, a value sent on, and an element of a literal of a value whose type is
//     a type parameter, whose slice, map, channel or literal type the walk cannot read (placeBuiltin, place,
//     placeComposite).
//
// Every other expression the walk leaves unplaced is a condition (`if`, `for`) or an array's length, a boolean or an
// integer, which hold nothing a look could reach past the value itself. A struct literal's key that names no field and
// a return whose function the walk cannot read leave a destination unknown too, but only in code the type checker
// refuses.
var unplacedUsers = map[string]string{
	"a call of a type parameter": "func apply[Call func(*holder)](call Call, value *holder) { call(value) }\n",
	"print":                      "func show(value *holder) { print(value) }\n",
	"println":                    "func show(value *holder) { println(value) }\n",
	"the blank identifier":       "func drop(value *holder) { _ = value }\n",
	"the blank identifier of a call's results": "func pair(value *holder) (*holder, error) { return value, nil }\n\n" +
		"func drop(value *holder) { _, _ = pair(value) }\n",
	"append to a type parameter": "func add[Items ~[]*holder](items Items, value *holder) Items {\n" +
		"\treturn append(items, value)\n}\n",
	"delete from a type parameter": "func drop[Items ~map[*holder]bool](items Items, value *holder) {\n" +
		"\tdelete(items, value)\n}\n",
	"send on a type parameter": "func send[Channel ~chan *holder](channel Channel, value *holder) {\n" +
		"\tchannel <- value\n}\n",
	"a literal of a type parameter": "func wrap[Items ~[]*holder](value *holder) Items { return Items{value} }\n",
}

// unplacedReads are the reads the user named user records, checked over dependency.
func unplacedReads(test *testing.T, user string) typeReads {
	test.Helper()

	fileSet := token.NewFileSet()
	dependencySource := "package dependency\n\ntype Inner struct{ Count int }\n\n" +
		"type Record struct {\n\tName string\n\tNext *Inner\n}\n"
	dependencyFile, parseError := parser.ParseFile(fileSet, "dependency.go", dependencySource,
		parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	dependency, checkError := (&types.Config{}).Check(unplacedDependency, fileSet, []*ast.File{dependencyFile}, nil)
	if checkError != nil {
		test.Fatal(checkError)
	}
	source := "package user\n\nimport \"" + unplacedDependency + "\"\n\n" + unplacedHolder + unplacedUsers[user]
	userFile, parseError := parser.ParseFile(fileSet, "user.go", source, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := types.Config{Importer: importerOf(func(path string) (*types.Package, error) {
		if path == unplacedDependency {
			return dependency, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	checked, checkError := configuration.Check(unplacedUser, fileSet, []*ast.File{userFile}, info)
	if checkError != nil {
		test.Fatalf("type-checking the user that reaches by %s: %v", user, checkError)
	}
	return collectTypeReads(checked, info, []*ast.File{userFile})
}

// importerOf is a types.Importer of a function.
type importerOf func(path string) (*types.Package, error)

// Import is the package at path.
func (importer importerOf) Import(path string) (*types.Package, error) {
	return importer(path)
}

// A value moved to a place the walk cannot see may be handed to an empty interface there, which any rule may look
// through, so every path that leaves its destination unknown records every type a look through it reaches as a use of
// the type whole: a pointer to a holder, which holds dependency's types through a pointer and a slice alone, reaches
// Record and Inner, though no copy of it holds either. Recording the copy alone would restore a package whose printf
// or marshaller finding hangs on a field of either in another build.
func TestEveryValueMovedWhereTheWalkCannotSeeRecordsItsWholeReach(test *testing.T) {
	test.Parallel()

	for user := range unplacedUsers {
		test.Run(user, func(test *testing.T) {
			test.Parallel()

			collected := unplacedReads(test, user)
			if collected.closureOnly != "" {
				test.Fatalf("the reads are judged by closure alone, so the walk tells nothing: %s",
					collected.closureOnly)
			}
			for _, typeName := range []string{"Record", "Inner"} {
				found := false
				for use := range collected.uses {
					if use.Kind == UseWhole && use.Object.Name() == typeName &&
						use.Object.Pkg().Path() == unplacedDependency {
						found = true
					}
				}
				if !found {
					test.Errorf("handing a pointer to a holder to %s, want dependency.%s read whole, got %v", user,
						typeName, collected.uses)
				}
			}
		})
	}
}
