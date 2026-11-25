package module_roots

import (
	"slices"
	"strings"
)

// Set decides which files are alive by definition.
//
// A file in the root set is never reported as unreferenced, whatever the reference index says about
// it. Getting this wrong in the permissive direction costs a missed finding; getting it wrong in the
// strict direction reports a live homepage as dead, which is the failure that makes a reader close
// the report and never open it again. So where the two are in tension this errs permissive, and says
// so at every site where it does.
type Set struct {
	// dynamicImportDirectories are directory prefixes that a computed `import()` specifier can reach.
	//
	// Collected from the program rather than configured, so a new dynamically-loaded directory is
	// covered the day it is written rather than the day someone remembers to list it.
	dynamicImportDirectories []string
}

// IsRoot reports whether a file is alive by definition, and why.
//
// The second result is empty when the file is not a root, so a caller can print the reason beside
// the decision rather than restating the rule.
func (set *Set) IsRoot(fileName string) (bool, Reason) {
	base := baseName(fileName)
	stem := stripExtension(base)

	// A declaration file declares rather than defines. Ambient ones are referenced by nothing and
	// affect the whole program, and the ones that are imported are already covered by the index, so
	// nothing is gained by reporting either.
	if strings.HasSuffix(base, ".d.ts") {
		return true, ReasonDeclaration
	}

	// A test runner loads test files by glob. They import the code under test and nothing imports
	// them, which is exactly the shape the reference index reports as unused.
	if isTestFile(stem) {
		return true, ReasonTest
	}

	if buildConfigFiles[stem] {
		return true, ReasonBuildConfiguration
	}

	// The App Router's reserved names only mean anything inside an app directory. Checking the
	// directory as well as the name keeps a utility genuinely called `error.ts` reportable.
	if nextAppRouterFiles[stem] && withinRouteDirectory(fileName) {
		return true, ReasonNextAppRoute
	}

	if withinPagesDirectory(fileName) {
		return true, ReasonNextPagesRoute
	}

	// Contains rather than HasPrefix. The collected entry is a directory segment (`/translations/`)
	// because a computed specifier's literal prefix is an alias path that this analysis deliberately
	// does not resolve, so the match has to be against the middle of an absolute path rather than
	// its start. HasPrefix here silently spared nothing and the locale fixture caught it.
	for _, directory := range set.dynamicImportDirectories {
		if strings.Contains(fileName, directory) {
			return true, ReasonDynamicImportTarget
		}
	}

	return false, ""
}

// DynamicImportDirectories returns the directories a computed `import()` roots, sorted, for a
// fingerprint of what IsRoot decides by besides the file's own name.
func (set *Set) DynamicImportDirectories() []string {
	return slices.Sorted(slices.Values(set.dynamicImportDirectories))
}
