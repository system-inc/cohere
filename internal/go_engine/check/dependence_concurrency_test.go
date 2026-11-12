package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// genericPrefix is the path every package of the generic fixture is under, by its directory's name.
const genericPrefix = "example.com/generic/"

// genericFiles are a fixture whose types the dependence walk reaches through generic instances: gen declares generic
// types, with methods on generic receivers and a recursive field, and use instantiates them in signatures, fields,
// map values and embedded fields, nested in one another. Nothing expands an instance's methods or fields until a walk
// asks, since go/types expands a Named instance lazily (golang/go#81122 is a race in that unpacking).
var genericFiles = map[string]string{
	"gen/gen.go": "package gen\n\n" +
		"type Box[T any] struct {\n\tValue T\n\tnext  *Box[T]\n}\n\n" +
		"func (box *Box[T]) Get() T { return box.Value }\n\n" +
		"type Pair[K comparable, V any] struct {\n\tKey   K\n\tValue V\n\tInner Box[V]\n}\n\n" +
		"func (pair Pair[K, V]) Swap() Pair[K, V] { return pair }\n\n" +
		"type Lister[T any] interface{ List() []T }\n",
	"use/use.go": "package use\n\nimport \"example.com/generic/gen\"\n\n" +
		"type Holder struct {\n\tBoxes gen.Box[int]\n\tPairs map[string]gen.Pair[string, gen.Box[int]]\n}\n\n" +
		"type Embedded struct {\n\tgen.Box[string]\n\tgen.Pair[int, []gen.Box[bool]]\n}\n\n" +
		"func Make() gen.Pair[int, gen.Box[string]] { return gen.Pair[int, gen.Box[string]]{} }\n\n" +
		"func (holder *Holder) Items() gen.Lister[gen.Box[int]] { return nil }\n",
}

// genericOrder is the fixture's packages by directory, each after every one it imports.
var genericOrder = []string{"gen", "use"}

// genericUsers are the user packages, each reading the fixture through its instances one way, by name.
var genericUsers = map[string]string{
	"copied":   "import \"example.com/generic/use\"\n\nfunc Copy() any {\n\tvalue := use.Make()\n\treturn value\n}\n",
	"embedded": "import \"example.com/generic/use\"\n\nfunc Get(value *use.Embedded) string { return value.Get() }\n",
	"swapped": "import \"example.com/generic/use\"\n\n" +
		"func Swap(value use.Embedded) any { return value.Swap() }\n",
	"listed": "import \"example.com/generic/use\"\n\n" +
		"func List(holder *use.Holder) any { return holder.Items().List() }\n",
	"held": "import \"example.com/generic/use\"\n\nfunc Copy(holder *use.Holder) any { return *holder }\n",
}

// genericHost type-checks the generic fixture from source, as the host's run checks a workspace, and its memo, fresh.
func genericHost(test *testing.T) (map[string]*types.Package, *ReadsBuild) {
	test.Helper()

	directory := test.TempDir()
	writeFiles(test, directory, genericFiles)
	fileSet := token.NewFileSet()
	checked := map[string]*types.Package{}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if found, isFound := checked[path]; isFound {
			return found, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	for _, name := range genericOrder {
		file := filepath.Join(directory, name, name+".go")
		parsed, parseError := parser.ParseFile(fileSet, file, nil, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		checkedPackage, checkError := configuration.Check(genericPrefix+name, fileSet, []*ast.File{parsed}, nil)
		if checkError != nil {
			test.Fatalf("type-checking %s: %v", name, checkError)
		}
		checked[genericPrefix+name] = checkedPackage
	}
	return checked, measuredReadsBuild()
}

// genericRestoring is the user package named user checked over host, its reads recorded from its type information
// and syntax, as the run records them.
func genericRestoring(test *testing.T, host map[string]*types.Package, user string) Restoring {
	test.Helper()

	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "user.go", "package user\n\n"+genericUsers[user],
		parser.SkipObjectResolution)
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
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if found, isFound := host[path]; isFound {
			return found, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	checked, checkError := configuration.Check(genericPrefix+user, fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("type-checking the %s user: %v", user, checkError)
	}
	reads := &build_reads.Reads{}
	reads.RecordTypes(checked, info, []*ast.File{file})
	imports := make([]string, 0, len(checked.Imports()))
	for _, imported := range checked.Imports() {
		imports = append(imports, imported.Path())
	}
	return Restoring{Path: checked.Path(), Imports: imports, Reads: reads}
}

// Every extra build computes its candidates' dependence sets at once over the one host memo they share (#pkn84vs),
// walking the host's types after its check, generic instances among them, which go/types expands lazily. Here eight
// goroutines per user, each user's twice over, ask a fresh memo for the same packages' sets and different ones at
// once, and every set of one package is the same set: run under -race through cohere-dev, a walk that writes what
// another reads without the memo's lock is a failure.
func TestDependenceSetsComputedAtOnceOverOneHostAgree(test *testing.T) {
	test.Parallel()

	host, memo := genericHost(test)
	restorings := map[string]Restoring{}
	for user := range genericUsers {
		restorings[user] = genericRestoring(test, host, user)
	}
	const asksPerUser = 8
	var setsLock sync.Mutex
	sets := map[string][]DependenceSet{}
	var group sync.WaitGroup
	start := make(chan struct{})
	for user, restoring := range restorings {
		for range asksPerUser {
			group.Go(func() {
				// Every ask waits for the rest, so the walks overlap rather than run one after another.
				<-start
				set := memo.Dependence(restoring)
				setsLock.Lock()
				sets[user] = append(sets[user], set)
				setsLock.Unlock()
			})
		}
	}
	close(start)
	group.Wait()
	for user, got := range sets {
		if len(got) != asksPerUser {
			test.Errorf("%s: %d sets came back, want %d", user, len(got), asksPerUser)
			continue
		}
		first := dependenceNames(got[0])
		for _, again := range got[1:] {
			if names := dependenceNames(again); fmt.Sprint(names) != fmt.Sprint(first) {
				test.Errorf("%s: one ask's set is %v and another's %v, want one set", user, first, names)
			}
		}
		if len(first) == 0 {
			test.Errorf("%s: the set is empty, want the generic packages its reads reach", user)
		}
	}
}
