package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	discoveryTsconfig = `{"compilerOptions":{"strict":true,"noEmit":true},"include":["**/*.ts"]}`
	discoveryPackage  = "// swift-tools-version:6.0\nimport PackageDescription\nlet package = Package(name: \"Toy\")\n"
)

// newRepository makes a directory that is a repository's root, by the .git directory discovery reads.
func newRepository(test *testing.T, files map[string]string) string {
	test.Helper()
	root := test.TempDir()
	if createDirectoryError := os.Mkdir(filepath.Join(root, ".git"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	writeTree(test, root, files)
	return root
}

// Every tsconfig.json and Package.swift the repository keeps is a project, nested at any depth, and a
// directory holding both is two (#f9nftxz). Parents come before children and TypeScript before Swift.
func TestDiscoveryFindsEveryProjectTheRepositoryKeeps(test *testing.T) {
	test.Parallel()
	root := newRepository(test, map[string]string{
		"tsconfig.json":               discoveryTsconfig,
		"Package.swift":               discoveryPackage,
		"apps/web/tsconfig.json":      discoveryTsconfig,
		"packages/a/b/Package.swift":  discoveryPackage,
		"packages/a-b/tsconfig.json":  discoveryTsconfig,
		"packages/a/b/Sources/x.txt":  "not a marker\n",
		"apps/web/tsconfig.base.json": discoveryTsconfig,
	})
	found, discoverProjectsError := discoverProjects(root, nil)
	if discoverProjectsError != nil {
		test.Fatal(discoverProjectsError)
	}
	want := []discoveredProject{
		{Directory: ".", Engine: engineTypeScript},
		{Directory: ".", Engine: engineSwift},
		{Directory: "apps/web", Engine: engineTypeScript},
		{Directory: "packages/a/b", Engine: engineSwift},
		{Directory: "packages/a-b", Engine: engineTypeScript},
	}
	if !reflect.DeepEqual(found.Projects, want) {
		test.Fatalf("found %+v, want %+v", found.Projects, want)
	}
}

// A go.work and a go.mod in one directory are one Go project, the workspace, and the workspace claims the
// modules its uses name: they leave Projects, since its run builds them. A module below it that it does not
// use stays a project and a note names why, and a use that leads into a submodule or outside the root claims
// nothing and breaks nothing.
func TestDiscoveryLetsAGoWorkspaceClaimTheModulesItUses(test *testing.T) {
	test.Parallel()
	workspace := "go 1.24\n\nuse (\n\t.\n\t./services/api\n\t./tools/lint\n\t./vendored/module\n\t../outside\n)\n"
	root := newRepository(test, map[string]string{
		"go.work":                   workspace,
		"go.mod":                    "module example.com/root\n",
		"services/api/go.mod":       "module example.com/api\n",
		"tools/lint/go.mod":         "module example.com/lint\n",
		"tools/unused/go.mod":       "module example.com/unused\n",
		"vendored/.git":             "gitdir: ../.git/modules/vendored\n",
		"vendored/module/go.mod":    "module example.com/vendored\n",
		"vendored/module/vendor.go": "package module\n",
	})
	found, discoverProjectsError := discoverProjects(root, nil)
	if discoverProjectsError != nil {
		test.Fatal(discoverProjectsError)
	}
	want := []discoveredProject{
		{Directory: ".", Engine: engineGo},
		{Directory: "tools/unused", Engine: engineGo},
	}
	if !reflect.DeepEqual(found.Projects, want) {
		test.Fatalf("found %+v, want %+v", found.Projects, want)
	}
	if found.Submodules != 1 {
		test.Errorf("submodules %d, want 1: the use inside one should leave the walk as it was", found.Submodules)
	}
	wantNote := "tools/unused/go.mod runs as its own module: go.work does not use it"
	if notes := found.notes(); !reflect.DeepEqual(notes, []string{wantNote}) {
		test.Errorf("notes %q, want only %q", notes, wantNote)
	}

	// The control: with no go.work, every module is a project of its own and no note says otherwise.
	if removeError := os.Remove(filepath.Join(root, "go.work")); removeError != nil {
		test.Fatal(removeError)
	}
	unclaimed, discoverProjectsError := discoverProjects(root, nil)
	if discoverProjectsError != nil {
		test.Fatal(discoverProjectsError)
	}
	if len(unclaimed.Projects) != 4 || len(unclaimed.notes()) != 0 {
		test.Errorf("without the workspace found %+v and notes %q, want four modules and no note",
			unclaimed.Projects, unclaimed.notes())
	}
}

// A go.work that does not parse fails its own project and nothing else: discovery still finds the
// TypeScript program beside it, keeps the workspace as a project whose run will fail on the file, runs the
// module below it on its own so it is still checked, and names the file and why in a note.
func TestDiscoveryKeepsAMalformedGoWorkToItsOwnProject(test *testing.T) {
	test.Parallel()
	root := newRepository(test, map[string]string{
		"tsconfig.json":            discoveryTsconfig,
		"workspace/go.work":        "go 1.24\n\nuse (\n\t./module\n",
		"workspace/module/go.mod":  "module example.com/module\n",
		"workspace/module/code.go": "package module\n",
	})
	found, discoveryError := discoverProjects(root, nil)
	if discoveryError != nil {
		test.Fatalf("one malformed go.work stopped discovery for every project: %v", discoveryError)
	}
	want := []discoveredProject{
		{Directory: ".", Engine: engineTypeScript},
		{Directory: "workspace", Engine: engineGo},
		{Directory: "workspace/module", Engine: engineGo},
	}
	if !reflect.DeepEqual(found.Projects, want) {
		test.Errorf("found %+v, want %+v", found.Projects, want)
	}
	notes := strings.Join(found.notes(), "\n")
	wantFile := filepath.Join(root, "workspace", "go.work")
	if !strings.Contains(notes, "workspace/go.work claims no module") || !strings.Contains(notes, wantFile) {
		test.Errorf("the notes do not name the malformed %s and why:\n%s", wantFile, notes)
	}
	if strings.Contains(notes, "does not use it") {
		test.Errorf("the module below a workspace that could not be read was called unused:\n%s", notes)
	}
}

// Within one directory TypeScript comes first, then Swift, then Go, whatever order the reads finished in.
func TestSortProjectsRanksTypeScriptThenSwiftThenGo(test *testing.T) {
	test.Parallel()
	found := discovery{Projects: []discoveredProject{
		{Directory: "app", Engine: engineGo},
		{Directory: ".", Engine: engineGo},
		{Directory: "app", Engine: engineSwift},
		{Directory: ".", Engine: engineSwift},
		{Directory: "app", Engine: engineTypeScript},
		{Directory: ".", Engine: engineTypeScript},
	}}
	found.sortProjects()
	want := []discoveredProject{
		{Directory: ".", Engine: engineTypeScript},
		{Directory: ".", Engine: engineSwift},
		{Directory: ".", Engine: engineGo},
		{Directory: "app", Engine: engineTypeScript},
		{Directory: "app", Engine: engineSwift},
		{Directory: "app", Engine: engineGo},
	}
	if !reflect.DeepEqual(found.Projects, want) {
		test.Errorf("sorted %+v, want %+v", found.Projects, want)
	}
}

// The summary line counts Go projects after Swift's.
func TestSummaryLineCountsGoProjects(test *testing.T) {
	test.Parallel()
	runs := []projectRun{
		{project: discoveredProject{Directory: ".", Engine: engineTypeScript}},
		{project: discoveredProject{Directory: ".", Engine: engineGo}},
		{project: discoveredProject{Directory: "swift", Engine: engineSwift}},
		{project: discoveredProject{Directory: "tools", Engine: engineGo}, exitCode: 1},
	}
	line := summaryLine(discovery{}, runs)
	if want := "projects: 4 checked (1 TypeScript, 1 Swift, 2 Go), 1 failed"; !strings.HasPrefix(line, want) {
		test.Errorf("summary %q, want it to start %q", line, want)
	}
}

// What discovery must never enter, each with a marker planted inside it: dependencies, build output,
// caches, fixtures, what .gitignore ignores, a repository of its own, a submodule, and a marker the root's
// ignorePatterns refuse. Positive control: the same tree walked with every refusal lifted finds every
// planted marker, so the pass is the refusals at work and not markers that were never there.
// Not parallel: it swaps the package-level neverDescended and newIgnoreScope, which discoverProjects reads
func TestDiscoveryNeverEntersWhatItMustNot(test *testing.T) {
	root := newRepository(test, map[string]string{
		"Package.swift":                         discoveryPackage,
		".gitignore":                            "generated/\n",
		"node_modules/dependency/tsconfig.json": discoveryTsconfig,
		"node_modules/gotool/go.mod":            "module example.com/gotool\n",
		".build/checkouts/dep/Package.swift":    discoveryPackage,
		".cache/cohere/tsconfig.json":           discoveryTsconfig,
		".cache/gomodule/go.work":               "go 1.27\n",
		"internal/testdata/tsconfig.json":       discoveryTsconfig,
		"internal/testdata/fixture/go.mod":      "module example.com/fixture\n",
		"generated/tsconfig.json":               discoveryTsconfig,
		"generated/gocode/go.mod":               "module example.com/gocode\n",
		"clone/go.mod":                          "module example.com/clone\n",
		"submodule/go.mod":                      "module example.com/submodule\n",
		"apps/.gitignore":                       "legacy/\n",
		"apps/legacy/tsconfig.json":             discoveryTsconfig,
		"clone/Package.swift":                   discoveryPackage,
		"submodule/tsconfig.json":               discoveryTsconfig,
		"submodule/.git":                        "gitdir: ../.git/modules/submodule\n",
		"refused/tsconfig.json":                 discoveryTsconfig,
	})
	if createDirectoryError := os.Mkdir(filepath.Join(root, "clone", ".git"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}

	found, discoverProjectsError := discoverProjects(root, []string{"refused/**"})
	if discoverProjectsError != nil {
		test.Fatal(discoverProjectsError)
	}
	if want := []discoveredProject{{Directory: ".", Engine: engineSwift}}; !reflect.DeepEqual(found.Projects, want) {
		test.Fatalf("found %+v, want only the root's package", found.Projects)
	}
	// node_modules, .build, .cache and testdata; the root's own .git is passed over without counting.
	if found.NeverDescended != 4 || found.Ignored != 2 || found.Submodules != 1 {
		test.Errorf(
			"never descended %d (want 4), ignored %d (want 2: one by the root .gitignore, one by apps/.gitignore), submodules %d (want 1)",
			found.NeverDescended,
			found.Ignored,
			found.Submodules,
		)
	}
	if want := []string{"clone"}; !reflect.DeepEqual(found.NestedRepositories, want) {
		test.Errorf("nested repositories %v, want %v", found.NestedRepositories, want)
	}
	if want := []string{"refused/tsconfig.json"}; !reflect.DeepEqual(found.Refused, want) {
		test.Errorf("refused %v, want %v", found.Refused, want)
	}

	// The control: with nothing never-descended and nothing ignored, the planted markers are found.
	previousNames, previousScope := neverDescended, newIgnoreScope
	test.Cleanup(func() { neverDescended, newIgnoreScope = previousNames, previousScope })
	neverDescended = map[string]bool{".git": true}
	newIgnoreScope = func(string) (ignoreScope, error) { return ignoresNothing{}, nil }
	lifted, discoverProjectsError := discoverProjects(root, nil)
	if discoverProjectsError != nil {
		test.Fatal(discoverProjectsError)
	}
	directories := map[string]bool{}
	for _, project := range lifted.Projects {
		directories[project.Directory] = true
	}
	for _, planted := range []string{
		"node_modules/dependency", ".build/checkouts/dep", ".cache/cohere", "internal/testdata", "generated",
		"apps/legacy", "refused", "node_modules/gotool", ".cache/gomodule", "internal/testdata/fixture",
		"generated/gocode",
	} {
		if !directories[planted] {
			test.Errorf(
				"with the refusals lifted, %s was still not found, so its refusal above proves nothing",
				planted,
			)
		}
	}
}

// ignoresNothing is a scope that ignores nothing, for the control above.
type ignoresNothing struct{}

func (ignoresNothing) Enter(string) (ignoreScope, error) { return ignoresNothing{}, nil }
func (ignoresNothing) Ignored(string, bool) bool         { return false }

// Discovery starts where the walk up found a project, unless the caller stands in a repository with no
// project at or above them inside it: then the repository's root, which is how cohere's own repository
// finds swift/. A project above the repository belongs to some other tree.
func TestDiscoveryRootIsTheRepositoryTheCallerStandsIn(test *testing.T) {
	test.Parallel()
	repository := newRepository(test, map[string]string{
		"swift/Package.swift": discoveryPackage,
		"docs/readme.md":      "docs\n",
	})
	start := filepath.Join(repository, "docs")
	location, locateError := locateProject(locationRequest{WorkingDirectory: start, ConfigFileName: projectMarker})
	if locateError == nil {
		test.Fatalf(
			"the walk up found %s, so this does not test a repository with no project above the caller",
			location.Root,
		)
	}
	if root := discoveryRoot(location, locateError, start); root != repository {
		test.Errorf("discovery starts at %s, want the repository %s", root, repository)
	}

	outer := test.TempDir()
	writeTree(
		test,
		outer,
		map[string]string{"tsconfig.json": discoveryTsconfig, "inner/swift/Package.swift": discoveryPackage},
	)
	inner := filepath.Join(outer, "inner")
	if createDirectoryError := os.Mkdir(filepath.Join(inner, ".git"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	location, locateError = locateProject(locationRequest{WorkingDirectory: inner, ConfigFileName: projectMarker})
	if locateError != nil || location.Root != outer {
		test.Fatalf("the walk up should find the outer project, got %s, %v", location.Root, locateError)
	}
	if root := discoveryRoot(location, locateError, inner); root != inner {
		test.Errorf("discovery starts at %s, want the repository %s rather than the project above it", root, inner)
	}

	project := test.TempDir()
	writeTree(test, project, map[string]string{"tsconfig.json": discoveryTsconfig, "sub/file.ts": "export {};\n"})
	start = filepath.Join(project, "sub")
	location, locateError = locateProject(locationRequest{WorkingDirectory: start, ConfigFileName: projectMarker})
	if root := discoveryRoot(location, locateError, start); root != project {
		test.Errorf("discovery starts at %s, want the project the walk up found, %s", root, project)
	}
}

// A run that names one project, by directory, tsconfig or path, checks that project alone, as it always
// did, and so do the listings and explanations; a bare run discovers, and so does one naming a lint config,
// which says which rules apply and not which project (#wvgxtey).
func TestDiscoveryAppliesOnlyToARunThatNamesNoProject(test *testing.T) {
	test.Parallel()
	if !discoveryApplies(map[string]bool{"no-fix": true, "format-all": true}, nil) {
		test.Error("a bare --no-fix --format-all run does not discover")
	}
	if !discoveryApplies(map[string]bool{"lint-config": true}, nil) {
		test.Error("a run naming --lint-config does not discover")
	}
	for _, named := range []string{
		"directory",
		"tsconfig",
		"rules",
		"print-config",
		"explain",
		"stdin-filepath",
		"version",
	} {
		if discoveryApplies(map[string]bool{named: true}, nil) {
			test.Errorf("a run naming --%s discovers", named)
		}
	}
	if discoveryApplies(map[string]bool{}, []string{"src/index.ts"}) {
		test.Error("a run naming a path discovers")
	}
}

// A root no TypeScript project holds gets the format project when it holds a file the house formatter handles
// outside every TypeScript project's directory (#4wjhppw), and the project yields each TypeScript project's
// directory to that project's own run. Controls, each without it: a TypeScript project at the root, which
// formats the repository itself; a root whose only such files are inside a TypeScript project below it; one
// whose only such file the root's ignorePatterns name; and a Go module with nothing for it to format.
func TestARootNoTypeScriptProjectHoldsGetsTheFormatProject(test *testing.T) {
	test.Parallel()
	goModule := map[string]string{"go.mod": "module example.com/root\n", "main.go": "package main\n"}
	withFiles := func(files map[string]string) map[string]string {
		tree := map[string]string{}
		for path, contents := range goModule {
			tree[path] = contents
		}
		for path, contents := range files {
			tree[path] = contents
		}
		return tree
	}
	for name, testCase := range map[string]struct {
		files         map[string]string
		ignore        []string
		wantFormat    bool
		wantYieldFrom []string
	}{
		"a README beside the module": {
			files:      withFiles(map[string]string{"README.md": "# root\n"}),
			wantFormat: true,
		},
		"a policy file below the module, and a web app": {
			files: withFiles(map[string]string{
				"policy/Words.json":    "[]\n",
				"web/tsconfig.json":    discoveryTsconfig,
				"web/source/a.ts":      "export const a = 1;\n",
				"web/package.json":     "{}\n",
				"web/source/README.md": "# web\n",
			}),
			wantFormat:    true,
			wantYieldFrom: []string{"web"},
		},
		// A script Node runs as ES or CommonJS is TypeScript the house formatter reads (#9v2x63d).
		"a `.mts` script beside the module": {
			files:      withFiles(map[string]string{"scripts/release.mts": "export const release = 1;\n"}),
			wantFormat: true,
		},
		"a `.cts` script beside the module": {
			files:      withFiles(map[string]string{"scripts/release.cts": "export const release = 1;\n"}),
			wantFormat: true,
		},
		"a TypeScript project at the root": {
			files: withFiles(map[string]string{"tsconfig.json": discoveryTsconfig, "README.md": "# root\n"}),
		},
		"Markdown only inside a TypeScript project": {
			files: withFiles(map[string]string{"web/tsconfig.json": discoveryTsconfig, "web/README.md": "# web\n"}),
		},
		"Markdown only where the ignorePatterns reach": {
			files:  withFiles(map[string]string{"vendored/README.md": "# theirs\n"}),
			ignore: []string{"vendored/**"},
		},
		"nothing to format": {
			files: withFiles(nil),
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			root := newRepository(test, testCase.files)
			found, discoverProjectsError := discoverProjects(root, testCase.ignore)
			if discoverProjectsError != nil {
				test.Fatal(discoverProjectsError)
			}
			found.addRootFormatProject()
			formatProjects := 0
			for _, project := range found.Projects {
				if project.Engine == engineFormat {
					formatProjects++
					if project.Directory != "." {
						test.Errorf("the format project is at %q, want the root", project.Directory)
					}
				}
			}
			if testCase.wantFormat != (formatProjects == 1) || formatProjects > 1 {
				test.Fatalf("%d format projects among %+v, want one: %v", formatProjects, found.Projects,
					testCase.wantFormat)
			}
			if !testCase.wantFormat {
				return
			}
			rootProjects := []discoveredProject{}
			for _, project := range found.Projects {
				if project.Directory == "." {
					rootProjects = append(rootProjects, project)
				}
			}
			if last := rootProjects[len(rootProjects)-1]; last.Engine != engineFormat {
				test.Errorf("the format project sorts before %+v, want it last of the root's", last)
			}
			wantYield := projectYield{}
			for _, directory := range testCase.wantYieldFrom {
				wantYield.Directories = append(wantYield.Directories, filepath.Join(root, directory))
			}
			if yield := found.formatYield(); !reflect.DeepEqual(yield, wantYield) {
				test.Errorf("the format project yields %+v, want %+v", yield, wantYield)
			}
		})
	}
}

// The summary line counts the format project after every engine with a marker.
func TestSummaryLineCountsTheFormatProject(test *testing.T) {
	test.Parallel()
	runs := []projectRun{
		{project: discoveredProject{Directory: ".", Engine: engineGo}},
		{project: discoveredProject{Directory: ".", Engine: engineFormat}, exitCode: 1},
		{project: discoveredProject{Directory: "swift", Engine: engineSwift}},
	}
	line := summaryLine(discovery{}, runs)
	if want := "projects: 3 checked (1 Swift, 1 Go, 1 Format), 1 failed"; !strings.HasPrefix(line, want) {
		test.Errorf("summary %q, want it to start %q", line, want)
	}
}

// TestTheFormatProjectsRecordSaysWhatItCovered is #vyjvz9k, through the binary: at a Go module's root holding Markdown
// and JSON, the format project's `--json` summary counts the files it accounted for: formatted fresh on the first run,
// vouched for by the format record on the next, and after one file changes, that one fresh and the other two vouched
// for. Its label is its own, `. (Format)`, beside the Go project's `.`. It used to say 0 files in scope.
func TestTheFormatProjectsRecordSaysWhatItCovered(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		".gitignore":         ".cache/\n",
		"go.mod":             "module example.com/covered\n\ngo 1.27\n",
		"main.go":            "package main\n\nfunc main() {}\n",
		"README.md":          "# covered\n",
		"policy/Words.json":  "[\"a\", \"b\"]\n",
		"policy/README.md":   "# policy\n",
		"internal/notes.txt": "not formatted\n",
	})
	summaries := func() map[string]summaryJson {
		test.Helper()
		command := exec.Command(binary, "--no-fix", "--format-only", "--json")
		command.Dir = root
		command.Env = append(withoutVariable(os.Environ(), goEngineChildVariable), "COHERE_VERDICT_FD=",
			"CGO_ENABLED=0")
		output, runError := command.Output()
		var exitError *exec.ExitError
		if runError != nil && !errors.As(runError, &exitError) {
			test.Fatalf("running cohere: %v", runError)
		}
		found := map[string]summaryJson{}
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			var record summaryJson
			if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
				test.Fatalf("a line that is not JSON: %q", line)
			}
			// A project's summary carries its label; the overall one, merging them, carries none.
			if record.Kind != "summary" || record.Label == "" {
				continue
			}
			if _, duplicated := found[record.Label]; duplicated {
				test.Errorf("two summaries share the label %q:\n%s", record.Label, output)
			}
			found[record.Label] = record
		}
		return found
	}
	// The third run follows an edit to one file, so its counts split: a zeroed count, checked or cached, shows
	// here even where the other runs' zero is the right answer.
	for run, want := range []struct {
		edit            string
		checked, cached int
	}{{"", 3, 0}, {"", 0, 3}, {"# covered, edited\n", 1, 2}} {
		if want.edit != "" {
			writeTree(test, root, map[string]string{"README.md": want.edit})
		}
		found := summaries()
		formatProject, present := found[". (Format)"]
		if _, goPresent := found["."]; !present || !goPresent {
			test.Fatalf("run %d: want summaries labeled `.` and `. (Format)`, got %v", run, found)
		}
		if formatProject.FilesInScope != 3 || formatProject.Checked != want.checked ||
			formatProject.Cached != want.cached {
			test.Errorf("run %d: the format project counts %d in scope, %d checked, %d cached; want 3, %d, %d", run,
				formatProject.FilesInScope, formatProject.Checked, formatProject.Cached, want.checked, want.cached)
		}
	}
}
