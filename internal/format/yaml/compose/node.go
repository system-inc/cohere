package compose

import "github.com/system-inc/cohere/internal/format/yaml/cst"

// Node is any composed node: a Scalar, Alias, YAMLMap, YAMLSeq or Pair, one struct for all of them as in
// the cst and unist packages. Kind says which, and the identity functions below read it the way
// upstream's isScalar and friends read NODE_TYPE. A field exists upstream only on the kinds noted.
//
// JavaScript's absent and empty fields: Tag and Comment and CommentBefore are "" when upstream's are
// undefined or null, because upstream never assigns them an empty string. Anchor can be the empty string
// (`&` alone), so HasAnchor tells it from absent. Range is nil when upstream's is undefined, which only a
// pair key resolvePairs makes from nothing has.
type Node struct {
	Kind Kind
	// Class is the node's constructor name: Scalar, Alias, Pair, YAMLMap, YAMLSeq, or the YAML 1.1
	// collections YAMLOMap (a YAMLSeq) and YAMLSet (a YAMLMap).
	Class string

	// Range is [start, value end, node end] in UTF-16 code units: the end of the value, then the end
	// of the node including any comment on the same line.
	Range []int
	// SourceToken is the CST token the node was composed from (keepSourceTokens): every scalar, alias and
	// collection composed from a token, but not an empty node composed from nothing, nor the map a
	// flow sequence wraps around a pair.
	SourceToken *cst.Token
	// SourceItem is a Pair's srcToken: the collection item it was composed from.
	SourceItem *cst.CollectionItem

	// Anchor is the anchor's name without the &, set by composeNode; HasAnchor says whether it is set.
	Anchor        string
	anchorPresent bool
	// Tag is the resolved tag: a full tag like tag:yaml.org,2002:str, a local tag like !foo, or ! for
	// the non-specific tag.
	Tag string

	Comment       string
	CommentBefore string
	SpaceBefore   bool

	// Flow marks a flow collection, and the map a flow sequence wraps around a pair.
	Flow bool
	// Items is a collection's items: Pairs in a map; Nodes in a sequence, or Pairs once a !!pairs or
	// !!omap tag has resolved it.
	Items []*Node

	// Key and Value are a Pair's. Value is nil for a pair with no value (upstream's null).
	Key   *Node
	Value *Node

	// Source is a Scalar's string value before the tag resolved it, and an Alias's anchor name without
	// the *. A Go string, WTF-8 where the JavaScript string holds a lone surrogate. HasSource says
	// whether it is set: the pair key resolvePairs makes from nothing has none.
	Source    string
	sourceSet bool
	// Type is a Scalar's style: BlockFolded, BlockLiteral, Plain, QuoteDouble, QuoteSingle, or "" for
	// the empty scalar of a block scalar without a header.
	Type string
	// Format is a Scalar's number format from its tag: `EXP`, `OCT`, HEX, `BIN` or `TIME`.
	Format string
	// MinFractionDigits is set by the float tags for a value written with trailing zeros, 1.50.
	MinFractionDigits int
	// ScalarValue is a Scalar's resolved value: nil (null), a ScalarBoolean, ScalarNumber, ScalarString
	// (WTF-8), *MergeKey, Date or Binary.
	ScalarValue ScalarValue
}

// HasSource is whether the node has a source: every alias, and every scalar composeScalar made.
func (node *Node) HasSource() bool { return node.sourceSet }

// HasAnchor is whether composeNode gave the node an anchor, perhaps an empty one.
func (node *Node) HasAnchor() bool { return node.anchorPresent }

// SetAnchor is `node.anchor = name`.
func (node *Node) SetAnchor(name string) {
	node.Anchor = name
	node.anchorPresent = true
}

// hasAllNullValues is Collection.hasAllNullValues.
func (node *Node) hasAllNullValues(allowScalar bool) bool {
	for _, item := range node.Items {
		if !IsPair(item) {
			return false
		}
		node := item.Value
		// `n == null || (allowScalar && isScalar(n) && n.value == null && !n.commentBefore &&
		// !n.comment && !n.tag)`
		if !(node == nil || allowScalar && IsScalar(node) && node.ScalarValue == nil && node.CommentBefore == "" &&
			node.Comment == "" && node.Tag == "") {
			return false
		}
	}
	return true
}
