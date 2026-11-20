// Package partial type-checks on darwin and not on linux, where the file only linux compiles declares Limit, an int,
// from a string: a package the object differential cannot compare there, since its types are not the build's.
package partial

// Doubled reads Limit, which each platform's file declares.
const Doubled = Limit * 2
