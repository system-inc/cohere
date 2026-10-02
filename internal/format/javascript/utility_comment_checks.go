package javascript

import (
	"regexp"
	"strings"
)

// utilities/has-leading-own-line-comment.js, utilities/return-statement-has-leading-comment.js,
// utilities/needs-hardline-after-dangling-comment.js,
// utilities/should-expression-statement-print-own-comments.js, utilities/is-type-cast-comment.js.

// hasLeadingOwnLineComment is upstream's hasLeadingOwnLineComment.
func hasLeadingOwnLineComment(text string, node Node) bool {
	if isJsxElement(node) {
		return hasNodeIgnoreComment(node)
	}

	return hasComment(node, commentLeading, func(comment Node) bool {
		return hasNewline(text, locEnd(comment))
	})
}

// returnArgumentHasLeadingComment is upstream's returnArgumentHasLeadingComment.
//
// This recurses the return argument, looking for the first token
// (the leftmost leaf node) and, if it (or its parents) has any
// leadingComments, returns true (so it can be wrapped in parens).
//
// Upstream memoizes it per node in a WeakMap. The answer reads only the node's subtree and its
// attached comments, which are fixed once printing starts, so computing it each time is the same.
func returnArgumentHasLeadingComment(node Node, options *Options) bool {
	if hasLeadingOwnLineComment(options.OriginalText, node) ||
		hasComment(node, commentLeading, func(comment Node) bool {
			return hasNewlineInRange(
				options.OriginalText,
				locStart(comment),
				locEnd(comment),
			)
		}) &&
			!isJsxElement(node) {
		return true
	}

	if hasNakedLeftSide(node) {
		leftMost := node
		for {
			newLeftMost := getLeftSide(leftMost)
			if newLeftMost == nil {
				break
			}
			leftMost = newLeftMost

			if hasLeadingOwnLineComment(options.OriginalText, leftMost) {
				return true
			}
		}
	}

	return false
}

// needsHardlineAfterDanglingComment is upstream's needsHardlineAfterDanglingComment.
func needsHardlineAfterDanglingComment(node Node) bool {
	comments := getComments(node, commentDangling, nil)
	if len(comments) == 0 {
		return false
	}
	return isLineComment(comments[len(comments)-1])
}

// shouldExpressionStatementPrintOwnComments is upstream's shouldExpressionStatementPrintOwnComments.
func shouldExpressionStatementPrintOwnComments(path *Path, options *Options) bool {
	if !shouldExpressionStatementPrintLeadingSemicolon(path, options) {
		return false
	}

	// Note: this causes the following print differently
	// `;/** @type {string[]} */ ([]).forEach(foo)`
	// `;/* normal comment */ ([]).forEach(foo)`
	// We may want consider remove the `isTypeCastComment` check
	comments := getComments(node(path), commentLeading, nil)
	if len(comments) > 0 && isTypeCastComment(comments[len(comments)-1]) {
		return true
	}

	return false
}

var typeCastCommentPattern = regexp.MustCompile(`@(?:type|satisfies)\b`)

// isTypeCastComment is upstream's isTypeCastComment. Upstream memoizes it per comment; the answer
// reads only the comment's value, so computing it each time is the same.
func isTypeCastComment(comment Node) bool {
	return isBlockComment(comment) &&
		strings.HasPrefix(comment.String("value"), "*") &&
		// TypeScript expects the type to be enclosed in curly brackets, however
		// Closure Compiler accepts types in parens and even without any delimiters at all.
		// That's why we just search for "@type" and "@satisfies".
		typeCastCommentPattern.MatchString(comment.String("value"))
}
