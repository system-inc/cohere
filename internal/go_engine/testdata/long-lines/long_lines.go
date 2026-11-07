// Package longlines describes a delivery, with four code lines past the limit that a wrap can bring within it.
package longlines

import "fmt"

// Describe says where a delivery goes and when, as one sentence a reader can follow from its first word to its very last.
// It names everyone.
func Describe(sender string, recipient string, street string, city string, weekday string) string {
	return fmt.Sprintf("%s sends a parcel to %s, who lives at %s in %s, and it arrives on %s", sender, recipient, street, city, weekday)
}

// Deliverable is whether a delivery has everything it needs to leave the depot.
func Deliverable(sender string, recipient string, street string, city string) bool {
	// Serial: the depot reads this check for one courier at a time, and its tooling reads this marker as one whole line of text
	return len(sender) > 0 && len(recipient) > 0 && len(street) > 0 && len(city) > 0 && sender != recipient && street != city
}

// Greeting is what a courier says first, two of its pieces joined on one line where a wrap must keep them together.
func Greeting(name string, city string) string {
	return "good morning, " + name + ", and welcome to the depot's " + "own courier service in " + city + ", have a nice day"
}

// Notices are what a courier reads out at the door.
var Notices = []string{
	"the parcel is fragile, so carry it upright and set it down gently on a flat surface", // read this one out first, please
}

// Signature is how a courier signs off, a wording no break brings within the limit, so it stays as it was written.
func Signature(name string) string {
	return "with thanks from the whole depot, who wish your parcel a safe, dry and gentle journey all the way to its new door, " + name
}
