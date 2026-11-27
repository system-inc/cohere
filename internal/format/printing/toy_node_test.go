package printing

type toyNode struct {
	Kind     string
	From     int
	To       int
	Value    string
	Label    *toyNode
	Children []*toyNode
	CommentFields[*toyNode]
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
