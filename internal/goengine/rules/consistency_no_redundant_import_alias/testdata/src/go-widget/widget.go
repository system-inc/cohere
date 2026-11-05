// Package widget lives in a folder named go-widget, which a package name cannot spell, so the path's last element
// does not say the name. The fixtures import it and the test never analyzes it.
package widget

// Size is something for an importing fixture to use, so its import compiles.
const Size = 3
