package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree writes files under a directory, creating whatever directories they need.
func writeTree(test *testing.T, directory string, files map[string]string) {
	test.Helper()
	for name, contents := range files {
		path := filepath.Join(directory, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
}

// TestLocateProjectWalksUpToTheNearestTsconfig holds discovery, and each flag that overrides it.
//
// The defect was that every default resolved against the working directory, so `cohere` worked from
// the root and nowhere else: from `modules/tasks` it reported "no tsconfig at
// modules/tasks/tsconfig.json", which is true and useless. Each case below is one sentence of the
// precedence locateProject states, asserted on the value that decides it.
func TestLocateProjectWalksUpToTheNearestTsconfig(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":                 "{}",
		"CohereSettings.json":           "{}",
		"modules/tasks/source/A.ts":     "",
		"projects/nested/tsconfig.json": "{}",
	})
	deep := filepath.Join(root, "modules", "tasks", "source")

	test.Run("discovered from a subdirectory", func(test *testing.T) {
		test.Parallel()
		location, locateProjectError := locateProject(locationRequest{
			WorkingDirectory:   deep,
			ConfigFileName:     projectMarker,
			LintConfigFileName: "CohereSettings.json",
		})
		if locateProjectError != nil {
			test.Fatal(locateProjectError)
		}
		if location.Root != root {
			test.Errorf("root = %s, want %s", location.Root, root)
		}
		if location.ConfigFileName != filepath.Join(root, "tsconfig.json") {
			test.Errorf("tsconfig = %s", location.ConfigFileName)
		}
		if location.LintConfigFileName != filepath.Join(root, "CohereSettings.json") {
			test.Errorf("a default lint config resolved somewhere other than the root: %s", location.LintConfigFileName)
		}
		// Typed paths stay where they were typed. Resolving them against the root would make
		// `cohere A.ts` from `source` look for a file at the top of the tree.
		if location.ArgumentBase != deep {
			test.Errorf("typed paths resolve against %s, want the working directory %s", location.ArgumentBase, deep)
		}
		if note := location.rootNote(); !strings.Contains(note, root) || !strings.Contains(note, deep) {
			test.Errorf("a root found elsewhere did not say both where it checked and where it started: %q", note)
		}
	})

	// The control for the note: at the root it must be silent, or it prints on every ordinary run.
	test.Run("silent at the root", func(test *testing.T) {
		test.Parallel()
		location, locateProjectError := locateProject(locationRequest{
			WorkingDirectory: root, ConfigFileName: projectMarker, LintConfigFileName: "CohereSettings.json",
		})
		if locateProjectError != nil {
			test.Fatal(locateProjectError)
		}
		if note := location.rootNote(); note != "" {
			test.Errorf("a run started at its own root still carried a root note: %q", note)
		}
	})

	// The nearest tsconfig wins, which is what makes a separate sub-project its own root rather than
	// a directory of the project above it.
	test.Run("the nearest tsconfig wins", func(test *testing.T) {
		test.Parallel()
		nested := filepath.Join(root, "projects", "nested")
		location, locateProjectError := locateProject(locationRequest{
			WorkingDirectory: nested, ConfigFileName: projectMarker, LintConfigFileName: "CohereSettings.json",
		})
		if locateProjectError != nil {
			test.Fatal(locateProjectError)
		}
		if location.Root != nested {
			test.Errorf("walked past a nearer tsconfig to %s", location.Root)
		}
	})

	test.Run("--directory wins and is a cd", func(test *testing.T) {
		test.Parallel()
		location, locateProjectError := locateProject(locationRequest{
			WorkingDirectory:   deep,
			Directory:          "../..",
			ConfigFileName:     projectMarker,
			LintConfigFileName: "CohereSettings.json",
		})
		if locateProjectError != nil {
			test.Fatal(locateProjectError)
		}
		modules := filepath.Join(root, "modules")
		if location.Root != modules || location.ArgumentBase != modules {
			test.Errorf(
				"--directory was not both the root and the base: root %s, base %s",
				location.Root,
				location.ArgumentBase,
			)
		}
		// Discovery would have found the real tsconfig above; an explicit directory must not.
		if location.ConfigFileName != filepath.Join(modules, "tsconfig.json") {
			test.Errorf("discovery overrode an explicit --directory: %s", location.ConfigFileName)
		}
	})

	test.Run("--tsconfig wins and anchors the root", func(test *testing.T) {
		test.Parallel()
		location, locateProjectError := locateProject(locationRequest{
			WorkingDirectory:    deep,
			ConfigFileName:      "../../../projects/nested/tsconfig.json",
			ConfigFileNameGiven: true,
			LintConfigFileName:  "CohereSettings.json",
		})
		if locateProjectError != nil {
			test.Fatal(locateProjectError)
		}
		nested := filepath.Join(root, "projects", "nested")
		if location.Root != nested {
			test.Errorf("an explicit tsconfig did not anchor the root: %s", location.Root)
		}
		if location.LintConfigFileName != filepath.Join(nested, "CohereSettings.json") {
			test.Errorf("the default lint config did not follow the root: %s", location.LintConfigFileName)
		}
	})

	test.Run("a typed --lint-config resolves where it was typed", func(test *testing.T) {
		test.Parallel()
		location, locateProjectError := locateProject(locationRequest{
			WorkingDirectory:        deep,
			ConfigFileName:          projectMarker,
			LintConfigFileName:      "Local.json",
			LintConfigFileNameGiven: true,
		})
		if locateProjectError != nil {
			test.Fatal(locateProjectError)
		}
		if location.LintConfigFileName != filepath.Join(deep, "Local.json") {
			test.Errorf("a typed lint config resolved against %s", location.LintConfigFileName)
		}
	})
}

// TestLocateProjectFindsSwiftPackages holds the second marker: the nearest directory holding either
// marker is the root, and the marker decides the engine.
//
// The tree mirrors ahra's: a TypeScript root with a Swift package under `projects/`, a Swift package
// vendored inside that one, and a TypeScript project inside a Swift package. Each case is one sentence
// of swiftProjectMarker's rule, asserted both ways so neither marker can win by always winning.
func TestLocateProjectFindsSwiftPackages(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":                                  "{}",
		"projects/macos/Package.swift":                   "",
		"projects/macos/Sources/App/Main.swift":          "",
		"projects/macos/Vendor/Term/Package.swift":       "",
		"projects/macos/Vendor/Term/Sources/Term.swift":  "",
		"projects/macos/web/tsconfig.json":               "{}",
		"projects/both/tsconfig.json":                    "{}",
		"projects/both/Package.swift":                    "",
		"projects/macos/Sources/App/tsconfig.json/empty": "",
	})
	request := func(workingDirectory string) locationRequest {
		return locationRequest{
			WorkingDirectory:   workingDirectory,
			ConfigFileName:     projectMarker,
			LintConfigFileName: "CohereSettings.json",
		}
	}
	macos := filepath.Join(root, "projects", "macos")

	cases := []struct {
		name       string
		standingIn string
		wantRoot   string
		wantEngine projectEngine
	}{
		{
			"a Swift package under a TypeScript root is the Swift package's",
			filepath.Join(macos, "Sources", "App"),
			macos,
			engineSwift,
		},
		{"the TypeScript root is still TypeScript's", root, root, engineTypeScript},
		{
			"a vendored package is its own root",
			filepath.Join(macos, "Vendor", "Term", "Sources"),
			filepath.Join(macos, "Vendor", "Term"),
			engineSwift,
		},
		{
			"a TypeScript project inside a Swift package is TypeScript's",
			filepath.Join(macos, "web"),
			filepath.Join(macos, "web"),
			engineTypeScript,
		},
		{
			"a directory holding both markers is TypeScript's",
			filepath.Join(root, "projects", "both"),
			filepath.Join(root, "projects", "both"),
			engineTypeScript,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			location, locateProjectError := locateProject(request(testCase.standingIn))
			if locateProjectError != nil {
				test.Fatal(locateProjectError)
			}
			if location.Root != testCase.wantRoot || location.Engine != testCase.wantEngine {
				test.Errorf(
					"root %s engine %s, want root %s engine %s",
					location.Root,
					location.Engine,
					testCase.wantRoot,
					testCase.wantEngine,
				)
			}
			// A Swift root carries no tsconfig path, so nothing downstream can build a graph from one.
			if location.Engine == engineSwift && location.ConfigFileName != "" {
				test.Errorf("a Swift root carried a tsconfig path: %s", location.ConfigFileName)
			}
			if location.LintConfigFileName != filepath.Join(testCase.wantRoot, "CohereSettings.json") {
				test.Errorf("the default lint config is not beside the marker: %s", location.LintConfigFileName)
			}
		})
	}

	// A directory named like a marker is not a marker. Sources/App holds a directory called
	// tsconfig.json, and the walk must pass it and reach the Package.swift above.
	test.Run("a directory named tsconfig.json is not a project", func(test *testing.T) {
		test.Parallel()
		location, locateProjectError := locateProject(request(filepath.Join(macos, "Sources", "App", "tsconfig.json")))
		if locateProjectError != nil {
			test.Fatal(locateProjectError)
		}
		if location.Root != macos || location.Engine != engineSwift {
			test.Errorf("stopped at a directory named like a marker: root %s engine %s", location.Root, location.Engine)
		}
	})

	test.Run("--directory reads the markers of the directory it names", func(test *testing.T) {
		test.Parallel()
		location, locateProjectError := locateProject(locationRequest{
			WorkingDirectory:   root,
			Directory:          "projects/macos",
			ConfigFileName:     projectMarker,
			LintConfigFileName: "CohereSettings.json",
		})
		if locateProjectError != nil {
			test.Fatal(locateProjectError)
		}
		if location.Root != macos || location.Engine != engineSwift {
			test.Errorf("--directory at a Swift package: root %s engine %s", location.Root, location.Engine)
		}
	})

	test.Run("--tsconfig is always TypeScript", func(test *testing.T) {
		test.Parallel()
		location, locateProjectError := locateProject(locationRequest{
			WorkingDirectory:    filepath.Join(macos, "Sources", "App"),
			ConfigFileName:      filepath.Join(root, "tsconfig.json"),
			ConfigFileNameGiven: true,
			LintConfigFileName:  "CohereSettings.json",
		})
		if locateProjectError != nil {
			test.Fatal(locateProjectError)
		}
		if location.Root != root || location.Engine != engineTypeScript {
			test.Errorf(
				"an explicit tsconfig inside a Swift package: root %s engine %s",
				location.Root,
				location.Engine,
			)
		}
	})
}

// TestLocateProjectFailsLoudlyOutsideAnyProject holds that finding nothing is an error naming where
// the walk began, never a fallback to the working directory.
func TestLocateProjectFailsLoudlyOutsideAnyProject(test *testing.T) {
	test.Parallel()
	// test.TempDir is under the system temp directory, which has no tsconfig.json at or above it.
	outside := test.TempDir()
	for directory := outside; ; directory = filepath.Dir(directory) {
		if _, found := engineAt(directory); found {
			test.Skipf("%s holds a project marker, so there is no outside to test from", directory)
		}
		if filepath.Dir(directory) == directory {
			break
		}
	}

	_, locateProjectError := locateProject(locationRequest{
		WorkingDirectory: outside, ConfigFileName: projectMarker, LintConfigFileName: "CohereSettings.json",
	})
	if locateProjectError == nil {
		test.Fatal("no tsconfig anywhere above, and locateProject returned a location")
	}
	for _, want := range []string{outside, projectMarker, swiftProjectMarker, goWorkspaceMarker, goModuleMarker} {
		if !strings.Contains(locateProjectError.Error(), want) {
			test.Errorf("the error does not say what it looked for and where, missing %s: %v", want, locateProjectError)
		}
	}
}

// TestEngineAtRanksTheFourMarkers holds the order engineAt reads the markers in: TypeScript, then Swift, then
// Go, and a directory holding a go.work and a go.mod is one Go project, the workspace, even when a go.work
// above it would use its module.
func TestEngineAtRanksTheFourMarkers(test *testing.T) {
	test.Parallel()
	root := newRepository(test, map[string]string{
		"go.work":                      "go 1.24\n\nuse ./workspace-and-module\n",
		"all/tsconfig.json":            "{}",
		"all/Package.swift":            "",
		"all/go.work":                  "go 1.24\n",
		"all/go.mod":                   "module example.com/all\n",
		"swift/Package.swift":          "",
		"swift/go.work":                "go 1.24\n",
		"swift/go.mod":                 "module example.com/swift\n",
		"workspace-and-module/go.work": "go 1.24\n\nuse .\n",
		"workspace-and-module/go.mod":  "module example.com/both\n",
		"module/go.mod":                "module example.com/module\n",
	})
	cases := []struct {
		directory  string
		wantEngine projectEngine
	}{
		{"all", engineTypeScript},
		{"swift", engineSwift},
		{"workspace-and-module", engineGo},
		{"module", engineGo},
	}
	for _, testCase := range cases {
		engine, found := engineAt(filepath.Join(root, testCase.directory))
		if !found || engine != testCase.wantEngine {
			test.Errorf("%s: engine %q (found %v), want %s", testCase.directory, engine, found, testCase.wantEngine)
		}
	}

	// The workspace in one directory with its module is the root there, and the walk stops at it rather than
	// reading the go.work above, whose use names it.
	workspaceAndModule := filepath.Join(root, "workspace-and-module")
	location, locateProjectError := locateProject(
		locationRequest{WorkingDirectory: workspaceAndModule, ConfigFileName: projectMarker},
	)
	if locateProjectError != nil {
		test.Fatal(locateProjectError)
	}
	if location.Root != workspaceAndModule || location.Engine != engineGo || location.ConfigFileName != "" {
		test.Errorf("root %s engine %s tsconfig %q, want the workspace %s, Go, and no tsconfig",
			location.Root, location.Engine, location.ConfigFileName, workspaceAndModule)
	}
}

// TestLocateProjectWalksAGoModuleUpToItsWorkspace holds goModuleRoot: a module the nearest go.work above it
// uses is checked from the workspace, as go builds it, and a module that go.work does not use stays its own
// root. Only the nearest go.work counts, and none above the enclosing repository does.
func TestLocateProjectWalksAGoModuleUpToItsWorkspace(test *testing.T) {
	test.Parallel()
	outside := test.TempDir()
	writeTree(test, outside, map[string]string{
		"go.work":                                "go 1.24\n\nuse ./bare\n",
		"repository/go.work":                     "go 1.24\n\nuse (\n\t./modules/used\n\t./modules/nearer/inner\n)\n",
		"repository/modules/used/go.mod":         "module example.com/used\n",
		"repository/modules/used/deep/a.go":      "package deep\n",
		"repository/modules/unused/go.mod":       "module example.com/unused\n",
		"repository/modules/nearer/go.work":      "go 1.24\n",
		"repository/modules/nearer/inner/go.mod": "module example.com/inner\n",
		"repository/modules/nearer/inner/b.go":   "package inner\n",
		"bare/go.mod":                            "module example.com/bare\n",
		"bare/c.go":                              "package bare\n",
	})
	repository := filepath.Join(outside, "repository")
	if createDirectoryError := os.Mkdir(filepath.Join(repository, ".git"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	// The nearest go.work above inner does not use it, and the repository's, further up, does.
	inner := filepath.Join(repository, "modules", "nearer", "inner")
	// A repository of its own, so the go.work above it, which uses its module, is past the boundary.
	bare := filepath.Join(outside, "bare")
	if createDirectoryError := os.Mkdir(filepath.Join(bare, ".git"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}

	unused := filepath.Join(repository, "modules", "unused")

	cases := []struct {
		name       string
		standingIn string
		wantRoot   string
	}{
		{"a used module is the workspace's", filepath.Join(repository, "modules", "used", "deep"), repository},
		{"an unused module is its own", unused, unused},
		{"only the nearest go.work counts", inner, inner},
		{"a module at its repository's root reads no go.work above it", bare, bare},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			location, locateProjectError := locateProject(locationRequest{
				WorkingDirectory: testCase.standingIn, ConfigFileName: projectMarker,
			})
			if locateProjectError != nil {
				test.Fatal(locateProjectError)
			}
			if location.Root != testCase.wantRoot || location.Engine != engineGo {
				test.Errorf("root %s engine %s, want root %s engine Go",
					location.Root, location.Engine, testCase.wantRoot)
			}
		})
	}

	// `--directory` is a `cd`: a used module named by it is the root, with no walk to its workspace.
	test.Run("--directory does not walk to the workspace", func(test *testing.T) {
		test.Parallel()
		used := filepath.Join(repository, "modules", "used")
		location, locateProjectError := locateProject(locationRequest{
			WorkingDirectory: repository, Directory: "modules/used", ConfigFileName: projectMarker,
		})
		if locateProjectError != nil {
			test.Fatal(locateProjectError)
		}
		if location.Root != used || location.Engine != engineGo {
			test.Errorf("root %s engine %s, want root %s engine Go", location.Root, location.Engine, used)
		}
	})
}

// TestAMalformedGoWorkIsAnErrorNamingIt holds that a go.work the walk up reads and cannot parse stops the run
// naming the file, rather than reading as no workspace and checking the module apart from it.
func TestAMalformedGoWorkIsAnErrorNamingIt(test *testing.T) {
	test.Parallel()
	root := newRepository(test, map[string]string{
		"go.work":          "go 1.24\n\nfrobnicate ./module\n",
		"module/go.mod":    "module example.com/module\n",
		"module/module.go": "package module\n",
	})
	module := filepath.Join(root, "module")
	_, locateProjectError := locateProject(locationRequest{WorkingDirectory: module, ConfigFileName: projectMarker})
	if locateProjectError == nil {
		test.Fatal("a go.work that does not parse was read as no workspace")
	}
	if !strings.Contains(locateProjectError.Error(), filepath.Join(root, "go.work")) {
		test.Errorf("the error does not name the go.work: %v", locateProjectError)
	}
}

// TestDotFromASubdirectoryIsThatSubdirectory holds the whole-tree answer to the root rather than to
// the working directory.
//
// Before the root could be found by walking up the two were the same directory, so comparing a named
// `.` against the working directory was correct. From `modules/tasks` it is not: `cohere .` there
// would have checked all of ahra, turning the narrowest thing a caller can type into the widest run.
func TestDotFromASubdirectoryIsThatSubdirectory(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"Top.ts":           "",
		"modules/Inner.ts": "",
	})
	modules := filepath.Join(root, "modules")

	scope, namedPathsScopeError := namedPathsScope(modules, root, []string{"."})
	if namedPathsScopeError != nil {
		test.Fatal(namedPathsScopeError)
	}
	if scope.Everything {
		test.Fatal("`.` from a subdirectory became the whole project")
	}
	if !scope.includes(filepath.Join(modules, "Inner.ts")) || scope.includes(filepath.Join(root, "Top.ts")) {
		test.Errorf("`.` did not mean the subdirectory: %v", scope.FileNames)
	}

	// The control: `.` at the root still means everything.
	whole, namedPathsScopeError := namedPathsScope(root, root, []string{"."})
	if namedPathsScopeError != nil {
		test.Fatal(namedPathsScopeError)
	}
	if !whole.Everything {
		test.Errorf("`.` at the root stopped meaning the whole tree: %v", whole.FileNames)
	}
}
