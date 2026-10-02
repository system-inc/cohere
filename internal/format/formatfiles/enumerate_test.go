package formatfiles

import (
	"os"
	"path/filepath"
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

// TestEnumerateAppliesEveryIgnoreLayer proves each layer is read and counted separately.
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

// handlesEveryLanguage stands in for a formatter's Handles: the walk takes any file-type predicate, so
// its tests need the languages a formatter speaks, not a formatter.
func handlesEveryLanguage(path string) bool {
	switch filepath.Ext(path) {
	case ".ts", ".tsx", ".js", ".json", ".css", ".md", ".graphql", ".yaml":
		return true
	}
	return false
}
