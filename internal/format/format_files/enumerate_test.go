package format_files

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/format_options"
)

// writeTree materializes a fixture project and returns its root.
func writeTree(test *testing.T, files map[string]string) string {
	test.Helper()
	root := test.TempDir()
	for name, contents := range files {
		path := filepath.Join(root, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatalf("mkdir for %s: %v", name, createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("write %s: %v", name, writeError)
		}
	}
	return root
}

// TestEnumerateOffersEveryLanguage is the positive control the ruling asked for: a project with a
// file in each language the engine speaks, proving each one is offered rather than only TypeScript.
//
// This is the whole point of the change. The pipeline's universe was the type graph, so a .css in a
// TypeScript project was not declined, it was absent, and absence and agreement print the same.
func TestEnumerateOffersEveryLanguage(test *testing.T) {
	test.Parallel()
	root := writeTree(test, map[string]string{
		"a.ts":      "export const a = 1;\n",
		"b.tsx":     "export const b = <div />;\n",
		"c.css":     ".c { color: red; }\n",
		"d.md":      "# d\n",
		"e.json":    "{ \"e\": 1 }\n",
		"f.graphql": "type F { g: Int }\n",
	})

	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	if len(enumeration.Files) != 6 {
		test.Fatalf("offered %d files, want 6: %v", len(enumeration.Files), enumeration.Files)
	}
	for _, name := range []string{"a.ts", "b.tsx", "c.css", "d.md", "e.json", "f.graphql"} {
		found := false
		for _, file := range enumeration.Files {
			if filepath.Base(file) == name {
				found = true
				break
			}
		}
		if !found {
			test.Errorf("%s was not offered", name)
		}
	}
}

// TestEnumerateNamesWhatItDeclined is the negative control, and it is the one that matters.
//
// A file the engine cannot format must come back as a named zero rather than as an absence. A clean
// enumeration and a vacuous one look identical from the outside, so the report has to say what it
// refused and why, or the coverage line inherits the same blindness it was built to destroy.
func TestEnumerateNamesWhatItDeclined(test *testing.T) {
	test.Parallel()
	root := writeTree(test, map[string]string{
		"a.ts":      "export const a = 1;\n",
		"tool.go":   "package tool\n",
		"script.rb": "puts 1\n",
		"README":    "no extension\n",
	})

	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	if enumeration.Unhandled != 3 {
		test.Errorf("Unhandled = %d, want 3", enumeration.Unhandled)
	}
	for extension, want := range map[string]int{".go": 1, ".rb": 1, "(none)": 1} {
		if enumeration.DeclinedExtensions[extension] != want {
			test.Errorf("DeclinedExtensions[%q] = %d, want %d",
				extension, enumeration.DeclinedExtensions[extension], want)
		}
	}
	// The subtraction has to reconcile, or a file went missing between the walk and the result.
	accounted := len(enumeration.Files) + enumeration.Unhandled + enumeration.SymbolicLinks
	for _, removed := range enumeration.IgnoredByLayer {
		accounted += removed
	}
	if accounted != enumeration.Walked {
		test.Errorf("walked %d but accounted for %d, so files went missing silently",
			enumeration.Walked, accounted)
	}
}

// TestEnumerateSkipsSymbolicLinks: a link is counted and never offered or read, whatever it points at.
// A link to a directory named like a source file is the case that broke a run: pnpm's
// node_modules/zone.js, a link to a package directory, was offered as a .js file and failed reading a
// directory (TanStack Query's examples/angular/basic, found by @system_cohere_types).
func TestEnumerateSkipsSymbolicLinks(test *testing.T) {
	test.Parallel()
	root := writeTree(test, map[string]string{
		"a.ts":                   "export const a = 1;\n",
		"packages/zone/index.js": "export const zone = 1;\n",
		"vendor/real.ts":         "export const real = 1;\n",
	})
	for link, target := range map[string]string{
		"zone.js":     filepath.Join(root, "packages", "zone"),
		"alias.ts":    filepath.Join(root, "vendor", "real.ts"),
		"dangling.ts": filepath.Join(root, "missing.ts"),
	} {
		if symlinkError := os.Symlink(target, filepath.Join(root, link)); symlinkError != nil {
			test.Skipf("this filesystem cannot make symbolic links: %v", symlinkError)
		}
	}

	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("a tree holding symbolic links failed to enumerate: %v", enumerateError)
	}
	if enumeration.SymbolicLinks != 3 {
		test.Errorf("SymbolicLinks = %d, want 3", enumeration.SymbolicLinks)
	}
	for _, file := range enumeration.Files {
		if base := filepath.Base(file); base == "zone.js" || base == "alias.ts" || base == "dangling.ts" {
			test.Errorf("offered the link %s", file)
		}
	}
	if len(enumeration.Files) != 3 {
		test.Errorf("offered %v, want the three real files", enumeration.Files)
	}
	accounted := len(enumeration.Files) + enumeration.Unhandled + enumeration.SymbolicLinks
	for _, removed := range enumeration.IgnoredByLayer {
		accounted += removed
	}
	if accounted != enumeration.Walked {
		test.Errorf("walked %d but accounted for %d", enumeration.Walked, accounted)
	}
}

// TestEnumerateRefusesNestedRepositories proves the walk does not descend into somebody else's tree.
//
// The ignore layers do not cover this and cannot: a submodule is tracked by the parent as a gitlink
// rather than as ignored paths, so no ignore list names it. Verified against the real repository --
// none of them mentions libraries/structure.
//
// The cost of not having this is measured rather than hypothetical. A whole-tree run tonight wrote
// a line into a submodule; the change was correct and still unrequested, which is the worst shape
// for a surprise because it survives review.
func TestEnumerateRefusesNestedRepositories(test *testing.T) {
	test.Parallel()
	root := writeTree(test, map[string]string{
		"a.ts":             "export const a = 1;\n",
		"vendor/.git/HEAD": "ref: refs/heads/main\n",
		"vendor/theirs.ts": "export const theirs = 1;\n",
		"vendor/deep/x.md": "# x\n",
	})

	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	for _, file := range enumeration.Files {
		if filepath.Base(file) == "theirs.ts" || filepath.Base(file) == "x.md" {
			test.Errorf("descended into a nested repository and offered %s", file)
		}
	}
	if len(enumeration.NestedRepositories) != 1 || enumeration.NestedRepositories[0] != "vendor" {
		test.Errorf("NestedRepositories = %v, want [vendor]", enumeration.NestedRepositories)
	}
	if len(enumeration.Files) != 1 {
		test.Errorf("offered %v, want just a.ts", enumeration.Files)
	}
}

// TestEnumerateNamesTheDirectoriesItEntered: a cache replaying the walk stats exactly the directories
// it read, so the list holds the root and every directory entered, and none it pruned by an ignore
// layer or skipped as a nested repository. A directory missing from it is a change nothing would see;
// one too many is only a wasted stat.
func TestEnumerateNamesTheDirectoriesItEntered(test *testing.T) {
	test.Parallel()
	root := settingsTree(test, `["pnpm-lock.yaml"]`, `["archived/**"]`, map[string]string{
		".gitignore":       "dist/\n",
		"a.ts":             "export const a = 1;\n",
		"source/b.ts":      "export const b = 1;\n",
		"source/deep/c.ts": "export const c = 1;\n",
		"dist/built.ts":    "export const built = 1;\n",
		"archived/old.md":  "# old\n",
		"vendor/.git/HEAD": "ref: refs/heads/main\n",
		"vendor/theirs.ts": "export const theirs = 1;\n",
	})

	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	var entered []string
	for _, directory := range enumeration.Directories {
		relative, relativePathError := filepath.Rel(root, directory)
		if relativePathError != nil || !filepath.IsAbs(directory) {
			test.Fatalf("directory %q is not an absolute path under the root", directory)
		}
		entered = append(entered, filepath.ToSlash(relative))
	}
	sort.Strings(entered)
	if want := ".,nexus,source,source/deep"; strings.Join(entered, ",") != want {
		test.Fatalf("entered %v, want %s: the root and every directory read, none pruned or nested", entered, want)
	}
}

// settingsTree writes a repository whose CohereSettings.json extends a Nexus tier, with the format
// block's `ignore` (omitted when house is empty) and the project's own ignorePatterns.
func settingsTree(test *testing.T, house string, ignorePatterns string, files map[string]string) string {
	test.Helper()
	block := `{"tabWidth": 4}`
	if house != "" {
		block = `{"tabWidth": 4, "ignore": ` + house + `}`
	}
	// The settings files join a copy, so the caller's map still holds only the files it named.
	tree := maps.Clone(files)
	tree["nexus/NexusCohereSettings.json"] = `{"rules": {}, "format": ` + block + `}`
	tree["CohereSettings.json"] = `{"extends": "./nexus/NexusCohereSettings.json", "rules": {}, "ignorePatterns": ` +
		ignorePatterns +
		`}`
	return writeTree(test, tree)
}

// TestEnumerateAppliesEveryIgnoreLayer is the walk's three layers: `.gitignore`, then the format block's
// `ignore`, then the project's ignorePatterns, each counted under its own name.
//
// Counted separately because a layer that silently fails to load removes nothing, and a slightly
// smaller total is not a detectable signal. A zero next to a layer name is.
func TestEnumerateAppliesEveryIgnoreLayer(test *testing.T) {
	test.Parallel()
	root := settingsTree(test, `["pnpm-lock.yaml"]`, `["archived/**"]`, map[string]string{
		".gitignore":      "built.ts\n",
		"a.ts":            "export const a = 1;\n",
		"built.ts":        "export const built = 1;\n",
		"archived/old.md": "# old\n",
		"pnpm-lock.yaml":  "lockfileVersion: 1\n",
	})

	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	if len(enumeration.IgnoredByLayer) != 3 {
		test.Errorf(
			"layers %v, want exactly .gitignore, %s and %s",
			enumeration.IgnoredByLayer,
			HouseIgnoreLayer,
			IgnorePatternsLayer,
		)
	}
	for layer, want := range map[string]int{".gitignore": 1, HouseIgnoreLayer: 1, IgnorePatternsLayer: 1} {
		if enumeration.IgnoredByLayer[layer] != want {
			test.Errorf("layer %s removed %d, want %d", layer, enumeration.IgnoredByLayer[layer], want)
		}
	}
	survivors := map[string]bool{}
	for _, file := range enumeration.Files {
		relative, relativeError := filepath.Rel(root, file)
		if relativeError != nil {
			test.Fatalf("naming %s under %s: %v", file, root, relativeError)
		}
		survivors[filepath.ToSlash(relative)] = true
	}
	if !survivors["a.ts"] || survivors["archived/old.md"] || survivors["pnpm-lock.yaml"] || survivors["built.ts"] {
		test.Errorf("survivors = %v, want a.ts and the settings files only", enumeration.Files)
	}
}

// TestALeftoverPrettierignoreIsRefused: cohere no longer reads a `.prettierignore`, so one left in the
// walk root is refused, naming the file and saying to delete it, rather than kept as a list that looks
// like it still skips something. With a chain and without one, since the file is read nowhere either way.
func TestALeftoverPrettierignoreIsRefused(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name string
		root func() string
	}{
		{"with a settings chain", func() string {
			return settingsTree(
				test,
				`["pnpm-lock.yaml"]`,
				`[]`,
				map[string]string{".prettierignore": "archived/\n", "a.ts": "export const a = 1;\n"},
			)
		}},
		{"without settings", func() string {
			return writeTree(test, map[string]string{".prettierignore": "archived/\n", "a.ts": "export const a = 1;\n"})
		}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			root := testCase.root()
			_, enumerateError := Enumerate(root, handlesEveryLanguage)
			if !errors.Is(enumerateError, format_options.ErrPrettierConfigRemains) {
				test.Fatalf("a leftover .prettierignore was not refused as Prettier config: %v", enumerateError)
			}
			for _, want := range []string{filepath.Join(root, ".prettierignore"), "delete it"} {
				if !strings.Contains(enumerateError.Error(), want) {
					test.Errorf("refusal %q does not say %s", enumerateError, want)
				}
			}
		})
	}

	// Only the walk root's: one inside the tree is a file like any other, and nothing reads it.
	root := settingsTree(
		test,
		`["pnpm-lock.yaml"]`,
		`[]`,
		map[string]string{"fixtures/.prettierignore": "x\n", "a.ts": "export const a = 1;\n"},
	)
	if _, enumerateError := Enumerate(root, handlesEveryLanguage); enumerateError != nil {
		test.Errorf("a .prettierignore below the root was refused: %v", enumerateError)
	}
}

// TestEveryWalkHasAHouseList: zero config is the house stack (#bfxz13m), so a walk never offers the
// files no repository formats. An outsider's block without an `ignore`, and a root with no settings at
// all, both skip pnpm-lock.yaml by the house list, beside .gitignore.
func TestEveryWalkHasAHouseList(test *testing.T) {
	test.Parallel()
	for name, root := range map[string]string{
		"an outsider's block without ignore": settingsTree(test, "", `[]`, map[string]string{
			".gitignore":     "built.ts\n",
			"a.ts":           "export const a = 1;\n",
			"built.ts":       "export const built = 1;\n",
			"pnpm-lock.yaml": "lockfileVersion: 1\n",
		}),
		"no settings at all": writeTree(test, map[string]string{
			".gitignore":     "built.ts\n",
			"a.ts":           "export const a = 1;\n",
			"built.ts":       "export const built = 1;\n",
			"pnpm-lock.yaml": "lockfileVersion: 1\n",
		}),
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
			if enumerateError != nil {
				test.Fatal(enumerateError)
			}
			if enumeration.IgnoredByLayer[".gitignore"] != 1 || enumeration.IgnoredByLayer[HouseIgnoreLayer] != 1 {
				test.Fatalf(
					"layers %v, want built.ts by .gitignore and pnpm-lock.yaml by %s",
					enumeration.IgnoredByLayer,
					HouseIgnoreLayer,
				)
			}
			for _, file := range enumeration.Files {
				if base := filepath.Base(file); base == "pnpm-lock.yaml" || base == "built.ts" {
					test.Errorf("offered %s", file)
				}
			}
		})
	}
}

// TestIgnorePatternsReadAsLintReadsThem: the shared list is lint's globs, relative to the settings
// file's directory even when the walk starts below it, and a glob prunes a directory only when it
// takes everything under it. `**/*.code.js` must not prune a directory that happens to match it.
func TestIgnorePatternsReadAsLintReadsThem(test *testing.T) {
	test.Parallel()
	root := settingsTree(test, `[]`, `["source/generated/**", "**/*.code.js"]`, map[string]string{
		"source/a.ts":                "export const a = 1;\n",
		"source/generated/schema.ts": "export const schema = 1;\n",
		"source/bundle.code.js":      "export const bundle = 1;\n",
		"source/odd.code.js/keep.ts": "export const keep = 1;\n",
	})

	enumeration, enumerateError := Enumerate(filepath.Join(root, "source"), handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	var offered []string
	for _, file := range enumeration.Files {
		relative, relativeError := filepath.Rel(root, file)
		if relativeError != nil {
			test.Fatalf("naming %s under %s: %v", file, root, relativeError)
		}
		offered = append(offered, filepath.ToSlash(relative))
	}
	sort.Strings(offered)
	if strings.Join(offered, ",") != "source/a.ts,source/odd.code.js/keep.ts" {
		test.Errorf("offered %v, want source/a.ts and source/odd.code.js/keep.ts", offered)
	}
	if enumeration.IgnoredByLayer[IgnorePatternsLayer] != 2 {
		test.Errorf(
			"ignorePatterns removed %d, want 2 (the generated directory, pruned whole, and the bundle)",
			enumeration.IgnoredByLayer[IgnorePatternsLayer],
		)
	}
}

// handlesEveryLanguage stands in for a formatter's Handles: the walk takes any file-type predicate, so
// its tests need the languages a formatter speaks, not a formatter.
func handlesEveryLanguage(path string) bool {
	switch filepath.Ext(path) {
	case ".ts", ".tsx", ".js", ".json", ".css", ".md", ".graphql", ".yaml":
		return true
	}
	return false
}

// NestedRepositoryContaining names the outermost repository of its own below the root, the one the
// format walk skips and a run in the parent would have to name. Two levels deep, because one level
// cannot tell the outermost from the nearest; and outside the root, and in the root's own repository,
// it names nothing.
func TestNestedRepositoryContainingNamesTheOutermostBelowTheRoot(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	for _, directory := range []string{
		filepath.Join(root, ".git"),
		filepath.Join(root, "libraries", "structure", ".git"),
		filepath.Join(root, "libraries", "structure", "libraries", "nexus"),
		filepath.Join(root, "source"),
	} {
		if createDirectoryError := os.MkdirAll(directory, 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
	}
	// The inner repository is a submodule, whose `.git` is a gitlink file rather than a directory.
	gitlink := filepath.Join(root, "libraries", "structure", "libraries", "nexus", ".git")
	if writeError := os.WriteFile(gitlink, []byte("gitdir: ../../.git/modules/nexus\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}

	cases := []struct {
		fileName string
		want     string
	}{
		{
			filepath.Join(root, "libraries", "structure", "libraries", "nexus", "source", "Thing.ts"),
			filepath.Join("libraries", "structure"),
		},
		{
			filepath.Join(root, "libraries", "structure", "source", "Button.tsx"),
			filepath.Join("libraries", "structure"),
		},
		{filepath.Join(root, "source", "Own.ts"), ""},
		{filepath.Join(root, "Root.ts"), ""},
		{filepath.Join(filepath.Dir(root), "Elsewhere.ts"), ""},
	}
	for _, testCase := range cases {
		if got := NestedRepositoryContaining(root, testCase.fileName); got != testCase.want {
			test.Errorf("NestedRepositoryContaining(%s) = %q, want %q", testCase.fileName, got, testCase.want)
		}
	}

	if !HasOwnRepository(filepath.Dir(gitlink)) {
		test.Error("a submodule's gitlink file was not recognized as a repository of its own")
	}
}

// Ignore patterns cover the paths they name at any depth, not only at the top. On Windows the walk's
// relative paths came back with `\`, and every pattern that tests a `/` matched only top-level paths:
// a nested `dist`, `.next/` or `modules/*/data/` was walked and formatted. The walk now hands the
// matcher slash paths on every platform. This holds the nested cases, so the Windows leg runs it where
// the separator differs.
func TestIgnorePatternsCoverNestedPaths(test *testing.T) {
	test.Parallel()
	root := writeTree(test, map[string]string{
		".gitignore":                   "dist\n.next/\nmodules/*/data/\nsrc/*.gen.ts\n*.log\n",
		"kept.ts":                      "export const kept = 1;\n",
		"packages/web/dist/out.ts":     "export const out = 1;\n",
		"apps/site/.next/page.ts":      "export const page = 1;\n",
		"modules/tasks/data/huge.json": "{}\n",
		"modules/tasks/source/Task.ts": "export const task = 1;\n",
		"src/model.gen.ts":             "export const generated = 1;\n",
		"src/deep/model.gen.ts":        "export const deeper = 1;\n",
		"logs/nested/run.log":          "line\n",
	})

	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	survivors := []string{}
	for _, file := range enumeration.Files {
		relative, relativeError := filepath.Rel(root, file)
		if relativeError != nil {
			test.Fatalf("naming %s under %s: %v", file, root, relativeError)
		}
		survivors = append(survivors, filepath.ToSlash(relative))
	}
	sort.Strings(survivors)
	// `src/*.gen.ts` names one directory, as gitignore reads a pattern holding a slash, so the deeper
	// file is kept.
	want := []string{"kept.ts", "modules/tasks/source/Task.ts", "src/deep/model.gen.ts"}
	if strings.Join(survivors, " ") != strings.Join(want, " ") {
		test.Errorf("survivors = %v, want %v", survivors, want)
	}
}

// TestNestedRepositoriesAreFoundUnderAPathTheProjectNeverFormats: ahra lists projects/** in its
// ignorePatterns, and the repositories under projects/ are still corpora a harness measures. Discovery
// reads only .gitignore, and checks a directory for a repository before pruning it, so a gitignored
// repository is found too; a gitignored plain directory is not descended.
func TestNestedRepositoriesAreFoundUnderAPathTheProjectNeverFormats(test *testing.T) {
	test.Parallel()
	root := settingsTree(test, `["pnpm-lock.yaml"]`, `["projects/**"]`, map[string]string{
		".gitignore":                      "projects/ignored-repo/\nprojects/scratch/\n",
		"a.ts":                            "export const a = 1;\n",
		"projects/listed/.git/HEAD":       "ref: refs/heads/main\n",
		"projects/listed/b.ts":            "export const b = 1;\n",
		"projects/ignored-repo/.git":      "gitdir: elsewhere\n",
		"projects/scratch/deep/.git/HEAD": "ref: refs/heads/main\n",
	})
	nested, nestedRepositoriesBelowError := NestedRepositoriesBelow(root)
	if nestedRepositoriesBelowError != nil {
		test.Fatal(nestedRepositoriesBelowError)
	}
	sort.Strings(nested)
	if strings.Join(nested, ",") != "projects/ignored-repo,projects/listed" {
		test.Errorf(
			"found %v, want projects/ignored-repo and projects/listed, and nothing below a gitignored plain directory",
			nested,
		)
	}

	// The host's own walk still offers nothing under projects/, and finds no repository there either.
	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatal(enumerateError)
	}
	for _, file := range enumeration.Files {
		if strings.Contains(filepath.ToSlash(file), "/projects/") {
			test.Errorf("the host's walk offered %s, under its own projects/**", file)
		}
	}
}

// TestANestedRepositoryDoesNotInheritItsHostsIgnorePatterns: a repository with no settings of its own
// takes its host's options, but not its host's ignorePatterns, which describe the host's tree. Walked as
// its own corpus, projects/listed must offer its files although the host lists projects/**.
func TestANestedRepositoryDoesNotInheritItsHostsIgnorePatterns(test *testing.T) {
	test.Parallel()
	root := settingsTree(test, `["pnpm-lock.yaml"]`, `["projects/**"]`, map[string]string{
		"projects/listed/.git/HEAD":      "ref: refs/heads/main\n",
		"projects/listed/b.ts":           "export const b = 1;\n",
		"projects/listed/pnpm-lock.yaml": "lockfileVersion: 1\n",
	})
	enumeration, enumerateError := Enumerate(filepath.Join(root, "projects", "listed"), handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatal(enumerateError)
	}
	if len(enumeration.Files) != 1 || filepath.Base(enumeration.Files[0]) != "b.ts" {
		test.Errorf(
			"offered %v, want b.ts alone: the host's ignorePatterns do not reach in, its house list does",
			enumeration.Files,
		)
	}
	if _, counted := enumeration.IgnoredByLayer[IgnorePatternsLayer]; counted {
		test.Error("the host's ignorePatterns were read as a layer of the nested repository's walk")
	}
	if enumeration.IgnoredByLayer[HouseIgnoreLayer] != 1 {
		test.Errorf("the house list removed %d, want pnpm-lock.yaml", enumeration.IgnoredByLayer[HouseIgnoreLayer])
	}
}

// survivorsOf is an enumeration's files, relative to root and slash separated.
func survivorsOf(root string, enumeration Enumeration) map[string]bool {
	survivors := map[string]bool{}
	for _, file := range enumeration.Files {
		// Ignored: Enumerate names each file by its absolute path under root, so Rel always finds one.
		relative, _ := filepath.Rel(root, file)
		survivors[filepath.ToSlash(relative)] = true
	}
	return survivors
}

// TestTheWalkReadsGitsIgnoreRulesAsGitDoes: every .gitignore from the root down, each scoped to its own
// directory, negation, and info/exclude, all counted under the one git layer (#ndtgy1w). A file below an
// excluded directory stays out whatever a deeper negation says, as git never looks inside the directory.
func TestTheWalkReadsGitsIgnoreRulesAsGitDoes(test *testing.T) {
	test.Parallel()
	root := settingsTree(test, `[]`, `[]`, map[string]string{
		".git/info/exclude":   "local.ts\n",
		".gitignore":          "*.gen.ts\n!keep.gen.ts\nvendor/\n",
		"a.ts":                "export const a = 1;\n",
		"local.ts":            "export const local = 1;\n",
		"x.gen.ts":            "export const x = 1;\n",
		"keep.gen.ts":         "export const keep = 1;\n",
		"sub/.gitignore":      "!*.gen.ts\nscratch.ts\n",
		"sub/y.gen.ts":        "export const y = 1;\n",
		"sub/scratch.ts":      "export const scratch = 1;\n",
		"sub/deep/scratch.ts": "export const deep = 1;\n",
		"vendor/.gitignore":   "!lib.ts\n",
		"vendor/lib.ts":       "export const lib = 1;\n",
	})
	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	survivors := survivorsOf(root, enumeration)
	for name, want := range map[string]bool{
		"a.ts":                true,
		"keep.gen.ts":         true,
		"sub/y.gen.ts":        true,
		"local.ts":            false,
		"x.gen.ts":            false,
		"sub/scratch.ts":      false,
		"sub/deep/scratch.ts": false,
		"vendor/lib.ts":       false,
	} {
		if survivors[name] != want {
			test.Errorf("%s offered %v, want %v (offered: %v)", name, survivors[name], want, enumeration.Files)
		}
	}
	// local.ts, x.gen.ts, sub/scratch.ts, sub/deep/scratch.ts and the vendor directory.
	if got := enumeration.IgnoredByLayer[GitignoreLayer]; got != 5 {
		test.Errorf("the git layer removed %d, want 5", got)
	}
}

// TestIgnoreFilesNamesEveryFileTheWalkReadOrLookedFor pins the list a cache replaying the walk reads
// (#z661dek): the .gitignore of the root and of every directory entered, present or not, info/exclude
// when .git is a directory, present or not, and the index, whose tracked paths are never ignored, present
// or not. A pruned directory's .gitignore is never read, so not listed.
func TestIgnoreFilesNamesEveryFileTheWalkReadOrLookedFor(test *testing.T) {
	test.Parallel()
	root := settingsTree(test, `[]`, `[]`, map[string]string{
		".git/HEAD":         "ref: refs/heads/main\n",
		".gitignore":        "pruned/\n",
		"a.ts":              "export const a = 1;\n",
		"with/.gitignore":   "x.ts\n",
		"with/b.ts":         "export const b = 1;\n",
		"without/c.ts":      "export const c = 1;\n",
		"pruned/.gitignore": "y.ts\n",
		"pruned/d.ts":       "export const d = 1;\n",
		"nested/.git":       "gitdir: ../.git/modules/nested\n",
		"nested/.gitignore": "z.ts\n",
		"nested/e.ts":       "export const e = 1;\n",
	})
	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	var got []string
	for _, file := range enumeration.IgnoreFiles {
		relative, relativeError := filepath.Rel(root, file)
		if relativeError != nil {
			test.Fatalf("naming %s under %s: %v", file, root, relativeError)
		}
		got = append(got, filepath.ToSlash(relative))
	}
	sort.Strings(got)
	// nexus/ is the settings tier the fixture writes, a directory the walk enters like any other.
	want := []string{
		".git/index",
		".git/info/exclude",
		".gitignore",
		"nexus/.gitignore",
		"with/.gitignore",
		"without/.gitignore",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		test.Errorf("IgnoreFiles %v, want %v", got, want)
	}
}

// TestAWorktreeWalkReadsAndNamesTheSharedExclude: a worktree's `.git` is a file pointing into its main checkout's git
// directory, whose common directory holds the info/exclude git reads for it (#vdbcswp). The walk skips what that file
// lists, and names the file among IgnoreFiles by its path in the common directory, so a replay sees an edit to it.
func TestAWorktreeWalkReadsAndNamesTheSharedExclude(test *testing.T) {
	test.Parallel()
	main := writeTree(test, map[string]string{
		".git/info/exclude":             "secret.ts\n",
		".git/worktrees/tree/commondir": "../..\n",
	})
	gitDirectory := filepath.Join(main, ".git", "worktrees", "tree")
	root := settingsTree(test, `[]`, `[]`, map[string]string{
		".git":      "gitdir: " + gitDirectory + "\n",
		"a.ts":      "export const a = 1;\n",
		"secret.ts": "export const secret = 1;\n",
	})
	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	shared := filepath.Join(main, ".git", "info", "exclude")
	if !slices.Contains(enumeration.IgnoreFiles, shared) {
		test.Errorf("IgnoreFiles %v does not name the shared exclude %s", enumeration.IgnoreFiles, shared)
	}
	if slices.Contains(enumeration.Files, filepath.Join(root, "secret.ts")) {
		test.Errorf("the walk offered secret.ts, which the shared exclude lists: %v", enumeration.Files)
	}
	if !slices.Contains(enumeration.Files, filepath.Join(root, "a.ts")) {
		test.Errorf("the walk did not offer a.ts, so the case proves nothing: %v", enumeration.Files)
	}
}

// TestTheHouseListReadsAsIgnoreFileLines: the format block's `ignore` is read with git's syntax relative to
// the walk root, negation and anchoring included, and counted as its own layer.
func TestTheHouseListReadsAsIgnoreFileLines(test *testing.T) {
	test.Parallel()
	root := settingsTree(test, `["*.json", "!keep.json", "/top.ts"]`, `[]`, map[string]string{
		"a.json":     "{}\n",
		"keep.json":  "{}\n",
		"top.ts":     "export const top = 1;\n",
		"sub/top.ts": "export const subTop = 1;\n",
		"sub/b.json": "{}\n",
	})
	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	survivors := survivorsOf(root, enumeration)
	for name, want := range map[string]bool{
		"keep.json":  true,
		"sub/top.ts": true,
		"a.json":     false,
		"top.ts":     false,
		"sub/b.json": false,
	} {
		if survivors[name] != want {
			test.Errorf("%s offered %v, want %v (offered: %v)", name, survivors[name], want, enumeration.Files)
		}
	}
}

// An Adamic `.a` file is held back from Files whatever the engine handles, after every ignore layer has had
// its say (#6mhafvb). The walk cannot tell Adamic source from a static library, so it never offers one; the run
// offers a held `.a` once the program claims it. Every handles predicate is told yes, so a `.a` in Files would be
// the walk ignoring the hold, and the subtraction still balances with the held files counted.
func TestEnumerateHoldsAdamicFilesBack(test *testing.T) {
	test.Parallel()
	// Ignored by name rather than by directory, so every ignore counted is a file the walk counted too.
	root := settingsTree(test, "", `["archived/old.a"]`, map[string]string{
		".gitignore":        "build/output.a\n",
		"a.ts":              "export const a = 1;\n",
		"geometry.a":        "export const area = 1;\n",
		"lib/libfoo.a":      "!<arch>\n",
		"build/output.a":    "!<arch>\n",
		"archived/old.a":    "export const old = 1;\n",
		"data.archive":      "not adamic\n",
		"nested/deeper.a":   "export const deeper = 1;\n",
		"nested/Thing.d.ts": "export {};\n",
	})
	if symlinkError := os.Symlink(
		filepath.Join(root, "geometry.a"),
		filepath.Join(root, "link.a"),
	); symlinkError != nil {
		test.Fatal(symlinkError)
	}

	enumeration, enumerateError := Enumerate(root, func(string) bool { return true })
	if enumerateError != nil {
		test.Fatalf("enumerate: %v", enumerateError)
	}
	for file := range survivorsOf(root, enumeration) {
		if strings.HasSuffix(file, ".a") {
			test.Errorf("%s was offered as a file, before any program claimed it", file)
		}
	}
	held := []string{}
	for _, file := range enumeration.Adamic {
		relative, relativeError := filepath.Rel(root, file)
		if relativeError != nil {
			test.Fatalf("naming %s under %s: %v", file, root, relativeError)
		}
		held = append(held, filepath.ToSlash(relative))
	}
	sort.Strings(held)
	if want := []string{
		"geometry.a",
		"lib/libfoo.a",
		"nested/deeper.a",
	}; strings.Join(held, " ") != strings.Join(want, " ") {
		test.Errorf("held back %v, want %v: the ignored and the linked stay out", held, want)
	}
	ignored := 0
	for _, count := range enumeration.IgnoredByLayer {
		ignored += count
	}
	if offered := enumeration.Walked - ignored - enumeration.Unhandled - enumeration.SymbolicLinks; offered != len(
		enumeration.Files,
	)+len(enumeration.Adamic) {
		test.Errorf(
			"walked %d less %d ignored, %d unhandled and %d links is %d, but %d files and %d held back",
			enumeration.Walked,
			ignored,
			enumeration.Unhandled,
			enumeration.SymbolicLinks,
			offered,
			len(enumeration.Files),
			len(enumeration.Adamic),
		)
	}
}

// Go's testdata is never entered, at any depth, and each one skipped is counted under its own layer, so a fixture
// kept unformatted on purpose is never offered and the skip still shows (#4wjhppw). The control is a directory
// named like it, which is entered.
func TestTheWalkNeverEntersTestdata(test *testing.T) {
	test.Parallel()
	root := writeTree(test, map[string]string{
		"README.md":                         "# root\n",
		"testdata/upstream.json":            "{ }\n",
		"internal/printer/testdata/a.md":    "#a\n",
		"internal/printer/testdata_note.md": "# kept\n",
		"internal/printer/notes/b.json":     "{}\n",
	})
	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatal(enumerateError)
	}
	want := map[string]bool{
		"README.md":                         true,
		"internal/printer/testdata_note.md": true,
		"internal/printer/notes/b.json":     true,
	}
	if survivors := survivorsOf(root, enumeration); !maps.Equal(survivors, want) {
		test.Errorf("offered %v, want %v", survivors, want)
	}
	if skipped := enumeration.IgnoredByLayer[FixturesLayer]; skipped != 2 {
		test.Errorf("%d testdata directories counted, want 2", skipped)
	}
}

// A directory cohere never enters is never entered by the format walk either, though no ignore file names it, so
// cohere's own scratch in a repository whose .gitignore leaves `.cache` out is never judged (#fe4vr5s): ahraos-macos
// read 29 files that would change after a Swift run, `.cache/cohere/swift/package-description.json` among them. Each
// skip is counted under its name. The control is a directory named like one, which is entered.
func TestTheWalkNeverEntersWhatDiscoveryNeverEnters(test *testing.T) {
	test.Parallel()
	root := writeTree(test, map[string]string{
		"README.md":                              "# root\n",
		".cache/cohere/x.json":                   "{\"unformatted\":   1}\n",
		"Packages/Kit/.cache/cohere/y.json":      "{ }\n",
		"node_modules/package/index.js":          "x\n",
		".build/checkouts/README.md":             "#b\n",
		"Sources/App/.cache-notes/kept.json":     "{}\n",
		"Sources/App/Resources/Assets/icon.json": "{}\n",
	})
	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatal(enumerateError)
	}
	want := map[string]bool{
		"README.md":                              true,
		"Sources/App/.cache-notes/kept.json":     true,
		"Sources/App/Resources/Assets/icon.json": true,
	}
	if survivors := survivorsOf(root, enumeration); !maps.Equal(survivors, want) {
		test.Errorf("offered %v, want %v", survivors, want)
	}
	for name, count := range map[string]int{".cache": 2, "node_modules": 1, ".build": 1} {
		if enumeration.IgnoredByLayer[name] != count {
			test.Errorf("%d %s directories counted, want %d", enumeration.IgnoredByLayer[name], name, count)
		}
	}
}

// A directory named Vendor, at any depth, is someone else's code to the Swift engine, so the Format walk never enters
// it either (#fe4vr5s): at ahraos-macos the Format project read SwiftTerm's README.md and Contents.json under
// Vendor/SwiftTerm while the Swift run there excluded all 156 of its Swift files as vendored. The control is a
// directory named like it, which is entered.
func TestTheWalkNeverEntersAVendorDirectory(test *testing.T) {
	test.Parallel()
	root := writeTree(test, map[string]string{
		"README.md":                    "# root\n",
		"Vendor/SwiftTerm/README.md":   "#upstream\n",
		"App/Vendor/Kit/Contents.json": "{ }\n",
		"App/VendorNotes/kept.md":      "# kept\n",
	})
	enumeration, enumerateError := Enumerate(root, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatal(enumerateError)
	}
	want := map[string]bool{"README.md": true, "App/VendorNotes/kept.md": true}
	if survivors := survivorsOf(root, enumeration); !maps.Equal(survivors, want) {
		test.Errorf("offered %v, want %v", survivors, want)
	}
	if skipped := enumeration.IgnoredByLayer[VendoredLayer]; skipped != 2 {
		test.Errorf("%d Vendor directories counted, want 2", skipped)
	}
}
