// Package versioned is a major version's code under a path ending /v2, so the path's last element does not spell the
// name the package declares. The fixtures import it and the test never analyzes it.
package versioned

// Version is something for an importing fixture to use, so its import compiles.
const Version = 2
