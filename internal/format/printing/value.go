package printing

// valueKind is which of its shapes a Value holds.
type valueKind uint8

const (
	missingValue valueKind = iota
	nodeValue
	listValue
	mixedListValue
	leafValue
)

// Value is a value on the path, upstream's `node[name]`, in the shapes the print core meets: missing
// (undefined or null), a node, a list of nodes, or a leaf. The one leaf a printer reads back is css's
// url() argument the value parser cannot handle, a string among a paren group's groups, which is also
// why a list may be mixed, nodes and leaves together. The zero Value is missing.
//
// It is a struct and not an interface, so a Field read boxes nothing. N is only comparable here, not a
// Node, because Node's own Field returns a Value.
type Value[N comparable] struct {
	kind   valueKind
	node   N
	nodes  []N
	values []Value[N]
	leaf   string
}

// NodeValue is a node. A nil node is still a node to Node, and nothing to IsNil, as a typed nil pointer
// was to the stack of interfaces this replaced.
func NodeValue[N comparable](node N) Value[N] { return Value[N]{kind: nodeValue, node: node} }

// ListValue is a list of nodes. A nil list is a list to IsList, and nothing to IsNil.
func ListValue[N comparable](nodes []N) Value[N] { return Value[N]{kind: listValue, nodes: nodes} }

// MixedListValue is a list of nodes and leaves, css's groups with a string among them.
func MixedListValue[N comparable](values []Value[N]) Value[N] {
	return Value[N]{kind: mixedListValue, values: values}
}

// LeafValue is a string a printer reads back from the path.
func LeafValue[N comparable](text string) Value[N] { return Value[N]{kind: leafValue, leaf: text} }

// Node is the value as a node, and whether it is one, a nil node included.
func (value Value[N]) Node() (N, bool) { return value.node, value.kind == nodeValue }

// Nodes is the value as a list of nodes, and whether it is one. A mixed list is not.
func (value Value[N]) Nodes() ([]N, bool) { return value.nodes, value.kind == listValue }

// Leaf is the value as a leaf's text, and whether it is one.
func (value Value[N]) Leaf() (string, bool) { return value.leaf, value.kind == leafValue }

// IsList is upstream's Array.isArray on the value, a mixed list and a nil list included.
func (value Value[N]) IsList() bool { return value.kind == listValue || value.kind == mixedListValue }

// IsNil is upstream's `value === undefined || value === null`: missing, a nil node, or a nil list.
func (value Value[N]) IsNil() bool {
	switch value.kind {
	case missingValue:
		return true
	case nodeValue:
		return isNilNode(value.node)
	case listValue:
		return value.nodes == nil
	case mixedListValue:
		return value.values == nil
	}
	return false
}

// isMissing is upstream's `value === undefined`, which a nil node or list is not.
func (value Value[N]) isMissing() bool { return value.kind == missingValue }

// Len is the length of a list, and zero for anything else.
func (value Value[N]) Len() int {
	switch value.kind {
	case listValue:
		return len(value.nodes)
	case mixedListValue:
		return len(value.values)
	}
	return 0
}

// Element is upstream's `value[index]` on a list: the element, or missing past either end or on a value
// that is not a list.
func (value Value[N]) Element(index int) Value[N] {
	if index < 0 || index >= value.Len() {
		return Value[N]{}
	}
	if value.kind == listValue {
		return NodeValue(value.nodes[index])
	}
	return value.values[index]
}

// fieldOf is upstream's `value?.[name]`: a node's property, and missing on anything that is not a node.
func fieldOf[N Node[N]](value Value[N], name string) Value[N] {
	if node, isNode := value.Node(); isNode {
		return node.Field(name)
	}
	return Value[N]{}
}

// isNilNode is a nil node pointer, which a generic N can only be compared to as its zero value.
func isNilNode[N comparable](node N) bool {
	var zero N
	return node == zero
}
