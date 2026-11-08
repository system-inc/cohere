// Package aliasedtesting names the testing package's test by an alias, so a package can take one without importing
// testing itself.
package aliasedtesting

import "testing"

// T is the testing package's test.
type T = testing.T
