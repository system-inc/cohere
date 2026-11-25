// Package unfollowed holds a body with a use no identity names, which the facts' reach takes as a user of every
// object (#5vbs1np), and a variable's initializer with one, which makes the package's initializers users of every
// object too (#bed0mnk).
package unfollowed

// settings is of a struct type with no name, whose field no identity reaches.
var settings struct{ A int }

// helper is called by a body that is followed and by one that is not.
func helper() int { return 1 }

// alone is called by no body.
func alone() int { return 2 }

// Caller calls helper, every use of its body followed.
func Caller() int { return helper() }

// Configured reads settings's field, whose identity is unknown, beside calling helper.
func Configured() int { return settings.A + helper() }

// initial is a value whose initializer reads settings's field, whose identity is unknown.
var initial = settings.A
