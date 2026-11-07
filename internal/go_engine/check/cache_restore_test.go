package check

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// A session restores a second load when the long-line wrap wrote a file. When that restore cannot compute the keys,
// here because a file the first load listed is gone, the run saves nothing and counts nothing, and says why, rather
// than saving entries under the first load's keys, for bytes no longer on disk.
func TestARestoreWhoseKeysFailLeavesNoKeysOfTheLoadBefore(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, map[string]string{
		"go.mod":       "module example.com/twice\n\ngo 1.24\n",
		"base/base.go": "package base\n\n// Value is one.\nfunc Value() int { return 1 }\n",
		"user/user.go": "package user\n\nimport \"example.com/twice/base\"\n\n// Twice is two.\n" +
			"func Twice() int { return base.Value() * 2 }\n",
	})
	rules := []rule.Rule{structtagRule}
	session := NewCacheSession(testCaching(test, rules), rules)
	loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, Listings: session})
	if loadError != nil {
		test.Fatalf("loading %s: %v", root, loadError)
	}
	if len(session.keys) == 0 {
		test.Fatal("the first restore keyed nothing, so the second could not leave its keys behind")
	}
	if removeError := os.Remove(filepath.Join(root, "base", "base.go")); removeError != nil {
		test.Fatal(removeError)
	}
	var reachable []*packages.Package
	packages.Visit(loaded.Packages, nil, func(visited *packages.Package) { reachable = append(reachable, visited) })
	session.Restore(load.LoaderSource, reachable, loaded.Packages)
	result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld})
	if runError != nil {
		test.Fatalf("running %d rules: %v", len(rules), runError)
	}
	if result.Cache.Written != 0 || result.Cache.Hit != 0 || result.Cache.Missed != 0 {
		test.Fatalf("want nothing saved or counted after keys that failed, got %+v", result.Cache)
	}
	if len(result.Cache.Problems) == 0 {
		test.Fatal("want the keys' failure among the problems")
	}
}
