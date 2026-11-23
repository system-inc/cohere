package check

import (
	"cmp"
	"fmt"
	"go/types"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

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
//
// A build's graph takes the lines of a node of a package it restored from the host's run from the host's graph
// (hostLines), which the host renders once for its own verdicts and every build's, and never renders them itself. The
// packages it takes them for are those whose restore is final in the load (CacheSession.restoredBelow): each whose own
// verdict held, which the load decides before any package above it, or which the walk left untouched before the load
// began. Such a package's types render as the host's: read from the host's export data, and every type its
// declarations use whole read alike in the build, by its verdict or by the walk's induction. That is what pins a
// defined type declared over another package's, `type T q.X`, which has no edge to q.X and takes, read from export
// data, the build's q.X struct: its own reads use q.X whole (build_reads' roleDeclared), so a build whose q.X differs
// declines the package and checks it from source. Only the lines renderedTypeName writes are taken: the node's observed
// lines are a property of another package's type, its edges are the build's, and so is its digest, whose successors
// may be packages the build checked from source. An alias is never taken: export data names the type it stands for,
// which the build resolves to its own object of that path, and an alias's chain through other packages is pinned by
// no read of the package's. Taking the host's lines holds the build's part lock while the host's graph takes its own,
// which never waits on a build's graph, so the order is always the build's first and no deadlock can come of it.
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

	// restoredFrom are the packages of the build restored from the host's run, a hostPackage by the build's
	// *types.Package, learned from the verdicts (learnRestored), whose nodes take the host's lines (hostLines).
	// reusedNodes, renderedNodes and unmatchedNodes count the nodes that took the host's lines, those rendered here,
	// and those of a restored package the host's graph had no node of the same path and name for, rendered here too.
	restoredFrom   sync.Map
	reusedNodes    atomic.Int64
	renderedNodes  atomic.Int64
	unmatchedNodes atomic.Int64
}

// hostPackage is the host's package a package of the build was restored from, nil when the host kept none, and the
// host's graph, which renders its nodes.
type hostPackage struct {
	graph *typeGraph
	types *types.Package
}

// packageTypeGraph is one package's type names in a graph, by type name, each a node, under the package's own lock.
type packageTypeGraph struct {
	lock  sync.Mutex
	nodes map[*types.TypeName]*typeNode
}

// typeNode is one type name of the graph: its rendered lines, the first ownLineCount of them renderedTypeName's and the
// rest its observed lines, the type names they name, the types of the fields its lines render by their properties
// (closureStep.observedTypes), and its component's digest once it is known. Its edges and observed types are what its
// rendering visited, which a dependence set follows (dependenceReach), set once as the node is made and never changed
// after.
type typeNode struct {
	typeName      *types.TypeName
	lines         []string
	ownLineCount  int
	edges         []*types.TypeName
	observedTypes []types.Type
	digest        cache.Key
	digested      bool
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

// measuredSizes are the build's sizes the graph measures by (measureBy), nil when it renders every field whole. It
// marks no walk, so asking never keeps the sizes from being set.
func (graph *typeGraph) measuredSizes() types.Sizes {
	graph.lock.Lock()
	defer graph.lock.Unlock()
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
	own := graph.ownLines(typeName)
	node := &typeNode{
		typeName: typeName,
		// A new slice: the host's lines are its node's, whose observed lines follow them in the same array.
		lines:         slices.Concat(own, step.observed),
		ownLineCount:  len(own),
		edges:         step.typeNames,
		observedTypes: step.observedTypes,
	}
	part.nodes[typeName] = node
	return node
}

// ownLines are typeName's lines as renderedTypeName writes them: the host's node's (hostLines), for a type name of a
// package the build restored from the host's run, and otherwise rendered here, each node counted by which.
func (graph *typeGraph) ownLines(typeName *types.TypeName) []string {
	if hostLines, reused := graph.hostLines(typeName); reused {
		graph.reusedNodes.Add(1)
		return hostLines
	}
	graph.renderedNodes.Add(1)
	return renderedTypeName(typeName, graph.importerSees)
}

// hostLines are the lines renderedTypeName writes of the host's type name of typeName's path and name, rendered by the
// host's graph under its own package's lock, once for every build, and whether there are any to take: none for an
// alias (see typeGraph), for a package the build did not restore from the host's run, nor where the host has no
// package-level type name of the path and name, which is counted.
func (graph *typeGraph) hostLines(typeName *types.TypeName) ([]string, bool) {
	if typeName.IsAlias() {
		return nil, false
	}
	learned, restored := graph.restoredFrom.Load(typeName.Pkg())
	if !restored {
		return nil, false
	}
	from, _ := learned.(hostPackage)
	// A local type is no package-level type name of the host's, though one of its name may be.
	if from.types == nil || typeName.Parent() != typeName.Pkg().Scope() {
		graph.unmatchedNodes.Add(1)
		return nil, false
	}
	hostTypeName, isTypeName := from.types.Scope().Lookup(typeName.Name()).(*types.TypeName)
	if !isTypeName || hostTypeName.IsAlias() || hostTypeName.Pkg().Path() != typeName.Pkg().Path() {
		graph.unmatchedNodes.Add(1)
		return nil, false
	}
	node := from.graph.renderedNode(hostTypeName, from.graph.sizesForWalk())
	return node.lines[:node.ownLineCount], true
}

// learnRestored has the graph take the lines of each package of restored, a package of the build restored from the
// host's run by the host's package it was restored from, from host, the host's graph (hostLines), the first time it
// learns of the package. Restored is a property of the package's types, read from the host's export data, which the
// load never changes, so every verdict that names a package tells the same. A graph that is its own host learns
// nothing, since its lock would be taken twice.
func (graph *typeGraph) learnRestored(host *typeGraph, restored map[*types.Package]*types.Package) {
	if host == graph {
		return
	}
	for built, hostTypes := range restored {
		if _, known := graph.restoredFrom.Load(built); !known {
			graph.restoredFrom.LoadOrStore(built, hostPackage{graph: host, types: hostTypes})
		}
	}
}

// nodeCounts are how many nodes took the host's lines, how many were rendered here, and how many of those were of a
// restored package the host had no node of the same path and name for.
func (graph *typeGraph) nodeCounts() (int, int, int) {
	return int(graph.reusedNodes.Load()), int(graph.renderedNodes.Load()), int(graph.unmatchedNodes.Load())
}

// renderedNode is typeName's node, rendered under its package's lock the first time it is asked (nodeOf). Its edges and
// observed types are read after the lock is let go, which is safe since they are set before the node is stored and
// never changed.
func (graph *typeGraph) renderedNode(typeName *types.TypeName, sizes types.Sizes) *typeNode {
	part := graph.packageOf(typeName.Pkg())
	part.lock.Lock()
	defer part.lock.Unlock()
	return graph.nodeOf(part, typeName, sizes)
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
