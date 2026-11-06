package printing

// CommentFields is upstream's comment bookkeeping, which it keeps as properties on plain objects: the
// comments attached to a node, and on a comment node, how it was attached and whether it printed.
//
// Embed it in the language's node struct and return its address from CommentData.
type CommentFields[N any] struct {
	// Comments are the comments attached to this node, upstream's node.comments.
	Comments []N

	// Leading, Trailing and Printed are set on a comment node by attachment and printing.
	Leading  bool
	Trailing bool
	Printed  bool

	// Placement is "ownLine", "endOfLine" or "remaining", how attachment classified the comment.
	Placement string

	// Marker distinguishes dangling comments a printer prints in different places.
	Marker string

	// Enclosing, Preceding and Following are attachment's view of a comment's neighbours. Upstream sets
	// them during attachment, reads them in the language handlers, and deletes them afterwards. The
	// zero value means absent.
	Enclosing, Preceding, Following          N
	hasEnclosing, hasPreceding, hasFollowing bool
}

// EnclosingNode, PrecedingNode and FollowingNode return a neighbour and whether it was set, because a
// nil pointer of a generic type cannot be compared to nil without knowing the type.
func (fields *CommentFields[N]) EnclosingNode() (N, bool) {
	return fields.Enclosing, fields.hasEnclosing
}

// PrecedingNode is the comment's closest preceding node, as attachment found it.
func (fields *CommentFields[N]) PrecedingNode() (N, bool) {
	return fields.Preceding, fields.hasPreceding
}

// FollowingNode is the comment's closest following node, as attachment found it.
func (fields *CommentFields[N]) FollowingNode() (N, bool) {
	return fields.Following, fields.hasFollowing
}
