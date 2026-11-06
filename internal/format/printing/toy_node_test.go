package printing

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
