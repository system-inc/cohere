// Package marker declares Marker in a file every build compiles, and windows gives it a method in a file of its own,
// so a run that checks every build checks windows beside the host's.
package marker

// Marker marks a build.
type Marker struct{}
