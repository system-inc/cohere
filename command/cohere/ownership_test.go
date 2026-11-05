package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ownedDiscovery discovers a repository's projects and decides their ownership, as a bare run does.
func ownedDiscovery(test *testing.T, root string) discovery {
	test.Helper()
	found, err := discoverProjects(root, nil)
	if err != nil {
		test.Fatal(err)
	}
	if found.Ownership, err = resolveOwnership(&found); err != nil {
		test.Fatal(err)
	}
	return found
}

// labels are the projects that run, by label.
func (found discovery) labels() []string {
	labels := []string{}
	for _, project := range found.Projects {
		labels = append(labels, projectLabel(project))
	}
	return labels
}

const ownershipOptions = `"compilerOptions":{"strict":true,"noEmit":true}`

// A solution root, `files: []` with references, is not run, and what it references is, whatever the
// tsconfig is named: Vite's template references tsconfig.app.json and tsconfig.node.json beside it. Two
// tsconfigs in one directory share its cache table, so the second runs with the cache off and formats
// nothing, the first formatting the directory. Control: the same root with a file of its own is a project.
func TestASolutionRootIsNotRunAndWhatItReferencesIs(test *testing.T) {
	test.Parallel()
	files := map[string]string{
		"tsconfig.json":               `{"files":[],"references":[{"path":"./tsconfig.app.json"},{"path":"./tsconfig.node.json"},{"path":"./packages/core"}]}`,
		"tsconfig.app.json":           `{` + ownershipOptions + `,"include":["src"]}`,
		"tsconfig.node.json":          `{` + ownershipOptions + `,"include":["vite.config.ts"]}`,
		"src/main.ts":                 "export const main = 1;\n",
		"vite.config.ts":              "export default {};\n",
		"packages/core/tsconfig.json": `{` + ownershipOptions + `,"include":["**/*.ts"]}`,
		"packages/core/index.ts":      "export const core = 1;\n",
	}
	found := ownedDiscovery(test, newRepository(test, files))
	if want := []string{"tsconfig.app.json", "tsconfig.node.json", "packages/core"}; !reflect.DeepEqual(found.labels(), want) {
		test.Fatalf("ran %v, want %v", found.labels(), want)
	}
	if !reflect.DeepEqual(found.Ownership.Solutions, []string{"."}) {
		test.Errorf("solutions %v, want the root", found.Ownership.Solutions)
	}
	if !found.Ownership.SharedDirectory["tsconfig.node.json"] || found.Ownership.SharedDirectory["tsconfig.app.json"] {
		test.Errorf("the second tsconfig in the root should run with the cache off, and only it: %v", found.Ownership.SharedDirectory)
	}
	root := found.Root
	if got := found.Ownership.Yields["tsconfig.node.json"].Directories; !reflect.DeepEqual(got, []string{root}) {
		test.Errorf("the second tsconfig in the root formats %v away, want the root itself", got)
	}
	if got := found.Ownership.Yields["tsconfig.app.json"].Directories; !reflect.DeepEqual(got, []string{filepath.Join(root, "packages", "core")}) {
		test.Errorf("the root's first tsconfig leaves %v to others' format walks, want packages/core", got)
	}

	files["tsconfig.json"] = `{"files":["vite.config.ts"],"references":[{"path":"./packages/core"}]}`
	found = ownedDiscovery(test, newRepository(test, files))
	if len(found.Ownership.Solutions) != 0 || found.labels()[0] != "." {
		test.Fatalf("a root with a file of its own was taken for a solution: ran %v, solutions %v", found.labels(), found.Ownership.Solutions)
	}
}

// A root whose tsconfig covers only config files (TanStack Query's), and a repository with no root
// tsconfig at all (prisma's), each find every project below; one .gitignore ignores is not among them.
func TestNestedProjectsAreFoundUnderAnyRoot(test *testing.T) {
	test.Parallel()
	nested := map[string]string{
		".gitignore":                   "legacy/\n",
		"packages/query/tsconfig.json": `{` + ownershipOptions + `,"include":["src"]}`,
		"packages/query/src/index.ts":  "export const query = 1;\n",
		"packages/react/tsconfig.json": `{` + ownershipOptions + `,"include":["src"]}`,
		"packages/react/src/index.ts":  "export const react = 1;\n",
		"legacy/tsconfig.json":         `{` + ownershipOptions + `,"include":["**/*.ts"]}`,
		"legacy/old.ts":                "export const old = 1;\n",
	}
	configOnly := map[string]string{
		"tsconfig.json":    `{` + ownershipOptions + `,"include":["*.config.ts"]}`,
		"eslint.config.ts": "export default [];\n",
	}
	for name, extra := range map[string]map[string]string{"a config-only root": configOnly, "no root tsconfig": nil} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			files := map[string]string{}
			for path, contents := range nested {
				files[path] = contents
			}
			for path, contents := range extra {
				files[path] = contents
			}
			found := ownedDiscovery(test, newRepository(test, files))
			want := []string{"packages/query", "packages/react"}
			if extra != nil {
				want = append([]string{"."}, want...)
			}
			if !reflect.DeepEqual(found.labels(), want) {
				test.Fatalf("ran %v, want %v", found.labels(), want)
			}
			for label, yield := range found.Ownership.Yields {
				if len(yield.Files) > 0 {
					test.Errorf("%s yields %v, though no file is included twice", label, yield.Files)
				}
			}
		})
	}
}

// A file two tsconfigs include belongs to the nearer, the deepest whose directory holds it, and the other
// yields it; a file under that directory the nearer one leaves out stays the outer one's. A tsconfig left
// with no file of its own is not run.
func TestAFileIncludedTwiceBelongsToTheNearerTsconfig(test *testing.T) {
	test.Parallel()
	root := newRepository(test, map[string]string{
		"tsconfig.json":             `{` + ownershipOptions + `,"include":["**/*.ts"]}`,
		"index.ts":                  "export const index = 1;\n",
		"packages/a/tsconfig.json":  `{` + ownershipOptions + `,"include":["src"]}`,
		"packages/a/src/inner.ts":   "export const inner = 1;\n",
		"packages/a/scripts/run.ts": "export const run = 1;\n",
	})
	found := ownedDiscovery(test, root)
	if want := []string{".", "packages/a"}; !reflect.DeepEqual(found.labels(), want) {
		test.Fatalf("ran %v, want %v", found.labels(), want)
	}
	if got, want := found.Ownership.Yields["."].Files, []string{filepath.Join(root, "packages", "a", "src", "inner.ts")}; !reflect.DeepEqual(got, want) {
		test.Fatalf("the root yields %v, want %v: run.ts is outside packages/a's include, so it stays the root's", got, want)
	}
	if yield, yields := found.Ownership.Yields["packages/a"]; yields {
		test.Errorf("the nearer tsconfig yields %+v, want nothing", yield)
	}

	// The outer tsconfig includes nothing the inner ones do not own.
	writeTree(test, root, map[string]string{"tsconfig.json": `{` + ownershipOptions + `,"include":["packages/a/src"]}`})
	if removeError := os.Remove(filepath.Join(root, "index.ts")); removeError != nil {
		test.Fatal(removeError)
	}
	found = ownedDiscovery(test, root)
	if want := []string{"packages/a"}; !reflect.DeepEqual(found.labels(), want) || !reflect.DeepEqual(found.Ownership.Yielding, []string{"."}) {
		test.Fatalf("ran %v, yielding %v; want packages/a alone, the root yielding everything", found.labels(), found.Ownership.Yielding)
	}
}

// Two sibling tsconfigs that include one shared directory: neither holds it, so it goes to the one whose
// directory shares the longest path with it, and on a tie to the first in discovery order.
func TestASharedFileNeitherHoldsGoesByPathThenOrder(test *testing.T) {
	test.Parallel()
	root := newRepository(test, map[string]string{
		"apps/web/tsconfig.json": `{` + ownershipOptions + `,"include":["*.ts","../shared/*.ts"]}`,
		"apps/web/index.ts":      "export const web = 1;\n",
		"apps/shared/types.ts":   "export type Shared = number;\n",
		"tools/tsconfig.json":    `{` + ownershipOptions + `,"include":["*.ts","../apps/shared/*.ts"]}`,
		"tools/index.ts":         "export const tools = 1;\n",
		"apps/api/tsconfig.json": `{` + ownershipOptions + `,"include":["*.ts","../shared/*.ts"]}`,
		"apps/api/index.ts":      "export const api = 1;\n",
	})
	found := ownedDiscovery(test, root)
	shared := filepath.Join(root, "apps", "shared", "types.ts")
	for label, want := range map[string]bool{"apps/api": false, "apps/web": true, "tools": true} {
		yield := found.Ownership.Yields[label]
		if got := len(yield.Files) == 1 && yield.Files[0] == shared; got != want {
			test.Errorf("%s yields the shared file: %t, want %t (yields %v)", label, got, want, yield.Files)
		}
	}
}

// One TypeScript project is never read: nothing is shared, so ahra's run keeps its cost. Proved by a
// tsconfig that cannot be read leaving no trace; with a second project beside it, the same tsconfig is read
// and its failure leaves it running, for its own run to report.
func TestOneProjectIsNotRead(test *testing.T) {
	test.Parallel()
	root := newRepository(test, map[string]string{"tsconfig.json": `{not json`, "index.ts": "export const x = 1;\n"})
	found := ownedDiscovery(test, root)
	if !reflect.DeepEqual(found.labels(), []string{"."}) || len(found.Ownership.Yields) != 0 {
		test.Fatalf("one project changed: ran %v, yields %v", found.labels(), found.Ownership.Yields)
	}
	if mentionsReferences(filepath.Join(root, "tsconfig.json")) {
		test.Fatal("the byte search found references in a tsconfig without them")
	}
	writeTree(test, root, map[string]string{"packages/a/tsconfig.json": `{` + ownershipOptions + `}`, "packages/a/a.ts": "export const a = 1;\n"})
	found = ownedDiscovery(test, root)
	if !reflect.DeepEqual(found.labels(), []string{".", "packages/a"}) {
		test.Fatalf("an unreadable tsconfig beside another project was dropped: ran %v", found.labels())
	}
}

// A named lint config is passed to every project's run made absolute, whichever way it was spelled, and
// nothing else in the arguments moves.
func TestChildArgumentsCarryTheLintConfigAbsolute(test *testing.T) {
	test.Parallel()
	for _, arguments := range [][]string{
		{"--no-fix", "--lint-config", "settings/Cohere.json", "--json"},
		{"--no-fix", "-lint-config=settings/Cohere.json", "--json"},
		{"--no-fix", "--lint-config=settings/Cohere.json", "--json"},
	} {
		got := childArguments(arguments, "/work/settings/Cohere.json")
		if want := []string{"--no-fix", "--json", "--lint-config", "/work/settings/Cohere.json"}; !reflect.DeepEqual(got, want) {
			test.Errorf("%v became %v, want %v", arguments, got, want)
		}
	}
	if got := childArguments([]string{"--no-fix"}, ""); !reflect.DeepEqual(got, []string{"--no-fix"}) {
		test.Errorf("no lint config named changed the arguments: %v", got)
	}
}

// The built binary, on the shapes the quiet hundred met (#wvgxtey): a lint config named relative to where
// the run started still discovers, and every project's run reads it; a file two tsconfigs include is
// reported once, by the nearer; a solution root runs what it references; and the format walk leaves a
// nested project's directory to its own run, so a file is reported as unformatted once.
func TestOneRunChecksEveryNestedProjectAndEachFileOnce(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	settings := `{"rules":{"no-debugger":"error"}}`
	debugger := "export function value(): number {\n  debugger;\n  return 1;\n}\n"
	root := newRepository(test, map[string]string{
		"tsconfig.json":                  `{` + ownershipOptions + `,"include":["**/*.ts"]}`,
		"index.ts":                       "export const index = 1;\n",
		"packages/a/tsconfig.json":       `{` + ownershipOptions + `,"include":["src"]}`,
		"packages/a/src/inner.ts":        debugger,
		"packages/a/notes.json":          `{"a":1}`,
		"settings/Cohere.json":           settings,
		"packages/a/CohereSettings.json": `{"rules":{}}`,
	})

	output, code := runWithEngine(test, binary, "", root, "--no-fix", "--lint-config", "settings/Cohere.json")
	if code == 0 {
		test.Fatalf("a debugger statement passed the run:\n%s", output)
	}
	if count := strings.Count(output, "inner.ts:2:3"); count != 1 {
		test.Errorf("the finding in a file two tsconfigs include was reported %d times, want once, by packages/a:\n%s", count, output)
	}
	if count := strings.Count(output, filepath.Join("packages", "a", "notes.json")+":1:1 - --fix would rewrite"); count != 1 {
		test.Errorf("a nested project's unformatted file was reported %d times, want once:\n%s", count, output)
	}
	for _, want := range []string{"== packages/a (TypeScript) ==", "projects: 2 checked (2 TypeScript)", "left 1 files the tsconfig includes"} {
		if !strings.Contains(output, want) {
			test.Errorf("the report lacks %q:\n%s", want, output)
		}
	}

	solution := newRepository(test, map[string]string{
		"tsconfig.json":       `{"files":[],"references":[{"path":"./tsconfig.app.json"}]}`,
		"tsconfig.app.json":   `{` + ownershipOptions + `,"include":["src"]}`,
		"src/main.ts":         debugger,
		"CohereSettings.json": settings,
	})
	output, code = runWithEngine(test, binary, "", solution, "--no-fix", "--no-format")
	if code == 0 || !strings.Contains(output, "main.ts:2:3") || !strings.Contains(output, ". not run: a solution tsconfig") {
		test.Fatalf("a solution root did not run what it references (exit %d):\n%s", code, output)
	}
}
