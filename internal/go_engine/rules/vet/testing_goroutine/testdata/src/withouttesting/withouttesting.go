// Package withouttesting imports no testing package, only aliasedtesting under testing's name, so the pass judges
// nothing here: the declaration below reads as a test by how it is written, and its forbidden call is the testing
// package's, so only the check that the package imports testing keeps it clean.
package withouttesting

import testing "aliasedtesting"

func TestWithoutTesting(t *testing.T) {
	go t.Fatal("no testing import") // clean
}
