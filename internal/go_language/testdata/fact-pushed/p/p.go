// Package p calls q's W, for the facts' reach (#5vbs1np, must 2).
package p

import "example.com/pushed/q"

// B hands W a string for %d, a printf finding wherever W wraps fmt.Printf. It names only W, whose signature is the
// same in every build, so the reach leaves it standing, and only W's wrapper fact can take it out.
func B() {
	q.W("%d\n", "x")
}
