// Package unused_code is staticcheck's U1000, code nothing uses: a function, a type, a field, a constant or a
// variable that no exported name, entry point or used object reaches.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 unused (unused.go, implements.go and runtime.go, with
// upstream's fa9ae6d1a5 in unused.go), whose logic it keeps, whose numbered rules are its specification, in the
// overview below and beside the code each one governs, and whose finding it words as cohere's registry worded the
// original's. Each package builds a graph of the objects it declares, whose edges say which object uses which and
// which owns which, colors it from a root the package's entry points hang from, and reports each object the coloring
// never reached, but those quieted because the object owning them went unreached too.
//
// Where the translation changes shape, the code says why. The original builds its graph for staticcheck's runner,
// which merges graphs across a package's variants and across packages. cohere reports one variant at a time and never
// merges, so the port leaves out the merge and the serialized graph it works on, the Graphviz output, Graph and the
// Debug writer, and an object carries no object path, which only the merge reads and whose encoder was most of
// U1000's cost. The original's Options are left out too: staticcheck's runner and cohere's registry only ever run
// DefaultOptions, so each option is folded in where it was read, and code only another setting reaches is gone. Where
// the original asserts a type in the single-value form, which panics on a miss, the port checks it and passes that
// node over should it ever not hold; on type-checked code each one holds.
//
// The registry runs the port as U1000 in the original's place (#7hdcn3w), and the original stays the oracle: the tests
// beside this file hold the port to the original's own expectations, over a copy of the original's testdata, and run
// the original, reported as the registry reported it (originalReport), and the port over the same packages.
package unused_code

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"math"
	"unsafe"

	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/lint"
)

// The original's overview of the rules, kept as it wrote them, true of the default options the port always runs:
//
// - packages use:
//   - (1.1) exported named types
//   - (1.2) exported functions (but not methods!)
//   - (1.3) exported variables
//   - (1.4) exported constants
//   - (1.5) init functions
//   - (1.6) functions exported to cgo
//   - (1.7) the main function iff in the main package
//   - (1.8) symbols linked via go:linkname
//   - (1.9) objects in generated files
//
// - named types use:
//   - (2.1) exported methods
//   - (2.2) the type they're based on
//   - (2.5) all their type parameters. Unused type parameters are probably useless, but they're a brand new feature
//     and we don't want to introduce false positives because we couldn't anticipate some novel use-case.
//   - (2.6) all their type arguments
//
// - functions use:
//   - (4.1) all their arguments, return parameters and receivers
//   - (4.2) anonymous functions defined beneath them
//   - (4.3) closures and bound methods.
//     this implements a simplified model where a function is used merely by being referenced, even if it is never
//     called. that way we don't have to keep track of closures escaping functions.
//   - (4.4) functions they return. we assume that someone else will call the returned function
//   - (4.5) functions/interface methods they call
//   - (4.6) types they instantiate or convert to
//   - (4.7) fields they access
//   - (4.9) package-level variables they assign to iff in tests (sinks for benchmarks)
//   - (4.10) all their type parameters. See 2.5 for reasoning.
//   - (4.11) local variables
//   - Note that the majority of this is handled implicitly by seeing idents be used. In particular, unlike the old
//     IR-based implementation, the AST-based one doesn't care about closures, bound methods or anonymous functions.
//     They're all just additional nodes in the AST.
//
// - conversions use:
//   - (5.1) when converting between two equivalent structs, the fields in either struct use each other. the fields
//     are relevant for the conversion, but only if the fields are also accessed outside the conversion.
//   - (5.2) when converting to or from unsafe.Pointer, mark all fields as used.
//
// - structs use:
//   - (6.1) fields of type NoCopy sentinel
//   - (6.2) exported fields
//   - (6.3) embedded fields that help implement interfaces (either fully implements it, or contributes required
//     methods) (recursively)
//   - (6.4) embedded fields that have exported methods (recursively)
//   - (6.5) embedded structs that have exported fields (recursively)
//   - (6.6) all fields if they have a structs.HostLayout field
//
// - (7.1) field accesses use fields
// - (7.2) fields use their types
//
// - (8.0) How we handle interfaces:
//   - (8.1) We do not technically care about interfaces that only consist of exported methods. Exported methods on
//     concrete types are always marked as used.
//   - (8.2) Any concrete type implements all known interfaces. Even if it isn't assigned to any interfaces in our
//     code, the user may receive a value of the type and expect to pass it back to us through an interface.
//
//     Concrete types use their methods that implement interfaces. If the type is used, it uses those methods.
//     Otherwise, it doesn't. This way, types aren't incorrectly marked reachable through the edge from method to
//     type.
//
//   - (8.3) All interface methods are marked as used, even if they never get called. This is to accommodate sum types
//     (unexported interface method that must exist but never gets called.)
//
//   - (8.4) All embedded interfaces are marked as used. This is an extension of 8.3, but we have to explicitly track
//     embedded interfaces because in a chain C->B->A, B wouldn't be marked as used by 8.3 just because it contributes
//     A's methods to C.
//
// - Inherent uses:
//   - (9.2) variables use their types
//   - (9.3) types use their underlying and element types
//   - (9.4) conversions use the type they convert to
//   - (9.7) variable _reads_ use variables, writes do not, except in tests
//   - (9.8) runtime functions that may be called from user code via the compiler
//   - (9.9) objects named the blank identifier are used. They cannot be referred to and are usually used explicitly
//     to use something that would otherwise be unused.
//   - The majority of idents get marked as read by virtue of being in the AST.
//
// - const groups:
//   - (10.1) if one constant out of a block of constants is used, mark all of them used. a lot of the time, unused
//     constants exist for the sake of completeness. See also https://github.com/dominikh/go-tools/issues/365
//
//     Do not, however, include constants named _ in constant groups.
//
// - (11.1) anonymous struct types use all their fields. we cannot deduplicate struct types, as that leads to
//   order-dependent reports. we can't not deduplicate struct types while still tracking fields, because then each
//   instance of the unnamed type in the data flow chain will get its own fields, causing false positives. Thus, we only
//   accurately track fields of named struct types, and assume that unnamed struct types use all their fields.
//
// - type parameters use:
//   - (12.1) their constraint type

// graph is one package's use graph: a node for each object the package declares that the walk sees or uses, and the
// edges between them.
type graph struct {
	// typesPackage is the package the graph is of. An object of another package is never a node.
	typesPackage *types.Package
	// typesInfo is the package's type information, which resolves the syntax to its objects.
	typesInfo *types.Info
	// files is the package's syntax.
	files []*ast.File
	// fileSet holds the positions of the package's syntax and objects.
	fileSet *token.FileSet
	// directives is every //lint: directive in the package, which directivesAnalyzer read.
	directives []lint.Directive
	// generated is each generated file of the package, by its name, which staticcheck's generated analyzer read.
	generated map[string]generated.Generator

	// usedEdges and ownedEdges are every use and every ownership between nodes, each keyed by edgeKey. This data is
	// also present in the nodes' chains, but there it can't be accessed in constant time; these deduplicate edges. The
	// original keeps one map keyed by a struct of both nodes and the kind; a map per kind keyed by one word keeps a
	// third of the bytes per key, hashes it on the runtime's path for 64-bit keys, and holds the same edges.
	usedEdges  map[uint64]struct{}
	ownedEdges map[uint64]struct{}
	// nodes is every node, by its index, the root first.
	nodes []node
	// uses and owns are every use and every ownership as a link of its node's chain, which node.firstUse and
	// node.firstOwned start. Link 0 is none, so each list starts with one link that no chain reaches. The original
	// keeps a slice of each kind on every node; one list a kind is two lists to make for the whole graph, not two
	// for each node and again each time one grows.
	uses []edgeLink
	owns []edgeLink
	// objects is each object's node, keyed by objectKey, the object's one pointer word, which hashes on the runtime's
	// path for 64-bit keys. Keyed by the object itself, a key is two words, its type and its pointer, and every
	// lookup hashes and compares both through the interface.
	objects map[unsafe.Pointer]nodeIndex
	// lastBy and lastByNode are the user use last asked node for and its node. A walk passes one user to every use
	// in a body, so use asks node only when the user changes. The zero value is the root's own pair, as node(nil) is
	// 0.
	lastBy     types.Object
	lastByNode nodeIndex

	// namedTypes is every named type the package declares, a function's own among them, but no alias. The original
	// calls them package-level, and its walk collects the local ones too.
	namedTypes []*types.TypeName
	// interfaceTypes is every interface type the walk read that declares at least one method or embeds a type.
	interfaceTypes []*types.Interface
}

// nodeIndex is a node's place in graph.nodes, the original's NodeID. The root is 0. The original's is 64 bits; 32 are
// room for more objects than any package declares, and node stops a walk that would number past them.
type nodeIndex uint32

// linkIndex is a link's place in graph.uses or graph.owns, 0 for none.
type linkIndex uint32

// edgeLink is one edge in its node's chain: the node the edge leads to, and the node's next link of the same kind.
type edgeLink struct {
	to   nodeIndex
	next linkIndex
}

// node is one object in the graph, and where its uses and ownerships start. The original keeps its own index on it
// too, for the merge and the Graphviz output; the port leaves both out, and a node's index is its place in
// graph.nodes.
type node struct {
	// typesObject is the object the node stands for, the generic one of an instance. The root's is nil. The original
	// words what a finding says of it, its object, when it makes the node; the port words it in results, from this same
	// object, so a node is 24 bytes rather than 104 while the walk appends and copies the list.
	typesObject types.Object

	// firstUse is the first link of this node's chain in graph.uses, each node this one uses, the latest the walk
	// found first. The original keeps them in the order found, which only its merge reads: coloring reaches the same
	// nodes along a chain in any order, so the port keeps the order that needs one link a node.
	firstUse linkIndex
	// firstOwned is the first link of this node's chain in graph.owns, each node this one owns, which is quiet when
	// this one goes unused.
	firstOwned linkIndex
}

// object is what a finding says of an object: its kind, its name and its position. The original's also carries an
// object path, which only its merge reads, and a display position, which only staticcheck's runner prints; cohere
// reports at the object's own position, as the registry did the original's.
type object struct {
	// name is the object's name, a method's qualified by its receiver: `(*T).m` or `T.m`.
	name string
	// shortName is the object's bare name, which the original's own tests match their expectations by.
	shortName string
	// kind is the word a finding names the object's kind with: func, var, field, const, type, type param.
	kind string
	// position is where the object is declared.
	position token.Pos
}

// result is the graph's verdict on each object it holds: used, unused and reported, or quiet, unused but not reported
// because what owns it is unused too. Each is in the order of the graph's nodes.
type result struct {
	used   []object
	unused []object
	quiet  []object
}

// edgeKey is the edge from one node to another as one word, from in the high half and to in the low, which
// graph.usedEdges and graph.ownedEdges are keyed by. It is exact, two edges sharing a key only when they share both
// nodes, because a node's index is 32 bits.
func edgeKey(from nodeIndex, to nodeIndex) uint64 {
	return uint64(from)<<32 | uint64(to)
}

// newGraph is an empty graph of one package: the root and no edges.
func newGraph(
	fileSet *token.FileSet,
	files []*ast.File,
	typesPackage *types.Package,
	typesInfo *types.Info,
	directives []lint.Directive,
	generatedFiles map[string]generated.Generator,
) *graph {
	// Almost every node is an object a declaration in the package names, each in typesInfo.Defs. Over cohere's own
	// packages the nodes and the ownerships come to a little under one per declaration, and the uses to about one and
	// a half, under two in nearly every package. Sized so up front, the node list and the maps seldom grow while the
	// walk fills them. The lists of links grow by append, where room left over is a copy, not a rehash, so they take
	// what nine packages in ten need: the uses one and a half a declaration, the ownerships one. A size is only room:
	// it changes no node, no order and no edge, so no finding.
	declared := len(typesInfo.Defs)
	return &graph{
		typesPackage: typesPackage,
		typesInfo:    typesInfo,
		files:        files,
		directives:   directives,
		generated:    generatedFiles,
		fileSet:      fileSet,
		nodes:        make([]node, 1, declared+1),
		uses:         make([]edgeLink, 1, declared+declared/2+1),
		owns:         make([]edgeLink, 1, declared+1),
		usedEdges:    make(map[uint64]struct{}, 2*declared),
		ownedEdges:   make(map[uint64]struct{}, declared),
		objects:      make(map[unsafe.Pointer]nodeIndex, declared),
	}
}

// objectKey is typesObject's pointer, which graph.objects is keyed by. Two objects share it only when they are the
// same object: every object is a pointer to a struct of its own, and the map's key holds it alive, so no other can
// come to share its address while the graph lives. types.Object's unexported methods close it to these eight types.
func objectKey(typesObject types.Object) unsafe.Pointer {
	switch typesObject := typesObject.(type) {
	case *types.Var:
		return pointerOf(typesObject)
	case *types.Func:
		return pointerOf(typesObject)
	case *types.TypeName:
		return pointerOf(typesObject)
	case *types.Const:
		return pointerOf(typesObject)
	case *types.Label:
		return pointerOf(typesObject)
	case *types.PkgName:
		return pointerOf(typesObject)
	case *types.Builtin:
		return pointerOf(typesObject)
	case *types.Nil:
		return pointerOf(typesObject)
	default:
		// Invariant: go/types implements types.Object with these eight types alone, its unexported methods keeping
		// out any other.
		panic(fmt.Sprintf("an object of type %T", typesObject))
	}
}

// pointerOf is object's address, as a map key that holds object alive.
func pointerOf[Object any](object *Object) unsafe.Pointer {
	// Unsafe: the pointer is only compared and hashed as a map key, never dereferenced, cast to another type or moved
	// by arithmetic, and as an unsafe.Pointer the garbage collector still traces it, so the object it names lives as
	// long as the key does and no other object can take its address.
	return unsafe.Pointer(object)
}

// objectOf is what a finding says of typesObject.
func (graph *graph) objectOf(typesObject types.Object) object {
	name := typesObject.Name()
	if signature, isSignature := typesObject.Type().(*types.Signature); isSignature && signature.Recv() != nil {
		switch types.Unalias(signature.Recv().Type()).(type) {
		case *types.Named, *types.Pointer:
			receiver := types.TypeString(signature.Recv().Type(), func(*types.Package) string { return "" })
			if len(receiver) > 0 && receiver[0] == '*' {
				name = fmt.Sprintf("(%s).%s", receiver, typesObject.Name())
			} else if len(receiver) > 0 {
				name = fmt.Sprintf("%s.%s", receiver, typesObject.Name())
			}
		}
	}
	return object{
		name:      name,
		shortName: typesObject.Name(),
		kind:      kindOf(typesObject),
		position:  typesObject.Pos(),
	}
}

// kindOf is the word a finding names typesObject's kind with, the original's typString.
func kindOf(typesObject types.Object) string {
	switch typesObject := typesObject.(type) {
	case *types.Func:
		return "func"
	case *types.Var:
		if typesObject.IsField() {
			return "field"
		}
		return "var"
	case *types.Const:
		return "const"
	case *types.TypeName:
		if _, isParameter := typesObject.Type().(*types.TypeParam); isParameter {
			return "type param"
		}
		return "type"
	default:
		return "identifier"
	}
}

// node is typesObject's node, made the first time it is asked for, and the root for no object. The original makes a
// new node in a function of its own that panics if the object already has one; node is its one caller and asks
// first, so the port makes it here.
func (graph *graph) node(typesObject types.Object) nodeIndex {
	if typesObject == nil {
		return 0
	}
	generic := origin(typesObject)
	key := objectKey(generic)
	if index, found := graph.objects[key]; found {
		return index
	}
	if uint64(len(graph.nodes)) > math.MaxUint32 {
		// Invariant: a node's index is 32 bits, which a package outgrows only by declaring four billion objects, more
		// than any machine could type-check. Were it ever to, two nodes would share an index, so the walk stops rather
		// than merge them.
		panic(fmt.Sprintf("a package of more than %d objects", uint64(math.MaxUint32)))
	}
	index := nodeIndex(len(graph.nodes))
	graph.nodes = append(graph.nodes, node{typesObject: generic})
	graph.objects[key] = index
	return index
}

// origin is the generic object an instantiated variable or function was made from, or typesObject itself.
func origin(typesObject types.Object) types.Object {
	switch typesObject := typesObject.(type) {
	case *types.Var:
		return typesObject.Origin()
	case *types.Func:
		return typesObject.Origin()
	default:
		return typesObject
	}
}

// addOwned records that owner owns owned. Like addUse, it stores the edge's key and reads whether the map grew, one
// hash where a lookup and then a store would be two.
func (graph *graph) addOwned(owner nodeIndex, owned nodeIndex) {
	before := len(graph.ownedEdges)
	graph.ownedEdges[edgeKey(owner, owned)] = struct{}{}
	if len(graph.ownedEdges) == before {
		return
	}
	graph.owns = append(graph.owns, edgeLink{to: owned, next: graph.nodes[owner].firstOwned})
	graph.nodes[owner].firstOwned = newestLink(graph.owns)
}

// addUse records that by uses used.
func (graph *graph) addUse(by nodeIndex, used nodeIndex) {
	before := len(graph.usedEdges)
	graph.usedEdges[edgeKey(by, used)] = struct{}{}
	if len(graph.usedEdges) == before {
		return
	}
	graph.uses = append(graph.uses, edgeLink{to: used, next: graph.nodes[by].firstUse})
	graph.nodes[by].firstUse = newestLink(graph.uses)
}

// newestLink is the index of links' last link, the one just appended.
func newestLink(links []edgeLink) linkIndex {
	if uint64(len(links)) > math.MaxUint32 {
		// Invariant: a link's index is 32 bits, and a package has a new edge only for a pair of its objects, so it
		// outgrows them only with billions of objects, more than any machine could type-check. Were it ever to, a
		// chain would lead to the wrong link, so the walk stops rather than follow one.
		panic(fmt.Sprintf("a package of more than %d edges of one kind", uint64(math.MaxUint32)))
	}
	return linkIndex(len(links) - 1)
}

// see makes seen a node, owned by owner when there is one, unless it is another package's.
func (graph *graph) see(seen types.Object, owner types.Object) {
	if seen == nil {
		// Invariant: the walk sees only an object it resolved from a declaration or a scope, never a missing one.
		panic("saw nil object")
	}

	// DefaultOptions' ExportedIsUsed: an object of another package is never a node.
	if seen.Pkg() != graph.typesPackage || seen.Pkg() == nil {
		return
	}

	seenNode := graph.node(seen)
	if owner != nil {
		graph.addOwned(graph.node(owner), seenNode)
	}
}

// isIrrelevant is whether typesObject is never a node: an imported package's name.
func isIrrelevant(typesObject types.Object) bool {
	_, isPackageName := typesObject.(*types.PkgName)
	return isPackageName
}

// use records that by uses used, the root when by is nil. A use of or by another package's object is left out:
// DefaultOptions' ExportedIsUsed counts every exported object used, so no use across packages is needed.
func (graph *graph) use(used types.Object, by types.Object) {
	if used.Pkg() != graph.typesPackage || used.Pkg() == nil {
		return
	}
	if by != nil && by.Pkg() != graph.typesPackage {
		return
	}

	if isIrrelevant(used) {
		return
	}

	usedNode := graph.node(used)
	// The user's node is node(by), remembered from the last use by the same user. node gives an object the same index
	// every time it is asked: the first ask makes the node, here or elsewhere, and no later ask changes it, the list or
	// the map, so a remembered index is the one asking again would give, and the nodes are made in the same order.
	if by != graph.lastBy {
		graph.lastBy = by
		graph.lastByNode = graph.node(by)
	}
	graph.addUse(graph.lastByNode, usedNode)
}

// colorAndQuieten is each node's state: seen from the root, and quiet when an unseen node owns it. The original colors
// the serialized graph, the one its runner merges; the port colors the graph it built, as it never merges. The
// original's color is a method writing into the states its caller passes; here it is a closure over the states this
// function makes, as quieten already is in the original.
func (graph *graph) colorAndQuieten() []nodeState {
	states := make([]nodeState, len(graph.nodes))

	// color marks root seen, and everything it uses.
	var color func(root nodeIndex)
	color = func(root nodeIndex) {
		if states[root].seen() {
			return
		}
		states[root] |= nodeStateSeen
		for link := graph.nodes[root].firstUse; link != 0; link = graph.uses[link].next {
			color(graph.uses[link].to)
		}
	}
	color(0)

	var quieten func(index nodeIndex)
	quieten = func(index nodeIndex) {
		states[index] |= nodeStateQuiet
		for link := graph.nodes[index].firstOwned; link != 0; link = graph.owns[link].next {
			quieten(graph.owns[link].to)
		}
	}

	for index, each := range graph.nodes {
		if states[index].seen() {
			continue
		}
		for link := each.firstOwned; link != 0; link = graph.owns[link].next {
			quieten(graph.owns[link].to)
		}
	}

	return states
}

// results is the graph's verdict on every node but the root, in the order of the nodes, each node's object worded
// here from the object the node stands for, as the original words it when it makes the node.
func (graph *graph) results() result {
	states := graph.colorAndQuieten()

	// Each list is counted first and made once at its size, which changes neither what it holds nor its order.
	usedCount, quietCount := 0, 0
	for _, state := range states[1:] {
		if state.seen() {
			usedCount++
		} else if state.quiet() {
			quietCount++
		}
	}
	verdict := result{
		used:   make([]object, 0, usedCount),
		unused: make([]object, 0, len(states)-1-usedCount-quietCount),
		quiet:  make([]object, 0, quietCount),
	}
	for index, each := range graph.nodes[1:] {
		state := states[index+1]
		if state.seen() {
			verdict.used = append(verdict.used, graph.objectOf(each.typesObject))
		} else if state.quiet() {
			verdict.quiet = append(verdict.quiet, graph.objectOf(each.typesObject))
		} else {
			verdict.unused = append(verdict.unused, graph.objectOf(each.typesObject))
		}
	}
	return verdict
}
