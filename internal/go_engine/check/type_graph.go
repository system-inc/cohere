package check

import (
	"cmp"
	"go/types"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// typeGraph is one build's named types and aliases as the closure digests read them, each rendered once (#b2t9xmf): a
// node per type name, its lines as exportRendering writes them (renderedTypeName) but for the unexported methods no
// importer sees (importerSees), and an edge to every type name those lines name (directTypeNames over
// declarationTypes). A type name's closure is what its edges reach, so the closure
// digest of an object is its own line and the digests of the strongly connected components its types name, each
// component digested once from its members' lines and the digests of the components it points to. Two builds' digests
// of an object are equal exactly when every named type its closure reaches renders alike in both, which is what the
// whole closure written out would compare, and no type is rendered or walked again for the next object that reaches
// it. Its methods take its lock, so verdicts may run on several goroutines; one digest at a time is computed.
type typeGraph struct {
	lock  sync.Mutex
	nodes map[*types.TypeName]*typeNode

	// unexportedInterfaces are whether each package declares an interface with an unexported method of its own
	// (declaresUnexportedInterface), memoized, which decides whether an importer sees its unexported methods.
	unexportedInterfaces map[*types.Package]bool

	// sizes are the build's, which the properties of another package's unexported field are computed under
	// (directTypeNames), nil until the build is known, when every field is entered (measureBy), and walked whether
	// any walk has run, after which they are never set, so no two renderings of one graph differ in kind.
	sizes  types.Sizes
	walked bool

	// rendered, nil outside tests, is told of each type name as it is rendered, so a test can count the renderings.
	rendered func(*types.TypeName)
}

// typeNode is one type name of the graph: its rendered lines, the type names they name, and its component's digest
// once it is known.
type typeNode struct {
	typeName *types.TypeName
	lines    []string
	edges    []*types.TypeName
	digest   cache.Key
	digested bool
}

// newTypeGraph is an empty graph of one build's type names.
func newTypeGraph() *typeGraph {
	return &typeGraph{nodes: map[*types.TypeName]*typeNode{}, unexportedInterfaces: map[*types.Package]bool{}}
}

// importerSees reports whether a type name's node keeps method (importerSeesMethod). The caller holds the lock.
func (graph *typeGraph) importerSees(method *types.Func) bool {
	return importerSeesMethod(method, func(declaring *types.Package) bool {
		declares, known := graph.unexportedInterfaces[declaring]
		if !known {
			declares = declaresUnexportedInterface(declaring)
			graph.unexportedInterfaces[declaring] = declares
		}
		return declares
	})
}

// closureDigest is object's closure digest: its own line (ownRendering) and the digests of the components the named
// types its type names belong to, each once, sorted.
func (graph *typeGraph) closureDigest(object types.Object) cache.Key {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	graph.walked = true
	var components []cache.Key
	step := directTypeNames(rootTypes(object), graph.sizes)
	for _, root := range step.typeNames {
		components = append(components, graph.digestOf(root))
	}
	own := strings.Join(append([]string{ownRendering(object)}, step.observed...), "\n")
	return cache.DigestOf(digestParts([]byte(own), components)...)
}

// measureBy has the graph compute the properties of another package's unexported field under sizes, the build's,
// when no walk has run yet: a graph that walked before it knew its build keeps entering every field, which errs wide.
func (graph *typeGraph) measureBy(sizes types.Sizes) {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	if !graph.walked {
		graph.sizes = sizes
	}
}

// reachable are the type names roots name and every type name those reach, each once.
func (graph *typeGraph) reachable(roots []types.Type) []*types.TypeName {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	graph.walked = true
	pending := directTypeNames(roots, graph.sizes).typeNames
	seen := map[*types.TypeName]bool{}
	var reached []*types.TypeName
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[current] {
			continue
		}
		seen[current] = true
		reached = append(reached, current)
		pending = append(pending, graph.nodeOf(current).edges...)
	}
	return reached
}

// nodeOf is typeName's node, rendered and its edges found the first time it is asked. The caller holds the lock.
func (graph *typeGraph) nodeOf(typeName *types.TypeName) *typeNode {
	if node, known := graph.nodes[typeName]; known {
		return node
	}
	if graph.rendered != nil {
		graph.rendered(typeName)
	}
	step := directTypeNames(declarationTypes(typeName, graph.importerSees), graph.sizes)
	node := &typeNode{
		typeName: typeName,
		lines:    append(renderedTypeName(typeName, graph.importerSees), step.observed...),
		edges:    step.typeNames,
	}
	graph.nodes[typeName] = node
	return node
}

// digestOf is the digest of the component typeName belongs to, found by Tarjan's walk of what it reaches that has no
// digest yet, which digests each component it closes, every component it points to closed, and so digested, before
// it. The caller holds the lock.
func (graph *typeGraph) digestOf(start *types.TypeName) cache.Key {
	if node := graph.nodeOf(start); node.digested {
		return node.digest
	}
	index := map[*typeNode]int{}
	lowest := map[*typeNode]int{}
	onStack := map[*typeNode]bool{}
	var stack []*typeNode
	var connect func(node *typeNode)
	connect = func(node *typeNode) {
		index[node], lowest[node] = len(index), len(index)
		stack = append(stack, node)
		onStack[node] = true
		for _, target := range node.edges {
			successor := graph.nodeOf(target)
			if successor.digested {
				continue
			}
			if _, visited := index[successor]; !visited {
				connect(successor)
				lowest[node] = min(lowest[node], lowest[successor])
			} else if onStack[successor] {
				lowest[node] = min(lowest[node], index[successor])
			}
		}
		if lowest[node] != index[node] {
			return
		}
		var members []*typeNode
		for {
			member := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[member] = false
			members = append(members, member)
			if member == node {
				break
			}
		}
		graph.digestComponent(members)
	}
	connect(graph.nodeOf(start))
	return graph.nodes[start].digest
}

// digestComponent digests one component: each member's path, name and lines, sorted by path, name and lines, since
// two local types of one path and name are told apart by their lines alone, then the digests of the components its
// members point to, which are all digested, each once, sorted.
//
// Mutates: members each gains the component's digest
func (graph *typeGraph) digestComponent(members []*typeNode) {
	inComponent := make(map[*typeNode]bool, len(members))
	for _, member := range members {
		inComponent[member] = true
	}
	slices.SortFunc(members, func(left, right *typeNode) int {
		return cmp.Or(cmp.Compare(left.typeName.Pkg().Path(), right.typeName.Pkg().Path()),
			cmp.Compare(left.typeName.Name(), right.typeName.Name()), slices.Compare(left.lines, right.lines))
	})
	memberParts := [][]byte{[]byte("component"), []byte(strconv.Itoa(len(members)))}
	var successors []cache.Key
	for _, member := range members {
		memberParts = append(memberParts, []byte("named "+member.typeName.Pkg().Path()+"."+member.typeName.Name()),
			[]byte(strconv.Itoa(len(member.lines))))
		for _, line := range member.lines {
			memberParts = append(memberParts, []byte(line))
		}
		for _, target := range member.edges {
			if successor := graph.nodes[target]; !inComponent[successor] {
				successors = append(successors, successor.digest)
			}
		}
	}
	membersDigest := cache.DigestOf(memberParts...)
	digest := cache.DigestOf(digestParts(membersDigest[:], successors)...)
	for _, member := range members {
		member.digest, member.digested = digest, true
	}
}

// digestParts are head, then the count of digests, each once, and the digests sorted, as one digest's parts.
func digestParts(head []byte, digests []cache.Key) [][]byte {
	sorted := slices.Clone(digests)
	slices.SortFunc(sorted, func(left, right cache.Key) int { return slices.Compare(left[:], right[:]) })
	unique := slices.Compact(sorted)
	parts := make([][]byte, 0, len(unique)+2)
	parts = append(parts, head, []byte(strconv.Itoa(len(unique))))
	for _, digest := range unique {
		parts = append(parts, digest[:])
	}
	return parts
}
