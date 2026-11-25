// Package packagelength declares a len of its own at package scope, which every file of the package calls in the
// builtin's place, so it sits in a package of its own.
package packagelength

import "strings"

func len(text string) int { return 0 }

// The len called is the package's, not the builtin, so neither slice cuts off the prefix, and neither is reported.
func packageScopeLength(text string, prefix string) {
	if strings.HasPrefix(text, prefix) {
		text = text[len(prefix):]
	}
	if strings.HasSuffix(text, prefix) {
		text = text[:len(text)-len(prefix)]
	}
	// The unconditional call asks no len, so it is reported here as anywhere.
	if strings.HasPrefix(text, prefix) {
		text = strings.TrimPrefix(text, prefix)
	}
	_ = text
}
