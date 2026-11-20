// Package presence is the build's package the standing_facts presence tests settle: it imports fmt, so hostport judges
// it, and each variable says beside it whether the rule's predicate holds of its declaration.
package presence

import (
	"fmt"

	"below"
)

// Address is hostport's candidate: its sole value joins a host and a port with a bare colon.
var Address = fmt.Sprintf("%s:%d", "localhost", 80)

// Count is no candidate: its format joins nothing.
var Count = fmt.Sprintf("%d", 80)

// Joined is no candidate either: three arguments, as an address's are, but its format joins them with a dash.
var Joined = fmt.Sprintf("%s-%s", "localhost", "80")

// Dial names Address.
func Dial() string { return Address }

// Port names no variable.
func Port() int { return 80 }

// Counted names Count.
func Counted() string { return Count }

// Joining names Joined.
func Joining() string { return Joined }

// Remote names below.Address, another package's variable of Address's name.
func Remote() string { return below.Address }
