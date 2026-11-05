package printing

// AstPath is upstream's AstPath, src/common/ast-path.js: the printer's position in the tree.
//
// Upstream's stack alternates names and values: root value, then name, value, name, value. Here each
// name and the value it reached are one Frame, and the root's frame has no name. A value is a node, a
// list of nodes, a leaf, or missing; a name is a property name, or an index when the value above it is a
// list. That shape is load-bearing. siblings, index, key, next and previous all read it, and a stack that
// dropped the list frames would answer them differently.
//
// S is the language's per-print state, Options.Settings.
type AstPath[N Node[N], S any] struct {
	stack []Frame[N]

	// Settings is the print's Options.Settings, set by PrintAstToDoc, so a printer function given only the
	// path reaches its language's per-print state. Nothing in this package reads it.
	Settings S
}

// Frame is one step of the path: the name it was reached by and the value there, a name and value pair
// of upstream's stack.
type Frame[N Node[N]] struct {
	Name  PathName
	Value Value[N]
}

// PathName is a property name, or an index into the list the frame above holds. The root frame's is the
// zero PathName, as upstream's root has no name.
type PathName struct {
	Property string
	Index    int
	IsIndex  bool
}

// NewAstPath is upstream's `new AstPath(value)`, carrying the print's settings.
func NewAstPath[N Node[N], S any](root N, settings S) *AstPath[N, S] {
	return &AstPath[N, S]{stack: []Frame[N]{{Value: NodeValue(root)}}, Settings: settings}
}

// current is the frame of the current value, upstream's stack.at(-1) and the name before it.
func (path *AstPath[N, S]) current() Frame[N] { return path.stack[len(path.stack)-1] }

// Key is upstream's path.key: the property name holding the current node, or the list holding it.
func (path *AstPath[N, S]) Key() (string, bool) {
	frameIndex := len(path.stack) - 1
	if path.IsInArray() {
		frameIndex--
	}
	// The root frame has no name, where upstream's stack.at reads before the stack's start.
	if frameIndex < 1 || path.stack[frameIndex].Name.IsIndex {
		return "", false
	}
	return path.stack[frameIndex].Name.Property, true
}

// Index is upstream's path.index: the position in the sibling list, when there is one.
func (path *AstPath[N, S]) Index() (int, bool) {
	if !path.IsInArray() || !path.current().Name.IsIndex {
		return 0, false
	}
	return path.current().Name.Index, true
}

// Stack is upstream's path.stack, for the printers that read it directly (the YAML printer's
// isLastDescendantNode walks it). The slice is the path's own: read it, never modify it.
func (path *AstPath[N, S]) Stack() []Frame[N] { return path.stack }

// Value is upstream's path.node and getValue(): the current value, in whichever shape it has.
func (path *AstPath[N, S]) Value() Value[N] { return path.current().Value }

// Node is upstream's path.node as a node, and whether the current value is one.
func (path *AstPath[N, S]) Node() (N, bool) { return path.current().Value.Node() }

// Parent is upstream's path.parent.
func (path *AstPath[N, S]) Parent() (N, bool) { return path.GetNode(1) }

// Grandparent is upstream's path.grandparent.
func (path *AstPath[N, S]) Grandparent() (N, bool) { return path.GetNode(2) }

// IsInArray is upstream's path.isInArray.
func (path *AstPath[N, S]) IsInArray() bool { return path.siblings().IsList() }

// siblings is upstream's path.siblings: the list holding the current value, or missing.
func (path *AstPath[N, S]) siblings() Value[N] {
	if len(path.stack) < 2 || !path.stack[len(path.stack)-2].Value.IsList() {
		return Value[N]{}
	}
	return path.stack[len(path.stack)-2].Value
}

// Siblings is upstream's path.siblings as nodes: the list of nodes holding the current value, or nil.
func (path *AstPath[N, S]) Siblings() []N {
	nodes, _ := path.siblings().Nodes()
	return nodes
}

// Next is upstream's path.next.
func (path *AstPath[N, S]) Next() (N, bool) {
	index, present := path.Index()
	if !path.IsInArray() || !present {
		var zero N
		return zero, false
	}
	return path.siblings().Element(index + 1).Node()
}

// Previous is upstream's path.previous.
func (path *AstPath[N, S]) Previous() (N, bool) {
	index, present := path.Index()
	if !path.IsInArray() || !present {
		var zero N
		return zero, false
	}
	return path.siblings().Element(index - 1).Node()
}

// IsFirst is upstream's path.isFirst.
func (path *AstPath[N, S]) IsFirst() bool {
	index, present := path.Index()
	return present && index == 0
}

// IsLast is upstream's path.isLast.
func (path *AstPath[N, S]) IsLast() bool {
	index, present := path.Index()
	return path.IsInArray() && present && index == path.siblings().Len()-1
}

// IsRoot is upstream's path.isRoot.
func (path *AstPath[N, S]) IsRoot() bool { return len(path.stack) == 1 }

// Root is upstream's path.root.
func (path *AstPath[N, S]) Root() (N, bool) { return path.stack[0].Value.Node() }

// Ancestors is upstream's path.ancestors: every node above the current one, nearest first.
func (path *AstPath[N, S]) Ancestors() []N {
	var ancestors []N
	for frameIndex := len(path.stack) - 2; frameIndex >= 0; frameIndex-- {
		if node, isNode := path.stack[frameIndex].Value.Node(); isNode {
			ancestors = append(ancestors, node)
		}
	}
	return ancestors
}

// GetNode is upstream's getNode(count): the count-th node up from the current one.
func (path *AstPath[N, S]) GetNode(count int) (N, bool) {
	frameIndex := path.nodeFrameIndex(count)
	if frameIndex == -1 {
		var zero N
		return zero, false
	}
	return path.stack[frameIndex].Value.Node()
}

// GetParentNode is upstream's getParentNode(count).
func (path *AstPath[N, S]) GetParentNode(count int) (N, bool) { return path.GetNode(count + 1) }

// nodeFrameIndex is upstream's #getNodeStackIndex: walk the frames up, skipping lists.
func (path *AstPath[N, S]) nodeFrameIndex(count int) int {
	remaining := count
	for frameIndex := len(path.stack) - 1; frameIndex >= 0; frameIndex-- {
		if !path.stack[frameIndex].Value.IsList() {
			remaining--
			if remaining < 0 {
				return frameIndex
			}
		}
	}
	return -1
}

// descend pushes a frame for each property name in turn, upstream's `value?.[name]` from the current
// value, and returns the last value reached.
//
// Mutates: path a frame is pushed for each name, for the caller to cut back
func (path *AstPath[N, S]) descend(names []string) Value[N] {
	value := path.current().Value
	for _, name := range names {
		value = fieldOf(value, name)
		path.stack = append(path.stack, Frame[N]{Name: PathName{Property: name}, Value: value})
	}
	return value
}

// Call is upstream's path.call(callback, ...names): descend through names, run callback, restore.
//
// The stack is restored even if the callback panics, as upstream's finally restores it.
//
// Mutates: path the callback runs with the path descended through names, and its stack is back on return
func Call[N Node[N], S any, R any](path *AstPath[N, S], callback func(*AstPath[N, S]) R, names ...string) R {
	length := len(path.stack)
	path.descend(names)
	defer func() { path.stack = path.stack[:length] }()
	return callback(path)
}

// CallElement is upstream's path.call(callback, ...names, index): Call through names, then into element
// index of the list there. Upstream's names mix property names and indexes; a printer here only ever
// ends a path with an index (a call's argument, a sequence's first expression, a template's first quasi,
// an arrow's first parameter), so that is the one shape this takes.
//
// Mutates: path the callback runs with the path at the element, and its stack is back on return
func CallElement[N Node[N], S any, R any](
	path *AstPath[N, S],
	callback func(*AstPath[N, S]) R,
	index int,
	names ...string,
) R {
	length := len(path.stack)
	list := path.descend(names)
	path.stack = append(path.stack, Frame[N]{Name: PathName{Index: index, IsIndex: true}, Value: list.Element(index)})
	defer func() { path.stack = path.stack[:length] }()
	return callback(path)
}

// CallParent is upstream's path.callParent(callback, count): run callback with the path at an ancestor.
//
// Mutates: path the callback runs with the path cut back to the ancestor, and its stack is back on return
func CallParent[N Node[N], S any, R any](path *AstPath[N, S], callback func(*AstPath[N, S]) R, count int) R {
	frameIndex := path.nodeFrameIndex(count + 1)
	parentFrames := append([]Frame[N](nil), path.stack[frameIndex+1:]...)
	path.stack = path.stack[:frameIndex+1]
	defer func() { path.stack = append(path.stack, parentFrames...) }()
	return callback(path)
}

// Each is upstream's path.each(callback, ...names): visit every element of the list at names.
func (path *AstPath[N, S]) Each(callback func(path *AstPath[N, S], index int), names ...string) {
	length := len(path.stack)
	list := path.descend(names)
	defer func() { path.stack = path.stack[:length] }()

	count := list.Len()
	for index := 0; index < count; index++ {
		element := Frame[N]{Name: PathName{Index: index, IsIndex: true}, Value: list.Element(index)}
		path.stack = append(path.stack, element)
		callback(path, index)
		path.stack = path.stack[:len(path.stack)-1]
	}
}

// Map is upstream's path.map(callback, ...names): Each, collecting results by index.
func Map[N Node[N], S any, R any](
	path *AstPath[N, S],
	callback func(path *AstPath[N, S], index int) R,
	names ...string,
) []R {
	var results []R
	path.Each(func(path *AstPath[N, S], index int) {
		for len(results) <= index {
			var zero R
			results = append(results, zero)
		}
		results[index] = callback(path, index)
	}, names...)
	return results
}

// Predicate is one step of Match: the value at that depth, the name under it that leads back down (""
// for the current value, which upstream gives null), and the index when that leads through a list.
type Predicate[N Node[N]] func(value Value[N], name string, index int, hasIndex bool) bool

// Match is upstream's path.match(...predicates): test the current node and its ancestors in turn. A nil
// predicate matches anything at its depth, as upstream's undefined does.
func (path *AstPath[N, S]) Match(predicates ...Predicate[N]) bool {
	// frameIndex is the frame node came from. Upstream pops a name and then a value off its stack; one
	// frame up is that pair, the name of frameIndex and the value above it.
	frameIndex := len(path.stack) - 1
	var name PathName
	node := path.stack[frameIndex].Value

	up := func() {
		if frameIndex < 1 {
			frameIndex, name, node = -1, PathName{}, Value[N]{}
			return
		}
		name, node = path.stack[frameIndex].Name, path.stack[frameIndex-1].Value
		frameIndex--
	}

	for _, predicate := range predicates {
		if node.isMissing() {
			return false
		}
		index, hasIndex := 0, false
		if name.IsIndex {
			index, hasIndex = name.Index, true
			up()
		}
		if predicate != nil && !predicate(node, name.Property, index, hasIndex) {
			return false
		}
		up()
	}
	return true
}

// FindAncestor is upstream's path.findAncestor(predicate). It walks the stack as Ancestors does, nearest
// first, without building the list: markdown asks it of every node it prints (#vbjv3d6).
func (path *AstPath[N, S]) FindAncestor(predicate func(N) bool) (N, bool) {
	for frameIndex := len(path.stack) - 2; frameIndex >= 0; frameIndex-- {
		if node, isNode := path.stack[frameIndex].Value.Node(); isNode && predicate(node) {
			return node, true
		}
	}
	var zero N
	return zero, false
}

// HasAncestor is upstream's path.hasAncestor(predicate).
func (path *AstPath[N, S]) HasAncestor(predicate func(N) bool) bool {
	_, found := path.FindAncestor(predicate)
	return found
}
