package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// chainSources are three packages, leaf imported by middle imported by top, each with types that point at each other
// within the package and down the chain, so a digest of one of top's objects reaches every package's components.
var chainSources = map[string]string{
	"example.com/leaf": `package leaf

type First struct{ next *Second }
type Second struct {
	back *First
	call func(First) Second
}
`,
	"example.com/middle": `package middle

import "example.com/leaf"

type Node struct {
	value leaf.First
	self  *Node
	more  []Other
}
type Other map[string]*Node
`,
	"example.com/top": `package top

import "example.com/middle"

type Root struct{ held middle.Node }
`,
}

// chainWorld is chainSources checked, with forty variables in top, each of a type that reaches down the chain, so
// many verdicts digest at once into the same components.
func chainWorld(test *testing.T) []types.Object {
	test.Helper()
	fileSet := token.NewFileSet()
	checked := map[string]*types.Package{}
	check := func(path string, source string) *types.Package {
		parsed, parseError := parser.ParseFile(fileSet, path+".go", source, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		configuration := types.Config{Importer: importerFunction(func(imported string) (*types.Package, error) {
			return checked[imported], nil
		})}
		checkedPackage, checkError := configuration.Check(path, fileSet, []*ast.File{parsed}, nil)
		if checkError != nil {
			test.Fatal(checkError)
		}
		checked[path] = checkedPackage
		return checkedPackage
	}
	check("example.com/leaf", chainSources["example.com/leaf"])
	check("example.com/middle", chainSources["example.com/middle"])
	var top strings.Builder
	top.WriteString(chainSources["example.com/top"])
	for index := range 40 {
		fmt.Fprintf(&top, "var Held%d [%d]struct{ root Root; other middle.Other }\n", index, index+1)
	}
	topPackage := check("example.com/top", top.String())
	var objects []types.Object
	for _, name := range topPackage.Scope().Names() {
		objects = append(objects, topPackage.Scope().Lookup(name))
	}
	return objects
}

// Verdicts digesting at once, each package's type names under that package's own lock, finish, and every digest is the
// one a single verdict computes alone. The graph takes no lock while it holds another but the leaf ones, so no order
// of digests can deadlock; a later change that took the packages' locks while holding another's, in an order that
// could cycle, would stop here, and the deadline names it.
func TestVerdictsDigestingAtOnceFinishWithTheDigestsOfOne(test *testing.T) {
	test.Parallel()

	objects := chainWorld(test)
	const verdicts = 32
	want := make(map[types.Object]cache.Key, len(objects))
	got := make([]map[types.Object]cache.Key, verdicts)
	finished := make(chan struct{})
	// Every digest runs under the deadline, the one verdict's alone too, since a lock taken in an order that deadlocks
	// stops a verdict alone as surely as many.
	go func() {
		defer close(finished)
		alone := newTypeGraph()
		for _, object := range objects {
			want[object] = alone.closureDigest(object)
		}
		shared := newTypeGraph()
		var digesting sync.WaitGroup
		for verdict := range verdicts {
			got[verdict] = map[types.Object]cache.Key{}
			digesting.Go(func() {
				// Each verdict walks the objects from its own place, so they meet the same components in every order.
				for offset := range objects {
					object := objects[(verdict+offset)%len(objects)]
					got[verdict][object] = shared.closureDigest(object)
				}
			})
		}
		digesting.Wait()
	}()
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		test.Fatal("verdicts digesting at once did not finish in 30 s: the type graph's locks deadlocked")
	}
	for verdict, digests := range got {
		for object, digest := range digests {
			if digest != want[object] {
				test.Errorf("verdict %d digested %s as %x, want the digest one verdict computes alone, %x",
					verdict, object.Name(), digest, want[object])
			}
		}
	}
}
