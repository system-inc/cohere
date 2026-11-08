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
	// directives is every //lint: directive in the package, which staticcheck's directives analyzer read.
	directives []lint.Directive
	// generated is each generated file of the package, by its name, which staticcheck's generated analyzer read.
	generated map[string]generated.Generator

	// edges tracks all edges between nodes (uses and owns relationships). This data is also present in the node
	// struct, but there it can't be accessed in constant time. edges is used to deduplicate edges.
	edges map[edge]struct{}
	// nodes is every node, by its index, the root first.
	nodes []node
	// objects is each object's node.
	objects map[types.Object]nodeIndex

	// namedTypes is every named type the package declares, a function's own among them, but no alias. The original
	// calls them package-level, and its walk collects the local ones too.
	namedTypes []*types.TypeName
	// interfaceTypes is every interface type the walk read that declares at least one method or embeds a type.
	interfaceTypes []*types.Interface
}

// nodeIndex is a node's place in graph.nodes, the original's NodeID. The root is 0.
type nodeIndex uint64

// node is one object in the graph, and the nodes it uses and owns. The original keeps its own index on it too, for
// the merge and the Graphviz output; the port leaves both out, and a node's index is its place in graph.nodes.
type node struct {
	// object is what a finding says of the object the node stands for. The root's is empty.
	object object

	// uses is each node this one uses, in the order the walk found them. The original keeps slices rather than maps
	// to merge graphs by rewriting indexes in place; the port keeps them for the same order.
	uses []nodeIndex
	// owns is each node this one owns, which is quiet when this one goes unused.
	owns []nodeIndex
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

// edgeKind is whether an edge is a use or an ownership.
type edgeKind uint8

const (
	edgeKindUse edgeKind = iota + 1
	edgeKindOwn
)

// edge is one use or ownership, from one node to another, as graph.edges deduplicates them.
type edge struct {
	from nodeIndex
	to   nodeIndex
	kind edgeKind
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
	return &graph{
		typesPackage: typesPackage,
		typesInfo:    typesInfo,
		files:        files,
		directives:   directives,
		generated:    generatedFiles,
		fileSet:      fileSet,
		nodes:        []node{{}},
		edges:        map[edge]struct{}{},
		objects:      map[types.Object]nodeIndex{},
	}
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
	if index, found := graph.objects[generic]; found {
		return index
	}
	index := nodeIndex(len(graph.nodes))
	graph.nodes = append(graph.nodes, node{object: graph.objectOf(generic)})
	graph.objects[generic] = index
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

// addEdge records added and is whether it is new.
func (graph *graph) addEdge(added edge) bool {
	if _, found := graph.edges[added]; found {
		return false
	}
	graph.edges[added] = struct{}{}
	return true
}

// addOwned records that owner owns owned.
func (graph *graph) addOwned(owner nodeIndex, owned nodeIndex) {
	if !graph.addEdge(edge{owner, owned, edgeKindOwn}) {
		return
	}
	graph.nodes[owner].owns = append(graph.nodes[owner].owns, owned)
}

// addUse records that by uses used.
func (graph *graph) addUse(by nodeIndex, used nodeIndex) {
	if !graph.addEdge(edge{by, used, edgeKindUse}) {
		return
	}
	graph.nodes[by].uses = append(graph.nodes[by].uses, used)
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
	graph.addUse(graph.node(by), usedNode)
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
		for _, used := range graph.nodes[root].uses {
			color(used)
		}
	}
	color(0)

	var quieten func(index nodeIndex)
	quieten = func(index nodeIndex) {
		states[index] |= nodeStateQuiet
		for _, owned := range graph.nodes[index].owns {
			quieten(owned)
		}
	}

	for index, each := range graph.nodes {
		if states[index].seen() {
			continue
		}
		for _, owned := range each.owns {
			quieten(owned)
		}
	}

	return states
}

// results is the graph's verdict on every node but the root, in the order of the nodes.
func (graph *graph) results() result {
	states := graph.colorAndQuieten()

	var verdict result
	for index, each := range graph.nodes[1:] {
		state := states[index+1]
		if state.seen() {
			verdict.used = append(verdict.used, each.object)
		} else if state.quiet() {
			verdict.quiet = append(verdict.quiet, each.object)
		} else {
			verdict.unused = append(verdict.unused, each.object)
		}
	}
	return verdict
}
