package tailwind

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

// TestTsconfigPathsResolveStylesheetImports is the `tsconfig` option end to end, through the rule and the
// design system it loads (#ewss35z). The stylesheet imports `@design/brand.css`. Unmapped, that is a
// path beside the stylesheet, `app/@design/brand.css`, which declares nothing, so brand-glow is unknown.
// Mapped by a tsconfig's `paths`, it is `design/brand.css`, which declares brand-glow. `not-a-class` is
// unknown either way, so a run whose design system failed to load, and which reports nothing, can never
// pass for one that read the mapping.
func TestTsconfigPathsResolveStylesheetImports(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"Component.tsx":         `export const element = <div className="brand-glow flex not-a-class" />;`,
		"app/globals.css":       "@import \"tailwindcss\";\n@import \"@design/brand.css\";",
		"app/@design/brand.css": "/* what the specifier names unmapped, beside the stylesheet */",
		"design/brand.css":      "@utility brand-glow { color: red; }",
		// Comments and a trailing comma, as tsconfig files are written.
		"config/paths.json": `{
			// the design system's alias
			"compilerOptions": { "paths": { "@design/*": ["../design/*"], }, },
		}`,
		// A baseUrl in an extended file, rebased onto the file that extends it.
		// From a subdirectory, so the rebasing is what makes `../..` mean the repository's root.
		"config/shared/base.json": `{ "compilerOptions": { "baseUrl": "../..", "paths": { "@design/*": ["design/*"] } } }`,
		"config/extends.json":     `{ "extends": "./shared/base" }`,
		// A path mapped to nothing that exists, so ordinary resolution answers.
		"config/elsewhere.json": `{ "compilerOptions": { "paths": { "@design/*": ["../missing/*"] } } }`,
	}
	run := func(tsconfig string) rule_testing.Result {
		t.Helper()
		options := NoUnknownClassesOptions{TailwindLocationOptions: TailwindLocationOptions{
			EntryPoint: "app/globals.css", Tsconfig: tsconfig,
		}}
		return rule_testing.RunTypedFilesWithSetupAndOptions(t, NoUnknownClasses, files, "Component.tsx", options, func(root string) {
			modules := filepath.Join(root, "node_modules")
			if err := os.MkdirAll(modules, 0o755); err != nil {
				t.Fatalf("creating node_modules: %v", err)
			}
			if err := os.Symlink(unknownFixturePackageRoot(), filepath.Join(modules, "tailwindcss")); err != nil {
				t.Fatalf("linking tailwindcss: %v", err)
			}
		})
	}

	for name, testCase := range map[string]struct {
		tsconfig string
		reported string
	}{
		"no option, and the project's tsconfig maps nothing": {tsconfig: "", reported: "brand-glow not-a-class"},
		"paths, read through comments and a trailing comma":  {tsconfig: "config/paths.json", reported: "not-a-class"},
		"paths and baseUrl from an extended file":            {tsconfig: "config/extends.json", reported: "not-a-class"},
		"paths onto nothing that exists":                     {tsconfig: "config/elsewhere.json", reported: "brand-glow not-a-class"},
		"an option naming no file falls back to the default": {tsconfig: "config/absent.json", reported: "brand-glow not-a-class"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := run(testCase.tsconfig)
			// The class each finding names, which its description quotes; a decline names neither.
			var reported []string
			for _, diagnostic := range result.Diagnostics {
				for _, className := range []string{"brand-glow", "not-a-class"} {
					if strings.Contains(diagnostic.Message.Description, className) {
						reported = append(reported, className)
					}
				}
			}
			if got := strings.Join(reported, " "); got != testCase.reported {
				t.Errorf("reported %q, want %q", got, testCase.reported)
			}
		})
	}
}

// TestFindFirstPathRecursiveIsUpstreamsQueue pins upstream's order for several entries: each is tried
// where it was asked for before any is tried a directory up, and none climbs past cwd.
func TestFindFirstPathRecursiveIsUpstreamsQueue(t *testing.T) {
	t.Parallel()
	present := map[string]bool{"/repo/tsconfig.json": true, "/repo/app/jsconfig.json": true, "/repo/configs/custom.json": false}
	fileExists := func(path string) bool { return present[path] }

	if got := findFirstPathRecursive("/repo/app", "/repo/app", []string{"tsconfig.json", "jsconfig.json"}, fileExists); got != "/repo/app/jsconfig.json" {
		t.Errorf("from /repo/app found %q, want the jsconfig.json in cwd before any tsconfig.json above it", got)
	}
	if got := findFirstPathRecursive("/repo", "/repo", []string{"configs/custom.json", "tsconfig.json"}, fileExists); got != "/repo/tsconfig.json" {
		t.Errorf("found %q, want /repo/tsconfig.json once configs/custom.json is not there", got)
	}
	if got := findFirstPathRecursive("/repo/app", "/repo/app", []string{"tsconfig.json"}, fileExists); got != "" {
		t.Errorf("found %q; the search never climbs above cwd", got)
	}
}

// TestTsconfigMatchingIsTsconfigPaths pins the mapping's decisions: a relative specifier is never mapped,
// the longest prefix wins, and the match-all onto the base URL is added when the tsconfig names none.
func TestTsconfigMatchingIsTsconfigPaths(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"/repo/tsconfig.json":          `{ "compilerOptions": { "baseUrl": "src", "paths": { "@/*": ["styles/*"], "@/brand/*": ["brand/*"] } } }`,
		"/repo/src/styles/a.css":       "",
		"/repo/src/a.css":              "", // where a mapped `./a.css` would land, so mapping it shows
		"/repo/src/brand/b.css":        "",
		"/repo/src/styles/brand/b.css": "",
		"/repo/src/plain/c.css":        "",
		"/repo/src/pkg/package.json":   `{ "style": "dist/pkg.css" }`,
		"/repo/src/pkg/dist/pkg.css":   "",
		"/repo/src/viaextension.ts":    "",
		"/repo/src/viaextension.css":   "",
	}
	// Cleaned as the disk would: `/repo/src/./a.css` is `/repo/src/a.css` to any real filesystem.
	readFile := func(path string) (string, bool) { text, found := files[filepath.Clean(path)]; return text, found }
	fileExists := func(path string) bool { _, found := files[filepath.Clean(path)]; return found }

	mapping, err := loadTsconfigPaths("/repo/tsconfig.json", readFile, fileExists)
	if err != nil {
		t.Fatal(err)
	}
	for specifier, want := range map[string]string{
		"@/a.css":       "/repo/src/styles/a.css",
		"@/brand/b.css": "/repo/src/brand/b.css",
		"plain/c.css":   "/repo/src/plain/c.css",
		"pkg":           "/repo/src/pkg/dist/pkg.css",
		"viaextension":  "/repo/src/viaextension.css",
		"./a.css":       "",
		"@/missing.css": "",
	} {
		got, _ := mapping.resolve(specifier, readFile, fileExists)
		if got != want {
			t.Errorf("%s resolved to %q, want %q", specifier, got, want)
		}
	}
}
