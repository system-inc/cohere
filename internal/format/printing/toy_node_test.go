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

func (node *toyNode) Field(name string) any {
	switch name {
	case "children":
		return node.Children
	case "label":
		if node.Label == nil {
			return nil
		}
		return node.Label
	case "comments":
		return node.Comments
	}
	return nil
}

func (node *toyNode) CommentData() *CommentFields[*toyNode] { return &node.CommentFields }
