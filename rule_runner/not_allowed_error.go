package rule_runner

import "fmt"

// NotAllowedError is a request for a rule that isn't on the allowlist, whether or not cohere has the rule.
type NotAllowedError struct {
	Rule string
}

func (notAllowedError NotAllowedError) Error() string {
	return fmt.Sprintf(
		"rule_runner: %q is not a rule another program may run; the allowlist in rule_runner.go decides which are",
		notAllowedError.Rule,
	)
}
