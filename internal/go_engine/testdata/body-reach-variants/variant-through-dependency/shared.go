// Package audit holds a constant every build declares alike. Windows alone compiles an in-package test file, which
// declares a package-level true, so in the test variant windows builds Enabled is false.
package audit

// Enabled is true wherever true is the universe's.
const Enabled = true
