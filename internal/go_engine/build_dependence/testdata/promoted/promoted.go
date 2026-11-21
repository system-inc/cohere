// Package promoted holds structs whose promoted fields move with the build while nothing else the object
// differential renders of them does (#dj5csr5). On linux Extra gains a Name and Second gains a Count.
package promoted

// Base holds the Name every build selects through Wrap.
type Base struct {
	Name int
}

// Wrap embeds Base, so Inner selects Base's Name at depth 2.
type Wrap struct {
	Base
}

// Inner selects Name through Wrap at depth 2 on the host, and through Extra at depth 1 on linux, a string there.
type Inner struct {
	Wrap
	*Extra
}

// First holds the Count every build has.
type First struct {
	Count int
}

// Both selects First's Count on the host, and on linux, where Second has a Count too at the same depth, no Count at
// all: the name is ambiguous there.
type Both struct {
	First
	Second
}
