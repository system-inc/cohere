package core

import "github.com/system-inc/verify/internal/rule"

func init() {
	rule.Register(rule.Registration{Rule: NoDivRegex})
}
