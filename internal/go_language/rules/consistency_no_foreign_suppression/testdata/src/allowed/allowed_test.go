package allowed

// fixture is a test's source held in a raw string, as the U1000 port's tests hold theirs: the directives in it are
// the fixture's text, and a raw string is one token, never a comment.
var fixture = `package fixture

//lint:ignore U1000 kept for the test
func kept() {}

//lint:file-ignore U1000 kept for the test

var value = 1 //nolint:nilerr
`
