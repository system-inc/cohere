package formatfiles

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeTree materializes a fixture project and returns its root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, contents := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

// TestEnumerateOffersEveryLanguage is the positive control the ruling asked for: a project with a
// file in each language the engine speaks, proving each one is offered rather than only TypeScript.
//
// This is the whole point of the change. The pipeline's universe was the type graph, so a .css in a
// TypeScript project was not declined, it was absent, and absence and agreement print the same.
func TestEnumerateOffersEveryLanguage(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.ts":      "export const a = 1;\n",
		"b.tsx":     "export const b = <div />;\n",
		"c.css":     ".c { color: red; }\n",
		"d.md":      "# d\n",
		"e.json":    "{ \"e\": 1 }\n",
		"f.graphql": "type F { g: Int }\n",
	})

	enumeration, err := Enumerate(root, "", handlesEveryLanguage)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if len(enumeration.Files) != 6 {
		t.Fatalf("offered %d files, want 6: %v", len(enumeration.Files), enumeration.Files)
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
			t.Errorf("%s was not offered", name)
		}
	}
}

// TestEnumerateNamesWhatItDeclined is the negative control, and it is the one that matters.
//
// A file the engine cannot format must come back as a named zero rather than as an absence. A clean
// enumeration and a vacuous one look identical from the outside, so the report has to say what it
// refused and why, or the coverage line inherits the same blindness it was built to destroy.
func TestEnumerateNamesWhatItDeclined(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.ts":      "export const a = 1;\n",
		"tool.go":   "package tool\n",
		"script.rb": "puts 1\n",
		"README":    "no extension\n",
	})

	enumeration, err := Enumerate(root, "", handlesEveryLanguage)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if enumeration.Unhandled != 3 {
		t.Errorf("Unhandled = %d, want 3", enumeration.Unhandled)
	}
	for extension, want := range map[string]int{".go": 1, ".rb": 1, "(none)": 1} {
		if enumeration.DeclinedExtensions[extension] != want {
			t.Errorf("DeclinedExtensions[%q] = %d, want %d",
				extension, enumeration.DeclinedExtensions[extension], want)
		}
	}
	// The subtraction has to reconcile, or a file went missing between the walk and the result.
	accounted := len(enumeration.Files) + enumeration.Unhandled
	for _, removed := range enumeration.IgnoredByLayer {
		accounted += removed
	}
	if accounted != enumeration.Walked {
		t.Errorf("walked %d but accounted for %d, so files went missing silently",
			enumeration.Walked, accounted)
	}
}

// TestEnumerateAppliesEveryIgnoreLayer proves each layer is read and counted separately, as the walk
// reads them before a Nexus tier declares the house list: the two old files are still layers then.
//
// Counted separately because a layer that silently fails to load removes nothing, and a slightly
// smaller total is not a detectable signal. A zero next to a layer name is.
func TestEnumerateAppliesEveryIgnoreLayer(t *testing.T) {
	root := writeTree(t, map[string]string{
		".gitignore":      "built.ts\n",
		".prettierignore": "archived/\n",
		"a.ts":            "export const a = 1;\n",
		"built.ts":        "export const built = 1;\n",
		"archived/old.md": "# old\n",
		"pnpm-lock.yaml":  "lockfileVersion: 1\n",
	})
	structureIgnore := filepath.Join(root, "PrettierIgnoreDefaults")
	if err := os.WriteFile(structureIgnore, []byte("pnpm-lock.yaml\n"), 0o644); err != nil {
		t.Fatalf("write structure ignore: %v", err)
	}

	enumeration, err := Enumerate(root, structureIgnore, handlesEveryLanguage)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	for layer, want := range map[string]int{
		".gitignore": 1, "PrettierIgnoreDefaults": 1, ".prettierignore": 1,
	} {
		if enumeration.IgnoredByLayer[layer] != want {
			t.Errorf("layer %s removed %d, want %d", layer, enumeration.IgnoredByLayer[layer], want)
		}
	}
	if len(enumeration.Files) != 1 || filepath.Base(enumeration.Files[0]) != "a.ts" {
		t.Errorf("survivors = %v, want just a.ts", enumeration.Files)
	}
}

// TestEnumerateRefusesNestedRepositories proves the walk does not descend into somebody else's tree.
//
// The ignore layers do not cover this and cannot: a submodule is tracked by the parent as a gitlink
// rather than as ignored paths, so nothing in .gitignore or .prettierignore names it. Verified
// against the real repository -- neither file mentions libraries/structure.
//
// The cost of not having this is measured rather than hypothetical. A whole-tree run tonight wrote
// a line into a submodule; the change was correct and still unrequested, which is the worst shape
// for a surprise because it survives review.
func TestEnumerateRefusesNestedRepositories(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.ts":             "export const a = 1;\n",
		"vendor/.git/HEAD": "ref: refs/heads/main\n",
		"vendor/theirs.ts": "export const theirs = 1;\n",
		"vendor/deep/x.md": "# x\n",
	})

	enumeration, err := Enumerate(root, "", handlesEveryLanguage)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	for _, file := range enumeration.Files {
		if filepath.Base(file) == "theirs.ts" || filepath.Base(file) == "x.md" {
			t.Errorf("descended into a nested repository and offered %s", file)
		}
	}
	if len(enumeration.NestedRepositories) != 1 || enumeration.NestedRepositories[0] != "vendor" {
		t.Errorf("NestedRepositories = %v, want [vendor]", enumeration.NestedRepositories)
	}
	if len(enumeration.Files) != 1 {
		t.Errorf("offered %v, want just a.ts", enumeration.Files)
	}
}

// TestAMissingStructureLayerIsNamed: a named layer that is not there must say so. Read as an empty
// file, it removed nothing and the summary hid the zero, which is how the defaults moving in August
// left every walk offering pnpm-lock.yaml until October.
func TestAMissingStructureLayerIsNamed(t *testing.T) {
	root := t.TempDir()
	missing := StructureIgnorePath(root)
	enumeration, err := Enumerate(root, missing, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(enumeration.MissingLayers) != 1 || enumeration.MissingLayers[0] != missing {
		t.Fatalf("missing layers %v, want %s named", enumeration.MissingLayers, missing)
	}

	if err := os.MkdirAll(filepath.Dir(missing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(missing, []byte("pnpm-lock.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	enumeration, err = Enumerate(root, missing, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(enumeration.MissingLayers) != 0 {
		t.Fatalf("a present layer was named missing: %v", enumeration.MissingLayers)
	}
}

// settingsTree writes a repository whose CohereSettings.json extends a Nexus tier, with the format
// block's `ignore` (omitted when house is empty) and the project's own ignorePatterns.
func settingsTree(t *testing.T, house string, ignorePatterns string, files map[string]string) string {
	t.Helper()
	block := `{"tabWidth": 4}`
	if house != "" {
		block = `{"tabWidth": 4, "ignore": ` + house + `}`
	}
	files["nexus/NexusCohereSettings.json"] = `{"rules": {}, "format": ` + block + `}`
	files["CohereSettings.json"] = `{"extends": "./nexus/NexusCohereSettings.json", "rules": {}, "ignorePatterns": ` + ignorePatterns + `}`
	return writeTree(t, files)
}

// TestTheHouseListRetiresTheOldFiles is the walk once the Nexus tier declares its list: `.gitignore`,
// then the format block's `ignore`, then the project's ignorePatterns, each counted under its own name,
// and the two old files counted nowhere because they no longer remove anything themselves.
func TestTheHouseListRetiresTheOldFiles(t *testing.T) {
	root := settingsTree(t, `["pnpm-lock.yaml"]`, `["archived/**"]`, map[string]string{
		".gitignore":      "built.ts\n",
		".prettierignore": "archived/\n",
		"a.ts":            "export const a = 1;\n",
		"built.ts":        "export const built = 1;\n",
		"archived/old.md": "# old\n",
		"pnpm-lock.yaml":  "lockfileVersion: 1\n",
		"defaults":        "pnpm-lock.yaml\n",
	})

	enumeration, err := Enumerate(root, filepath.Join(root, "defaults"), handlesEveryLanguage)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	for layer, want := range map[string]int{".gitignore": 1, HouseIgnoreLayer: 1, IgnorePatternsLayer: 1} {
		if enumeration.IgnoredByLayer[layer] != want {
			t.Errorf("layer %s removed %d, want %d", layer, enumeration.IgnoredByLayer[layer], want)
		}
	}
	for _, retired := range []string{"PrettierIgnoreDefaults", ".prettierignore"} {
		if _, counted := enumeration.IgnoredByLayer[retired]; counted {
			t.Errorf("%s is still a layer once the house list is declared", retired)
		}
	}
	survivors := map[string]bool{}
	for _, file := range enumeration.Files {
		relative, _ := filepath.Rel(root, file)
		survivors[filepath.ToSlash(relative)] = true
	}
	if !survivors["a.ts"] || survivors["archived/old.md"] || survivors["pnpm-lock.yaml"] || survivors["built.ts"] {
		t.Errorf("survivors = %v, want a.ts and the settings files only", enumeration.Files)
	}
}

// TestARetiringFileThatDisagreesIsRefused: once the house list is declared, an old file that would skip
// a file the lists offer is refused, naming the file, its pattern and the path, rather than read as a
// layer that quietly keeps a rule nobody moved. Both old files, one at a time.
func TestARetiringFileThatDisagreesIsRefused(t *testing.T) {
	for _, testCase := range []struct {
		name, house, ignorePatterns, oldFile, pattern, path string
	}{
		{"the project's .prettierignore", `["pnpm-lock.yaml"]`, `[]`, ".prettierignore", "archived/", "archived/old.md"},
		{"Structure's defaults", `[]`, `["archived/**"]`, "defaults", "pnpm-lock.yaml", "pnpm-lock.yaml"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := settingsTree(t, testCase.house, testCase.ignorePatterns, map[string]string{
				".prettierignore": "archived/\n",
				"defaults":        "pnpm-lock.yaml\n",
				"a.ts":            "export const a = 1;\n",
				"archived/old.md": "# old\n",
				"pnpm-lock.yaml":  "lockfileVersion: 1\n",
			})
			_, err := Enumerate(root, filepath.Join(root, "defaults"), handlesEveryLanguage)
			if err == nil {
				t.Fatal("a retiring file that skips more than the lists was accepted")
			}
			for _, want := range []string{filepath.Join(root, testCase.oldFile), `"` + testCase.pattern + `"`, testCase.path} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not name %s", err, want)
				}
			}
		})
	}
}

// TestIgnorePatternsReadAsLintReadsThem: the shared list is lint's globs, relative to the settings
// file's directory even when the walk starts below it, and a glob prunes a directory only when it
// takes everything under it. `**/*.code.js` must not prune a directory that happens to match it.
func TestIgnorePatternsReadAsLintReadsThem(t *testing.T) {
	root := settingsTree(t, `[]`, `["source/generated/**", "**/*.code.js"]`, map[string]string{
		"source/a.ts":                "export const a = 1;\n",
		"source/generated/schema.ts": "export const schema = 1;\n",
		"source/bundle.code.js":      "export const bundle = 1;\n",
		"source/odd.code.js/keep.ts": "export const keep = 1;\n",
	})

	enumeration, err := Enumerate(filepath.Join(root, "source"), "", handlesEveryLanguage)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	var offered []string
	for _, file := range enumeration.Files {
		relative, _ := filepath.Rel(root, file)
		offered = append(offered, filepath.ToSlash(relative))
	}
	sort.Strings(offered)
	if strings.Join(offered, ",") != "source/a.ts,source/odd.code.js/keep.ts" {
		t.Errorf("offered %v, want source/a.ts and source/odd.code.js/keep.ts", offered)
	}
	if enumeration.IgnoredByLayer[IgnorePatternsLayer] != 2 {
		t.Errorf("ignorePatterns removed %d, want 2 (the generated directory, pruned whole, and the bundle)", enumeration.IgnoredByLayer[IgnorePatternsLayer])
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
func TestNestedRepositoryContainingNamesTheOutermostBelowTheRoot(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{
		filepath.Join(root, ".git"),
		filepath.Join(root, "libraries", "structure", ".git"),
		filepath.Join(root, "libraries", "structure", "libraries", "nexus"),
		filepath.Join(root, "source"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The inner repository is a submodule, whose `.git` is a gitlink file rather than a directory.
	gitlink := filepath.Join(root, "libraries", "structure", "libraries", "nexus", ".git")
	if err := os.WriteFile(gitlink, []byte("gitdir: ../../.git/modules/nexus\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		fileName string
		want     string
	}{
		{filepath.Join(root, "libraries", "structure", "libraries", "nexus", "source", "Thing.ts"), filepath.Join("libraries", "structure")},
		{filepath.Join(root, "libraries", "structure", "source", "Button.tsx"), filepath.Join("libraries", "structure")},
		{filepath.Join(root, "source", "Own.ts"), ""},
		{filepath.Join(root, "Root.ts"), ""},
		{filepath.Join(filepath.Dir(root), "Elsewhere.ts"), ""},
	}
	for _, testCase := range cases {
		if got := NestedRepositoryContaining(root, testCase.fileName); got != testCase.want {
			t.Errorf("NestedRepositoryContaining(%s) = %q, want %q", testCase.fileName, got, testCase.want)
		}
	}

	if !HasOwnRepository(filepath.Dir(gitlink)) {
		t.Error("a submodule's gitlink file was not recognized as a repository of its own")
	}
}

// Ignore patterns cover the paths they name at any depth, not only at the top. On Windows the walk's
// relative paths came back with `\`, and every pattern that tests a `/` matched only top-level paths:
// a nested `dist`, `.next/` or `modules/*/data/` was walked and formatted. The walk now hands the
// matcher slash paths on every platform. This holds the nested cases, so the Windows leg runs it where
// the separator differs.
func TestIgnorePatternsCoverNestedPaths(t *testing.T) {
	root := writeTree(t, map[string]string{
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

	enumeration, err := Enumerate(root, "", handlesEveryLanguage)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	survivors := []string{}
	for _, file := range enumeration.Files {
		relative, _ := filepath.Rel(root, file)
		survivors = append(survivors, filepath.ToSlash(relative))
	}
	sort.Strings(survivors)
	// `src/*.gen.ts` names one directory, as gitignore reads a pattern holding a slash, so the deeper
	// file is kept.
	want := []string{"kept.ts", "modules/tasks/source/Task.ts", "src/deep/model.gen.ts"}
	if strings.Join(survivors, " ") != strings.Join(want, " ") {
		t.Errorf("survivors = %v, want %v", survivors, want)
	}
}

// TestNestedRepositoriesAreFoundUnderAPathTheProjectNeverFormats: ahra lists projects/** in its
// ignorePatterns, and the repositories under projects/ are still corpora a harness measures. Discovery
// reads only .gitignore, and checks a directory for a repository before pruning it, so a gitignored
// repository is found too; a gitignored plain directory is not descended.
func TestNestedRepositoriesAreFoundUnderAPathTheProjectNeverFormats(t *testing.T) {
	root := settingsTree(t, `["pnpm-lock.yaml"]`, `["projects/**"]`, map[string]string{
		".gitignore":                      "projects/ignored-repo/\nprojects/scratch/\n",
		"a.ts":                            "export const a = 1;\n",
		"projects/listed/.git/HEAD":       "ref: refs/heads/main\n",
		"projects/listed/b.ts":            "export const b = 1;\n",
		"projects/ignored-repo/.git":      "gitdir: elsewhere\n",
		"projects/scratch/deep/.git/HEAD": "ref: refs/heads/main\n",
	})
	nested, err := NestedRepositoriesBelow(root)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(nested)
	if strings.Join(nested, ",") != "projects/ignored-repo,projects/listed" {
		t.Errorf("found %v, want projects/ignored-repo and projects/listed, and nothing below a gitignored plain directory", nested)
	}

	// The host's own walk still offers nothing under projects/, and finds no repository there either.
	enumeration, err := Enumerate(root, "", handlesEveryLanguage)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range enumeration.Files {
		if strings.Contains(filepath.ToSlash(file), "/projects/") {
			t.Errorf("the host's walk offered %s, under its own projects/**", file)
		}
	}
}

// TestANestedRepositoryDoesNotInheritItsHostsIgnorePatterns: a repository with no settings of its own
// takes its host's options, but not its host's ignorePatterns, which describe the host's tree. Walked as
// its own corpus, projects/listed must offer its files although the host lists projects/**.
func TestANestedRepositoryDoesNotInheritItsHostsIgnorePatterns(t *testing.T) {
	root := settingsTree(t, `["pnpm-lock.yaml"]`, `["projects/**"]`, map[string]string{
		"projects/listed/.git/HEAD":      "ref: refs/heads/main\n",
		"projects/listed/b.ts":           "export const b = 1;\n",
		"projects/listed/pnpm-lock.yaml": "lockfileVersion: 1\n",
	})
	enumeration, err := Enumerate(filepath.Join(root, "projects", "listed"), "", handlesEveryLanguage)
	if err != nil {
		t.Fatal(err)
	}
	if len(enumeration.Files) != 1 || filepath.Base(enumeration.Files[0]) != "b.ts" {
		t.Errorf("offered %v, want b.ts alone: the host's ignorePatterns do not reach in, its house list does", enumeration.Files)
	}
	if _, counted := enumeration.IgnoredByLayer[IgnorePatternsLayer]; counted {
		t.Error("the host's ignorePatterns were read as a layer of the nested repository's walk")
	}
	if enumeration.IgnoredByLayer[HouseIgnoreLayer] != 1 {
		t.Errorf("the house list removed %d, want pnpm-lock.yaml", enumeration.IgnoredByLayer[HouseIgnoreLayer])
	}
}
