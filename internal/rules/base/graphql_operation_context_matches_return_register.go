package base

import "github.com/system-inc/verify/internal/rule"

// init registers base/graphql-operation-context-matches-return.
//
// Registered and deliberately NOT enabled. This is a rule from api-phi-health's base layer, and
// neither that project nor ahra carries a `base/` key in any lint configuration: base is that
// project's layer rather than this tree's. Where these rules get turned on is Kirk's decision.
//
// No Decode entry: the original's `schema: []` means no option surface.
func init() {
	rule.Register(rule.Registration{Rule: GraphQlOperationContextMatchesReturn})
}
