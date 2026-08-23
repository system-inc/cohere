package nexus

import "github.com/system-inc/verify/internal/rule"

// cachedComments is allComments, computed once per file and shared by every rule that asks.
//
// The scan itself was already written once, which is what makes this easy to miss: three rules
// called one well-factored helper, and each call rescanned the file. A shared function is not a
// shared scan.
//
// Measured with `verify --timing` before this existed: consistency-no-shouting 807ms,
// consistency-no-long-line-comment 619ms, consistency-no-single-line-jsdoc 601ms, each visiting
// exactly one node per file. Flat node counts with unequal times is the signature of work done
// outside the walk.
func cachedComments(ctx rule.Context) []Comment {
	return rule.Cached(ctx.FileCache, "nexus.allComments", func() []Comment {
		return allComments(ctx.SourceFile)
	})
}
