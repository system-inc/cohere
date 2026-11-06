package compose

// Ported from eemeli/yaml 2.9.0, dist/nodes/: identity.js, Node.js, Scalar.js, Pair.js, Alias.js,
// Collection.js, YAMLMap.js and YAMLSeq.js, as far as composing reaches them, plus the node classes of
// dist/schema/yaml-1.1/omap.js and set.js, which the core schema's known tags construct.

// Kind is what upstream's identity checks read off a node's NODE_TYPE symbol.
type Kind string

const (
	KindAlias    Kind = "Alias"
	KindMap      Kind = "Map"
	KindPair     Kind = "Pair"
	KindScalar   Kind = "Scalar"
	KindSequence Kind = "Seq"
)

// Scalar types, upstream's Scalar.BLOCK_FOLDED and friends, with upstream's spelling: they are the
// values yaml-unist-parser switches on.
const (
	BlockFolded  = "BLOCK_FOLDED"
	BlockLiteral = "BLOCK_LITERAL"
	Plain        = "PLAIN"
	QuoteDouble  = "QUOTE_DOUBLE"
	QuoteSingle  = "QUOTE_SINGLE"
)

// MergeKey is the value the merge tag resolves << to, upstream's Symbol('<<'). Each one is a distinct
// symbol upstream, so it is a pointer here, to a type that is not zero-sized so distinct pointers stay
// distinct.
type MergeKey struct{ _ byte }

func (*MergeKey) scalarValue() {}

// Date is the value the timestamp tag resolves to: upstream's Date, as its time value in milliseconds,
// NaN for an invalid date.
type Date float64

func (Date) scalarValue() {}

// Binary is the value the binary tag resolves to: upstream's Buffer.
type Binary []byte

func (Binary) scalarValue() {}

// newScalar is `new Scalar(value)`.
func newScalar(value ScalarValue) *Node {
	return &Node{Kind: KindScalar, Class: "Scalar", ScalarValue: value}
}

// newAlias is `new Alias(source)`.
func newAlias(source string) *Node {
	return &Node{Kind: KindAlias, Class: "Alias", Source: source, sourceSet: true}
}

// newPair is `new Pair(key, value)`.
func newPair(key *Node, value *Node) *Node {
	return &Node{Kind: KindPair, Class: "Pair", Key: key, Value: value}
}

// newCollection is `new NodeClass(schema)` for one of the four collection classes. YAMLOMap's and
// YAMLSet's constructors set the tag to their own; items start as an empty array.
func newCollection(class string) *Node {
	node := collectionNode(class)
	return &node
}

// collectionNode is the node newCollection makes, as a value.
func collectionNode(class string) Node {
	switch class {
	case "YAMLMap":
		return Node{Kind: KindMap, Class: class, Items: []*Node{}}
	case "YAMLSet":
		return Node{Kind: KindMap, Class: class, Items: []*Node{}, Tag: setTagName}
	case "YAMLSeq":
		return Node{Kind: KindSequence, Class: class, Items: []*Node{}}
	case "YAMLOMap":
		return Node{Kind: KindSequence, Class: class, Items: []*Node{}, Tag: omapTagName}
	}
	// Invariant: class is a literal in this package or a tag's NodeClass, and the four tags that set
	// one (map, seq, omap and set) name exactly the four classes above.
	panic("compose: unknown collection class " + class)
}

// collectionTagName is the static tagName of a collection's class: YAMLSet inherits YAMLMap's and
// YAMLOMap inherits YAMLSeq's.
func collectionTagName(node *Node) string {
	if node.Kind == KindMap {
		return mapTagName
	}
	return seqTagName
}

// assignCollection is `Object.assign(new Class(), collection)`: a new node of the class with every own
// enumerable property of the collection copied over it. Upstream's collection has no anchor or srcToken
// yet at this point, and a property the collection does not have keeps the new node's value (the tag
// the constructor set). The range and items arrays are shared, as Object.assign shares them.
func assignCollection(class string, collection *Node) *Node {
	node := newCollection(class)
	node.Items = collection.Items
	node.Range = collection.Range
	if collection.Tag != "" {
		node.Tag = collection.Tag
	}
	node.Flow = collection.Flow
	node.Comment = collection.Comment
	node.CommentBefore = collection.CommentBefore
	node.SpaceBefore = collection.SpaceBefore
	if collection.anchorPresent {
		node.SetAnchor(collection.Anchor)
	}
	node.SourceToken = collection.SourceToken
	return node
}

// IsAlias is upstream's isAlias.
func IsAlias(node *Node) bool { return node != nil && node.Kind == KindAlias }

// IsMap is upstream's isMap.
func IsMap(node *Node) bool { return node != nil && node.Kind == KindMap }

// IsPair is upstream's isPair.
func IsPair(node *Node) bool { return node != nil && node.Kind == KindPair }

// IsScalar is upstream's isScalar.
func IsScalar(node *Node) bool { return node != nil && node.Kind == KindScalar }

// IsSequence is upstream's isSeq.
func IsSequence(node *Node) bool { return node != nil && node.Kind == KindSequence }

// IsCollection is upstream's isCollection.
func IsCollection(node *Node) bool { return IsMap(node) || IsSequence(node) }

// IsNode is upstream's isNode: anything but a pair.
func IsNode(node *Node) bool { return node != nil && node.Kind != KindPair }
