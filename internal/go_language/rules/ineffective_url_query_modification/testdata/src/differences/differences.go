// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: the port matches what the pattern matches. A rewrite of the pattern could part
// from it at the parentheses its matcher looks through at every level, at the receiver it binds with its parentheses
// removed, and at the arguments it requires of Query and leaves free for the method, so these calls hold each, and
// any of them judged otherwise would show here as a finding one side makes and the other does not.
package differences

import "net/url"

// Found by both: parentheses at every level at once.
func parentheses(address *url.URL) {
	((address).Query)().Set("a", "b")
	(((address).Query)().Set)("a", "b")
}

// Found by both: a receiver whose type is known only through its parentheses.
func parenthesizedReceiver(addresses []*url.URL) {
	(addresses[0]).Query().Del("a")
}

// Found by neither: Del named on the copy, never called.
func uncalled(address *url.URL) func(string) {
	return address.Query().Del
}
