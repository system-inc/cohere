package react

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers this package's `no-invalid-html-attribute` rule.
//
// A file of its own rather than a line in `register.go`, so a port is three owned files and no
// shared edit. No `Decode`: upstream's schema permits exactly one value, `rel`, which is also the
// default, so no configuration can change this rule's behaviour and there is nothing to decode. The
// reasoning is on the rule.
func init() {
	rule.Register(rule.Registration{Rule: NoInvalidHtmlAttribute})
}
