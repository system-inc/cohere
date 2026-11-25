// Package differences holds the comparisons where the port's test of syntax, and the fix it prints, could most
// plausibly part from the pattern's: arguments in parentheses, which the fix prints without them, a function in
// parentheses on the right only, a comparison spread over lines, and calls whose arguments are themselves calls.
// Both find the same here, fixes included, so the fixture records no difference.
package differences

import "strings"

func shapes(first string, second string) bool {
	_ = strings.ToLower((first)) == strings.ToLower((second))
	_ = strings.ToUpper(first) == (strings.ToUpper)(second)
	_ = strings.ToLower(first) !=
		strings.ToLower(second)
	return strings.ToLower(strings.TrimSpace(first)) == strings.ToLower(strings.Repeat(second, 2))
}
