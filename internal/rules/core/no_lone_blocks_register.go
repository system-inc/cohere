package core

import "github.com/system-inc/verify/internal/rule"

// init registers no-lone-blocks.
func init() {
	rule.Register(rule.Registration{Rule: NoLoneBlocks})
}
