// Package audit holds two constants every build declares alike. Windows alone compiles an in-package test file, so
// windows checks the package's test variant in its place and holds no package of its plain ID, and that file declares
// a package-level true, so Enabled is false there.
package audit

// Root2 is the square root of two, to more digits than a machine float holds.
const Root2 = 1.41421356237309504880168872420969807856967187537694807317667974

// Enabled is true wherever true is the universe's.
const Enabled = true
