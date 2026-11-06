package typescript

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers @typescript-eslint/no-useless-default-assignment.
//
// Registered and deliberately not enabled in the live config. CohereSettings.json line 373 carries
// `"typescript/no-useless-default-assignment": "off"`, a standing decision somebody made under the
// old short spelling. That key cannot resolve against this registration: `settingFor` matches
// exactly, then trims the rule name off the config key and requires what remains to end in a slash.
// The old key is 40 characters and the registered name is 48, so the key is shorter than the name,
// the trim is a no-op, and the branch never fires. Measured rather than read, with two controls
// that do resolve.
//
// So enabling this would reverse somebody's decision through a spelling difference, with nothing in
// the diff to show a decision was reversed at all. The port is complete and its eighty five
// imported cases pass; turning it on is a decision for whoever wrote that line.
func init() {
	rule.Register(rule.Registration[NoUselessDefaultAssignmentOptions]{
		Rule:   NoUselessDefaultAssignment,
		Decode: DecodeNoUselessDefaultAssignmentOptions,
	})
}
