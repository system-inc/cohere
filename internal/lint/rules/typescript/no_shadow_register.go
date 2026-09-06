package typescript

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers @typescript-eslint/no-shadow.
//
// Its own file rather than a line in the package's register.go, so two ports landing at once do not
// conflict on a file neither is really changing.
//
// No Decode: this port covers upstream's default option set only and takes no options. See the
// rule's doc comment for which axes are covered and which are left.
func init() {
	rule.Register(rule.Registration{Rule: NoShadow})
}
