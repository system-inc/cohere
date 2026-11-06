package javascript

import "github.com/system-inc/cohere/internal/format/printing"

// comments/attach/handle-for-x-statement-comments.js

// handleForXStatementComments is upstream's handleForXStatementComments.
func handleForXStatementComments(context *commentContext) bool {
	comment := context.Comment
	enclosingNode := context.Enclosing
	followingNode := context.Following
	options := context.Options
	if enclosingNode.Is("ForInStatement", "ForOfStatement", "ForStatement") &&
		followingNode != nil &&
		followingNode == enclosingNode.Child("body") {
		closingParenthesisIndex := lastIndexOfFrom(stripComments(options), ")", locStart(followingNode))

		if locStart(comment) > closingParenthesisIndex {
			printing.AddLeadingComment(followingNode, comment)
			return true
		}
	}

	return false
}
