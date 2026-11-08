package go_listing

import "fmt"

// Reach is what a build of root reads from a listing, in the listing's order: root itself, root's
// test main, and every package either depends on, directly or not. The test main's dependencies are
// what -test compiled again for root's test: root's own test variants and any dependency that
// imports root. A listing made without -test holds no test main, so its reach is root and its
// dependencies. A root the listing doesn't hold reaches nothing, and so does the empty root, which an
// object whose field list left out ImportPath would otherwise match.
//
// Every listed package's own Deps is already transitive, so the reach is one step from those two.
func Reach(packages []Package, root string) []Package {
	if root == "" {
		return nil
	}
	reached := map[string]bool{}
	for _, listed := range packages {
		if listed.ImportPath == root || listed.ImportPath == root+".test" {
			reached[listed.ImportPath] = true
			for _, dependency := range listed.Deps {
				reached[dependency] = true
			}
		}
	}
	var reach []Package
	for _, listed := range packages {
		if reached[listed.ImportPath] {
			reach = append(reach, listed)
		}
	}
	return reach
}

// ReachError is the first error go list reported in a reach, in its order: a package's own Error, or
// else the first of its DepsErrors, named with the package's import path. A reach with none is nil.
// It is what refuses a reach without refusing the whole listing, which a list run without -e does.
func ReachError(reach []Package) error {
	for _, listed := range reach {
		if listed.Error != nil {
			return fmt.Errorf("%s: %w", listed.ImportPath, listed.Error)
		}
		if len(listed.DepsErrors) != 0 {
			return fmt.Errorf("%s, through a dependency: %w", listed.ImportPath, listed.DepsErrors[0])
		}
	}
	return nil
}
