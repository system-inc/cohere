package load

import (
	"fmt"
	"go/types"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/bounded"

	"golang.org/x/tools/go/packages"
)

// A package's priority is its cost plus the largest priority among its importers: on a chain of three, c2 importing
// c1 importing c0, c2 also importing two leaves and c0 a package the cache restores, every package counts the files it
// parses and the restored one 1, and each adds the longest path of what imports it.
func TestPrioritiesAddTheLongestPathOfTheImporters(test *testing.T) {
	test.Parallel()
	built := &graph{nodes: map[string]*node{}}
	add := func(identifier string, files int, imports ...string) *node {
		added := &node{loaded: &packages.Package{ID: identifier, Imports: map[string]*packages.Package{}}}
		for index := range files {
			added.loaded.CompiledGoFiles = append(added.loaded.CompiledGoFiles, fmt.Sprintf("%s/%d.go", identifier,
				index))
		}
		for _, imported := range imports {
			added.loaded.Imports[imported] = built.nodes[imported].loaded
		}
		built.nodes[identifier] = added
		return added
	}
	// In connect's postorder, every importer after what it imports.
	reachable := []*node{
		add("restored", 9),
		add("c0", 1, "restored"),
		add("c1", 2, "c0"),
		add("wide", 4),
		add("narrow", 1),
		add("c2", 3, "c1", "wide", "narrow"),
	}
	typeChecker := &loader{
		graph:    built,
		restored: map[*packages.Package]*Restored{built.nodes["restored"].loaded: {}},
	}
	priorities := typeChecker.prioritize(reachable)
	want := map[string]int{"c2": 3, "c1": 5, "c0": 6, "wide": 7, "narrow": 4, "restored": 7}
	for _, current := range reachable {
		if priorities[current] != want[current.loaded.ID] {
			test.Errorf("%s has priority %d, want %d", current.loaded.ID, priorities[current],
				want[current.loaded.ID])
		}
	}
}

// orderLeaves is how many leaf packages the order fixture has beside its chain, and orderChain how long the chain is.
const (
	orderLeaves = 12
	orderChain  = 4
)

// orderFixture is a module of many leaf packages that import nothing and a chain beside them, each link importing the
// one before it, so link0 is the root every other link waits on. The leaves sort first, so the listing, and the load,
// start them first.
func orderFixture() map[string]string {
	files := map[string]string{"order/go.mod": "module example.com/order\n\ngo 1.21\n"}
	for index := range orderLeaves {
		name := fmt.Sprintf("leaf%02d", index)
		files["order/"+name+"/"+name+".go"] = fmt.Sprintf("package %s\n\nconst Name = %q\n", name, name)
	}
	files["order/link0/link0.go"] = "package link0\n\nconst Name = \"link0\"\n"
	for index := 1; index < orderChain; index++ {
		files[fmt.Sprintf("order/link%d/link%d.go", index, index)] = fmt.Sprintf(
			"package link%d\n\nimport \"example.com/order/link%d\"\n\nconst Name = link%d.Name\n", index,
			index-1, index-1)
	}
	return files
}

// Under one slot, held until every package's read waits for it, the chain's root is granted its parse before any
// leaf is, though every leaf started before it: the chain's root has the longest path to the end of the graph.
func TestTheLoadParsesTheLongestPathFirst(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeListingFiles(test, directory, orderFixture())
	workspace := directory + "/order"
	goEnvironment := goEnvironmentOf(test, workspace, []string{"GOWORK=off"})
	listed, listError := list(goEnvironment, []string{"./..."}, nil, nil, nil, nil)
	if listError != nil {
		test.Fatal(listError)
	}
	sizes := types.SizesFor("gc", runtime.GOARCH)
	if sizes == nil {
		test.Fatalf("go/types knows no sizes for %s", runtime.GOARCH)
	}
	typeChecker := newLoader(newGraph(listed, workspace, nil), sizes, newWithheldErrors(), nil, nil, nil)
	reachable := typeChecker.connect()
	if len(reachable) != orderLeaves+orderChain {
		test.Fatalf("want %d packages reachable, got %d", orderLeaves+orderChain, len(reachable))
	}
	typeChecker.slots = bounded.NewPool(1)
	var grantedLock sync.Mutex
	var parsed []string
	var read []string
	typeChecker.observeGrant = func(identifier string, work slotWork) {
		grantedLock.Lock()
		defer grantedLock.Unlock()
		switch work {
		case slotWorkParse:
			parsed = append(parsed, strings.TrimPrefix(identifier, "example.com/order/"))
		case slotWorkRead:
			read = append(read, strings.TrimPrefix(identifier, "example.com/order/"))
		}
	}
	typeChecker.slots.Acquire(0)
	loaded := make(chan struct{})
	go func() {
		defer close(loaded)
		typeChecker.load(reachable)
	}()
	// Every package has one file, whose read is the first slot it asks for.
	for typeChecker.slots.Waiters() < len(reachable) {
		runtime.Gosched()
	}
	typeChecker.slots.Release()
	<-loaded
	for _, current := range reachable {
		if len(current.loaded.Errors) > 0 {
			test.Fatalf("%s: %v", current.loaded.ID, current.loaded.Errors)
		}
	}
	// The reads waiting together go by priority, so the chain's three heaviest links come first and in order, whatever
	// order the sixteen arrived in; granted by arrival, that order is one of 3,360, so the check fails then in all but
	// a sliver of runs, where the parse order below is checked too. link3 ties the leaves at a priority of 1.
	chainFirst := []string{"link0", "link1", "link2"}
	if len(read) < len(chainFirst) || !slices.Equal(read[:len(chainFirst)], chainFirst) {
		test.Fatalf("the reads were granted %v, want %v first", read, chainFirst)
	}
	root := slices.Index(parsed, "link0")
	if root < 0 {
		test.Fatalf("link0 was never granted a parse, the parses granted were %v", parsed)
	}
	for _, identifier := range parsed[:root] {
		if strings.HasPrefix(identifier, "leaf") {
			test.Fatalf("%s was granted its parse before link0, the root of the chain: parses granted %v", identifier,
				parsed)
		}
	}
}
