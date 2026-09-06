package react

import "github.com/system-inc/cohere/internal/rule"

// No decoder. Upstream's `meta.schema` is `[]`, confirmed against the installed build rather than
// read off the inventory column.
func init() {
	rule.Register(rule.Registration{Rule: NoNamespace})
}
