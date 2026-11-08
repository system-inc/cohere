// Package dependency is the module outside the workspace whose objects the closure's tests call differing or alike.
package dependency

// Inner is embedded in Outer, so its Field is promoted through an embedded field no identifier names.
type Inner struct{ Field int }

// Outer embeds Inner.
type Outer struct{ Inner }

// Shape is the type of a type switch's binding where no case names it.
type Shape interface{ Area() int }

// Make returns a Shape, so a switch on it binds one.
func Make() Shape { return nil }

// Named is referenced by its name.
func Named() int { return 1 }

// Stable is read alike in both builds.
var Stable = 1

// Name is read alike in both builds.
const Name = "dependency"

// Handle is the type of Value's result, read only through expressions no identifier names.
type Handle int

// Value is a Handle, alike in both builds while Handle's reading differs.
func Value() Handle { return 1 }
