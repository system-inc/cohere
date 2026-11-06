// Package yaml lives in a folder named yaml.v3, as gopkg.in paths are, so the path's last element does not say the
// name. The fixtures import it and the test never analyzes it.
package yaml

// Indent is something for an importing fixture to use, so its import compiles.
const Indent = 2
