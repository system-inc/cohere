package base

import "github.com/system-inc/verify/internal/rule"

// init registers base/inject-type-matches-parameter.
//
// Its own file rather than a line in a shared register.go, so two ports landing at once do not
// conflict on a file neither is really changing.
//
// The rule is registered and DELIBERATELY NOT ENABLED, for the reason its sibling records: ahra's
// config names no `base/` rules at all, because base is api-phi-health's own lint layer.
//
// No Decode entry: the original's `schema: []` means the rule has no option surface at all.
func init() {
	rule.Register(rule.Registration{Rule: InjectTypeMatchesParameter})
}
