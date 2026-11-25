package core

import "github.com/system-inc/cohere/internal/typescript_language/rule"

// init registers no-lone-blocks.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: NoLoneBlocks})
}
