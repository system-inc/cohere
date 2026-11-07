// Package longlines describes a delivery, and three of its lines run past the limit in ways a wrap can bring within it.
package longlines

import "fmt"

// Describe says where a delivery goes and when, as one sentence a reader can follow from its first word to its very last.
// It names everyone.
func Describe(sender string, recipient string, street string, city string, weekday string) string {
	return fmt.Sprintf("%s sends a parcel to %s, who lives at %s in %s, and it arrives on %s", sender, recipient, street, city, weekday)
}

// Deliverable is whether a delivery has everything it needs to leave the depot.
func Deliverable(sender string, recipient string, street string, city string) bool {
	return len(sender) > 0 && len(recipient) > 0 && len(street) > 0 && len(city) > 0 && sender != recipient && street != city
}

// Notices are what a courier reads out at the door.
var Notices = []string{
	"the parcel is fragile, so carry it upright and set it down gently on a flat surface", // read this one out first, please
}
