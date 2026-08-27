package base

import "github.com/system-inc/verify/internal/rule"

// init registers base/orm-column-requires-declare.
//
// No Decode entry, because the source rule's `schema` is `[]` and it reads no options.
func init() {
	rule.Register(rule.Registration{Rule: OrmColumnRequiresDeclare})
}
