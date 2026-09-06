package react

import "github.com/system-inc/cohere/internal/rule"

func init() {
	rule.Register(rule.Registration{Rule: NoDeprecated})
}
