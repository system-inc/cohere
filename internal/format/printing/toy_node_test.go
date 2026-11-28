package printing

// toyNode is a node of the toy language. Its JSON is the shape upstream's printAstToDoc reads (type, start,
// end, and the fields the visitor keys name), which TestCoreAgreesWithUpstream's inputs are written in; the
// comment state stays out, since attaching comments is what the test compares.
type toyNode struct {
	Kind                    string     `json:"type"`
	From                    int        `json:"start"`
	To                      int        `json:"end"`
	Value                   string     `json:"value,omitempty"`
	Label                   *toyNode   `json:"label,omitempty"`
	Children                []*toyNode `json:"children,omitempty"`
	CommentFields[*toyNode] `json:"-"`
}

func (node *toyNode) Type() string { return node.Kind }
func (node *toyNode) Field(name string) Value[*toyNode] {
	switch name {
	case "children":
		return ListValue(node.Children)
	case "label":
		if node.Label == nil {
			return Value[*toyNode]{}
		}
		return NodeValue(node.Label)
	case "comments":
		return ListValue(node.Comments)
	}
	return Value[*toyNode]{}
}
func (node *toyNode) CommentData() *CommentFields[*toyNode] { return &node.CommentFields }
