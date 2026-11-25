// Package p calls q's Old, for the facts' reach (#5vbs1np).
package p

import "example.com/deprecated/q"

// Caller calls Old, a deprecated use wherever Old is deprecated.
func Caller() {
	q.Old()
}

// Stays uses nothing any build changes, so the package is linted in part.
func Stays() int {
	return 1
}
