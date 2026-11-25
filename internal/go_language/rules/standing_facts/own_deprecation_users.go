package standing_facts

import (
	"slices"

	"github.com/system-inc/cohere/internal/go_language/fact_reach"
)

// ownDeprecationUsers is the host's side of the package with each of its functions named for its own deprecation
// counted among its own users, at the places that declare it (#7v2bzvd). SA1019 reads the deprecation of the function
// a use sits in, which may use deprecated objects (deprecated_use's judge), so a function's own body turns on its own
// deprecation: one that differs takes that body out too, not only the bodies that call the function. Every other read
// is the host's.
type ownDeprecationUsers struct {
	fact_reach.Host

	// declaring are the places that declare each function named for its own deprecation, by its key.
	declaring map[fact_reach.Object][]fact_reach.Place
}

// Users are the host's users of object, and the places that declare it where it was named for its own deprecation.
func (source ownDeprecationUsers) Users(object fact_reach.Object) []fact_reach.Place {
	users := source.Host.Users(object)
	if declaring := source.declaring[object]; len(declaring) > 0 {
		users = append(slices.Clone(users), declaring...)
	}
	return users
}
