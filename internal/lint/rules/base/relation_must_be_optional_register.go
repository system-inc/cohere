package base

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers base/relation-must-be-optional.
//
// No Decode entry, because the source rule's `schema` is `[]` and it reads no options.
func init() {
	rule.Register(rule.Registration{Rule: RelationMustBeOptional})
}
