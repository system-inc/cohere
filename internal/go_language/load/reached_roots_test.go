package load

import (
	"path/filepath"
	"slices"
	"testing"
)

// reachedRootsFiles are a module whose roots are of every kind reachedRoots tells apart: tool, a command main with a
// test, which no action reaches; lone, a command main with no test, analyzed as itself; base, which has a test and
// which user imports; and user, which has no test.
var reachedRootsFiles = map[string]string{
	"go.mod": "module example.com/reached\n\ngo 1.24\n",
	"tool/main.go": "package main\n\n// main prints nothing.\nfunc main() { _ = value() }\n\n" +
		"// value is one.\nfunc value() int { return 1 }\n",
	"tool/main_test.go": "package main\n\nimport \"testing\"\n\n// TestValue calls value.\n" +
		"func TestValue(test *testing.T) {\n\ttest.Parallel()\n\t_ = value()\n}\n",
	"lone/main.go": "package main\n\n// main does nothing.\nfunc main() {}\n",
	"base/base.go": "package base\n\n// Base is one.\nfunc Base() int { return 1 }\n",
	"base/base_test.go": "package base\n\nimport \"testing\"\n\n// TestBase calls Base.\n" +
		"func TestBase(test *testing.T) {\n\ttest.Parallel()\n\t_ = Base()\n}\n",
	"user/user.go": "package user\n\nimport \"example.com/reached/base\"\n\n// User calls Base.\n" +
		"func User() int { return base.Base() }\n",
}

// loadedIdentifiers is what a load of directory says of its roots and its packages to analyze, each as sorted IDs,
// and the files its roots compile, sorted, for a load with or without AnalyzedRootsOnly.
func loadedIdentifiers(test *testing.T, directory string, analyzedRootsOnly bool) (roots, analyzed, compiled []string) {
	test.Helper()
	loaded, loadError := PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
		Caching{AnalyzedRootsOnly: analyzedRootsOnly})
	if loadError != nil {
		test.Fatalf("loading %s: %v", directory, loadError)
	}
	if len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading %s found diagnostics: %v", directory, loaded.Diagnostics)
	}
	seen := map[string]bool{}
	for _, root := range loaded.Roots {
		roots = append(roots, root.ID)
		for _, file := range root.GoFiles {
			if !seen[file] {
				seen[file] = true
				compiled = append(compiled, filepath.Base(filepath.Dir(file))+"/"+filepath.Base(file))
			}
		}
	}
	for _, each := range loaded.Packages {
		analyzed = append(analyzed, each.ID)
	}
	slices.Sort(roots)
	slices.Sort(analyzed)
	slices.Sort(compiled)
	return roots, analyzed, compiled
}

// A load with AnalyzedRootsOnly leaves out exactly the root no action reaches, tool, whose test variant is analyzed
// in its place and which nothing imports. It keeps lone, a command main with no test, which is analyzed as itself;
// base, whose test variant is analyzed but which user imports; and user. What it analyzes, and the files its roots
// compile, which a build's coverage and the merge read, are the same as a load of every root.
func TestALoadOfAnalyzedRootsLeavesOutOnlyWhatNoActionReaches(test *testing.T) {
	test.Parallel()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	writeListingFiles(test, root, reachedRootsFiles)

	everyRoot, everyAnalyzed, everyCompiled := loadedIdentifiers(test, root, false)
	reachedRoots, reachedAnalyzed, reachedCompiled := loadedIdentifiers(test, root, true)

	left := slices.DeleteFunc(slices.Clone(everyRoot), func(identifier string) bool {
		return slices.Contains(reachedRoots, identifier)
	})
	if want := []string{"example.com/reached/tool"}; !slices.Equal(left, want) {
		test.Errorf("the roots left out are %v, want %v (every root %v, kept %v)", left, want, everyRoot,
			reachedRoots)
	}
	for _, kept := range []string{
		"example.com/reached/lone",
		"example.com/reached/base",
		"example.com/reached/user",
		"example.com/reached/tool [example.com/reached/tool.test]",
	} {
		if !slices.Contains(reachedRoots, kept) {
			test.Errorf("the root %q was left out, but an action reaches it: kept %v", kept, reachedRoots)
		}
	}
	if !slices.Equal(reachedAnalyzed, everyAnalyzed) {
		test.Errorf("the packages analyzed changed: %v, against %v with every root", reachedAnalyzed, everyAnalyzed)
	}
	if !slices.Equal(reachedCompiled, everyCompiled) {
		test.Errorf("the files the roots compile changed: %v, against %v with every root", reachedCompiled,
			everyCompiled)
	}
}
