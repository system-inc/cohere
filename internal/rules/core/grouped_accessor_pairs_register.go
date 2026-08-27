package core

import "github.com/system-inc/verify/internal/rule"

// init registers grouped-accessor-pairs.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors landing
// at once touch nothing in common. Written immediately after the rule variable compiled rather than
// last: in the gap between the two, `TestEveryRuleIsRegistered` fails for the whole package and
// names this rule to every sibling agent, which is indistinguishable from an abandoned port.
//
// The decoder is hand-rolled rather than `rule.DecodeOptionsInto`, and this rule is a case that
// helper cannot serve at all: the first option is a bare string whose default is "anyOrder", so the
// zero value is the empty string, which matches none of the three arms.
func init() {
	rule.Register(rule.Registration{
		Rule:   GroupedAccessorPairs,
		Decode: DecodeGroupedAccessorPairsOptions,
	})
}
