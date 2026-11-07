package source_name_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/types/source_name"
)

// An Adamic name reads as the same name ending in .ts, and every other name as itself: a directory, a name
// that is only the extension, and names that merely contain ".a" are not Adamic.
func TestAnAdamicNameIsTreatedAsTypeScript(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]string{
		"/repo/Dog.a":         "/repo/Dog.ts",
		"Dog.test.a":          "Dog.test.ts",
		"/repo/geometry.a":    "/repo/geometry.ts",
		"/repo/Dog.ts":        "/repo/Dog.ts",
		"/repo/Dog.tsx":       "/repo/Dog.tsx",
		"/repo/data.archive":  "/repo/data.archive",
		"/repo/a":             "/repo/a",
		".a":                  ".a",
		"/repo/.a":            "/repo/.a",
		"/repo/Dog.d.ts":      "/repo/Dog.d.ts",
		"/repo/lib/libfoo.ab": "/repo/lib/libfoo.ab",
	} {
		if got := source_name.TreatedAs(name); got != want {
			test.Errorf("TreatedAs(%q) = %q, want %q", name, got, want)
		}
		if got := source_name.IsAdamic(name); got != (want != name) {
			test.Errorf("IsAdamic(%q) = %t", name, got)
		}
	}
}

// A test file is a `.test` name before each TypeScript or JavaScript script extension, or Adamic's
// `.test.a`, and nothing that only resembles one: a `.spec` name, a name that holds `.test.` in its
// middle, a `test` directory, a declaration file and a `.test` name before a non-script extension are
// not. Each extension in the table has a row each way.
func TestATestFileIsADotTestScriptName(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]bool{
		"/repo/Dog.test.ts":           true,
		"/repo/Dog.test.tsx":          true,
		"/repo/Dog.test.js":           true,
		"/repo/Dog.test.jsx":          true,
		"/repo/Dog.test.mts":          true,
		"/repo/Dog.test.cts":          true,
		"/repo/Dog.test.a":            true,
		"Tests/announce.test.ts":      true,
		"/repo/Dog.ts":                false,
		"/repo/Dog.tsx":               false,
		"/repo/Dog.js":                false,
		"/repo/Dog.jsx":               false,
		"/repo/Dog.mts":               false,
		"/repo/Dog.cts":               false,
		"/repo/Dog.a":                 false,
		"/repo/Dog.spec.ts":           false,
		"/repo/Dog.spec.tsx":          false,
		"/repo/Dog.test.helpers.ts":   false,
		"/repo/Dog.test.helpers.tsx":  false,
		"/repo/test/Dog.ts":           false,
		"/repo/Dog.test.d.ts":         false,
		"/repo/DogTest.ts":            false,
		"/repo/Dog.test.ts.snapshot":  false,
		"/repo/Dog.test.json":         false,
		"/repo/Dog.test.css":          false,
		"/repo/Dog.test.mjs.snapshot": false,
	} {
		if got := source_name.IsTestFile(name); got != want {
			test.Errorf("IsTestFile(%q) = %t, want %t", name, got, want)
		}
	}
}
