package check

import (
	"cmp"
	"fmt"
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
// declarationTypes). A type name's closure is what its edges reach, so the closure digest of an object is its own line
// and the digests of the strongly connected components its types name, each component digested once from its members'
// lines and the digests of the components it points to. Two builds' digests of an object are equal exactly when every
// named type its closure reaches renders alike in both, which is what the whole closure written out would compare, and
// no type is rendered or walked again for the next object that reaches it.
//
// Verdicts run on several goroutines, so each package's nodes have a lock of their own (packageTypeGraph), and two
// verdicts digesting types of different packages never wait on each other. A component never spans packages: an edge
// from a package's type names reaches only its own or the packages its declarations name, which it reaches through its
// imports, and imports never cycle. No lock is held while another is taken but for the leaf locks, the graph's own
// (packageOf, sizesForWalk) and interfacesLock, which take none, so no order of taking them can deadlock: a digest
// renders its package's part under that package's lock, lets it go, digests what the part points to in other packages
// under theirs, and takes its own again to close its components (digestOf).
type typeGraph struct {
	// lock guards packages, sizes and walked.
	lock     sync.Mutex
	packages map[*types.Package]*packageTypeGraph

	// unexportedInterfaces are whether each package declares an interface with an unexported method of its own
	// (declaresUnexportedInterface), memoized under interfacesLock, which decides whether an importer sees its
	// unexported methods.
	interfacesLock       sync.Mutex
	unexportedInterfaces map[*types.Package]bool

	// sizes are the build's, which the properties of another package's unexported field are computed under
	// (directTypeNames), nil until the build is known, when every field is entered (measureBy), and walked whether
	// any walk has run, after which they are never set, so no two renderings of one graph differ in kind.
	sizes  types.Sizes
	walked bool

	// rendered, nil outside tests, is told of each type name as it is rendered, under its package's lock, so a test
	// can count the renderings.
	rendered func(*types.TypeName)
}

// packageTypeGraph is one package's type names in a graph, by type name, each a node, under the package's own lock.
type packageTypeGraph struct {
	lock  sync.Mutex
	nodes map[*types.TypeName]*typeNode
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
	return &typeGraph{packages: map[*types.Package]*packageTypeGraph{},
		unexportedInterfaces: map[*types.Package]bool{}}
}

// importerSees reports whether a type name's node keeps method (importerSeesMethod), asking each declaring package
// once whether it declares an interface with an unexported method, outside interfacesLock, which the answer is kept
// under.
func (graph *typeGraph) importerSees(method *types.Func) bool {
	return importerSeesMethod(method, func(declaring *types.Package) bool {
		graph.interfacesLock.Lock()
		declares, known := graph.unexportedInterfaces[declaring]
		graph.interfacesLock.Unlock()
		if !known {
			declares = declaresUnexportedInterface(declaring)
			graph.interfacesLock.Lock()
			graph.unexportedInterfaces[declaring] = declares
			graph.interfacesLock.Unlock()
		}
		return declares
	})
}

// sizesForWalk are the build's sizes for a walk, after which they are never set again (measureBy).
func (graph *typeGraph) sizesForWalk() types.Sizes {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	graph.walked = true
	return graph.sizes
}

// packageOf is the part of the graph holding declaring's type names, made the first time it is asked.
func (graph *typeGraph) packageOf(declaring *types.Package) *packageTypeGraph {
	graph.lock.Lock()
	defer graph.lock.Unlock()
	part, known := graph.packages[declaring]
	if !known {
		part = &packageTypeGraph{nodes: map[*types.TypeName]*typeNode{}}
		graph.packages[declaring] = part
	}
	return part
}

// closureDigest is object's closure digest: its own line (ownRendering) and the digests of the components the named
// types its type names belong to, each once, sorted. Its own line and what its type names are read without a lock.
func (graph *typeGraph) closureDigest(object types.Object) cache.Key {
	sizes := graph.sizesForWalk()
	var components []cache.Key
	step := directTypeNames(rootTypes(object), sizes)
	for _, root := range step.typeNames {
		components = append(components, graph.digestOf(root, sizes))
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
	sizes := graph.sizesForWalk()
	pending := directTypeNames(roots, sizes).typeNames
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
		pending = append(pending, graph.edgesOf(current, sizes)...)
	}
	return reached
}

// edgesOf are the type names typeName's node names, under its package's lock.
func (graph *typeGraph) edgesOf(typeName *types.TypeName, sizes types.Sizes) []*types.TypeName {
	part := graph.packageOf(typeName.Pkg())
	part.lock.Lock()
	defer part.lock.Unlock()
	return graph.nodeOf(part, typeName, sizes).edges
}

// nodeOf is typeName's node in part, its package's, rendered and its edges found the first time it is asked. The
// caller holds part's lock.
func (graph *typeGraph) nodeOf(part *packageTypeGraph, typeName *types.TypeName, sizes types.Sizes) *typeNode {
	if node, known := part.nodes[typeName]; known {
		return node
	}
	if graph.rendered != nil {
		graph.rendered(typeName)
	}
	step := directTypeNames(declarationTypes(typeName, graph.importerSees), sizes)
	node := &typeNode{
		typeName: typeName,
		lines:    append(renderedTypeName(typeName, graph.importerSees), step.observed...),
		edges:    step.typeNames,
	}
	part.nodes[typeName] = node
	return node
}

// digestOf is the digest of the component start belongs to. Under its package's lock it renders every node of that
// package start reaches with no digest yet and gathers the type names they point to in other packages (foreignEdges);
// with no lock held it digests those, each under its own package's lock; and under its package's lock again it closes
// start's components by Tarjan's walk of the same nodes, a component digested once every component it points to has
// been, the other packages' from what it gathered. A node another digest closed meanwhile is skipped, since its
// component's digest is final, and so is every node only it reached.
func (graph *typeGraph) digestOf(start *types.TypeName, sizes types.Sizes) cache.Key {
	part := graph.packageOf(start.Pkg())
	part.lock.Lock()
	if node := graph.nodeOf(part, start, sizes); node.digested {
		part.lock.Unlock()
		return node.digest
	}
	foreign := graph.foreignEdges(part, start, sizes)
	part.lock.Unlock()

	foreignDigests := make(map[*types.TypeName]cache.Key, len(foreign))
	for _, target := range foreign {
		foreignDigests[target] = graph.digestOf(target, sizes)
	}

	part.lock.Lock()
	defer part.lock.Unlock()
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
			if target.Pkg() != start.Pkg() {
				continue
			}
			successor := graph.nodeOf(part, target, sizes)
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
		graph.digestComponent(part, members, foreignDigests)
	}
	if node := graph.nodeOf(part, start, sizes); !node.digested {
		connect(node)
	}
	return part.nodes[start].digest
}

// foreignEdges are the type names of other packages that the nodes of part start reaches with no digest yet point to,
// each once, every one of those nodes rendered on the way. The caller holds part's lock.
func (graph *typeGraph) foreignEdges(
	part *packageTypeGraph,
	start *types.TypeName,
	sizes types.Sizes,
) []*types.TypeName {
	seen := map[*types.TypeName]bool{start: true}
	pending := []*types.TypeName{start}
	foreignSeen := map[*types.TypeName]bool{}
	var foreign []*types.TypeName
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		node := graph.nodeOf(part, current, sizes)
		if node.digested {
			continue
		}
		for _, target := range node.edges {
			switch {
			case target.Pkg() != start.Pkg():
				if !foreignSeen[target] {
					foreignSeen[target] = true
					foreign = append(foreign, target)
				}
			case !seen[target]:
				seen[target] = true
				pending = append(pending, target)
			}
		}
	}
	return foreign
}

// digestComponent digests one component of part: each member's path, name and lines, sorted by path, name and lines,
// since two local types of one path and name are told apart by their lines alone, then the digests of the components
// its members point to, which are all digested, each once, sorted: part's own from their nodes, and other packages'
// from foreignDigests. The caller holds part's lock.
//
// Mutates: members each gains the component's digest
func (graph *typeGraph) digestComponent(
	part *packageTypeGraph,
	members []*typeNode,
	foreignDigests map[*types.TypeName]cache.Key,
) {
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
			if successor, own := part.nodes[target]; own {
				if !inComponent[successor] {
					successors = append(successors, successor.digest)
				}
				continue
			}
			digest, gathered := foreignDigests[target]
			if !gathered {
				// Invariant: foreignEdges gathered every type name of another package that a node with no digest
				// points to, and a member had none when it was gathered, since a digest is never taken back.
				panic(fmt.Sprintf("check: the component of %s points to %s, which foreignEdges never gathered",
					member.typeName.Name(), target.Name()))
			}
			successors = append(successors, digest)
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
