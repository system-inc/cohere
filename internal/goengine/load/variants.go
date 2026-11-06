package load

import "golang.org/x/tools/go/packages"

// analyzed is the roots to analyze, in go/packages' order. With Tests set, a package p with test files comes
// back as p, "p [p.test]" (p with its in-package tests), "p_test [p.test]" (its external tests) and "p.test"
// (go test's synthesized main). "p [p.test]" holds every file of p, so it stands in for p, and no non-test file
// is analyzed and reported twice; the main is generated, so it is dropped. A package with no test files comes
// back as p alone, which is then the one analyzed.
func analyzed(roots []*packages.Package) []*packages.Package {
	withTestVariant := map[string]bool{}
	for _, root := range roots {
		if root.ForTest != "" && root.PkgPath == root.ForTest {
			withTestVariant[root.PkgPath] = true
		}
	}
	var chosen []*packages.Package
	for _, root := range roots {
		switch {
		case root.ForTest != "":
			// A package go test builds again for another's tests, "q [p.test]", is q as analyzed already.
			if root.PkgPath == root.ForTest || root.PkgPath == root.ForTest+"_test" {
				chosen = append(chosen, root)
			}
		case isTestMain(root), withTestVariant[root.PkgPath]:
			// Dropped: generated, or analyzed as its test variant.
		default:
			chosen = append(chosen, root)
		}
	}
	return chosen
}

// withoutTestMain is roots without go test's synthesized main, which only go/packages' roots hold.
func withoutTestMain(roots []*packages.Package) []*packages.Package {
	var kept []*packages.Package
	for _, root := range roots {
		if !isTestMain(root) {
			kept = append(kept, root)
		}
	}
	return kept
}

// isTestMain reports whether candidate is go test's synthesized main. It is the one package that is no test
// variant itself and yet imports one: a package as written cannot import a package as built for a test.
func isTestMain(candidate *packages.Package) bool {
	if candidate.ForTest != "" {
		return false
	}
	for _, imported := range candidate.Imports {
		if imported.ForTest != "" {
			return true
		}
	}
	return false
}
