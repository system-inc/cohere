package core

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers no-lone-blocks.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: NoLoneBlocks})
}
