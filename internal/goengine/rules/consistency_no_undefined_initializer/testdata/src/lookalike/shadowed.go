package lookalike

// This package declares its own false. It holds false, but it is a named constant, which the rule leaves alone as it
// leaves every named constant, and not the predeclared value a reader would take it for.
const false = 1 == 2

var closed bool = false
