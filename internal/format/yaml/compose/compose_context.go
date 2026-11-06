package compose

// composeContext is upstream's ComposeContext, one object shared and mutated down the whole document:
// atKey and atRoot are switched as composing descends.
type composeContext struct {
	atKey      bool
	atRoot     bool
	directives *Directives
	options    *Options
	schema     *Schema
}

// newScalar, newPair and newCollection on a context take the node from the parse's arena when the
// options carry one (Options.Nodes), and allocate it otherwise (#v6ksqg3).
func (composition *composeContext) newScalar(value ScalarValue) *Node {
	return composition.options.Nodes.New(Node{Kind: KindScalar, Class: "Scalar", ScalarValue: value})
}

func (composition *composeContext) newPair(key *Node, value *Node) *Node {
	return composition.options.Nodes.New(Node{Kind: KindPair, Class: "Pair", Key: key, Value: value})
}

func (composition *composeContext) newCollection(class string) *Node {
	return composition.options.Nodes.New(collectionNode(class))
}
