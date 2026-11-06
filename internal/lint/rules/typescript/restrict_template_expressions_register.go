package typescript

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers @typescript-eslint/restrict-template-expressions.
//
// Registered and deliberately not enabled in the live config. CohereSettings.json carries
// `"typescript/restrict-template-expressions": "off"`, a standing decision somebody made under the
// old short spelling. That key cannot resolve against this registration: `settingFor` matches
// exactly, then trims the rule name off the config key, and the old key is shorter than
// `@typescript-eslint/restrict-template-expressions`, so the trim is a no-op and the branch never
// fires. Enabling would therefore reverse the decision through a spelling difference, leaving
// nothing in the diff to show it was reversed at all.
//
// The decoder is hand-rolled rather than generic, because five of the seven flags default to true
// and the `allow` default is a non-empty list. See its doc comment.
func init() {
	rule.Register(rule.Registration[RestrictTemplateExpressionsOptions]{
		Rule:   RestrictTemplateExpressions,
		Decode: DecodeRestrictTemplateExpressionsOptions,
	})
}
