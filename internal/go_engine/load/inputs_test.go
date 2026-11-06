package load

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
)

// inputsFixtureFiles is a module whose load reads a little of everything the inputs name: a package with an embedded
// file and an embedded directory inside its testdata, an object file go lists among its other files, a file the build
// constraints leave out, and a test; a directory with no Go files below one with some; a nested module, named and not
// walked; a module beside it that a replace directive names, walked as its own, and one in the directory above it,
// whose listing is never an input; a build cache inside the module, as `GOCACHE` can put one; and testdata, a dot
// directory, an underscore directory and a `.cache` like the front door's, which `./...` never enters.
var inputsFixtureFiles = map[string]string{
	"module/go.mod": "module example.com/inputs\n\ngo 1.21\n\n" +
		"require (\n\texample.com/above v0.0.0\n\texample.com/sibling v0.0.0\n)\n\n" +
		"replace example.com/sibling => ../sibling\n\nreplace example.com/above => ..\n",
	"module/go.sum": "",
	"module/p/p.go": "package p\n\nimport (\n\t\"embed\"\n\n\t\"example.com/above\"\n" +
		"\t\"example.com/sibling\"\n)\n\n" +
		"//go:embed message.txt\nvar Message string\n\n//go:embed testdata/assets\nvar Assets embed.FS\n\n" +
		"var Name = sibling.Name + above.Name\n",
	"module/p/message.txt":               "hello\n",
	"module/p/testdata/assets/page.html": "<p>hello</p>\n",
	"module/.cache/cohere/table":         "written by the front door after every run\n",
	"module/gocache/ab/entry":            "a build cache `GOCACHE` put inside the module\n",
	"go.mod":                             "module example.com/above\n\ngo 1.21\n",
	"above.go":                           "package above\n\nconst Name = \"above\"\n",
	"sibling/go.mod":                     "module example.com/sibling\n\ngo 1.21\n",
	"sibling/sibling.go":                 "package sibling\n\nconst Name = \"sibling\"\n",
	"sibling/deeper/notes.txt":           "a directory with no Go files in the replacement\n",
	"module/p/object.syso":               "not an object file, which go list only lists\n",
	"module/p/ignored.go":                "//go:build ignore\n\npackage p\n",
	"module/p/p_test.go": "package p\n\nimport \"testing\"\n\n" +
		"func TestMessage(test *testing.T) { _ = Message }\n",
	"module/nested/nested.go":         "package nested\n",
	"module/nested/empty/notes.txt":   "a directory with no Go files\n",
	"module/inner/go.mod":             "module example.com/inner\n\ngo 1.21\n",
	"module/inner/below/below.go":     "package below\n",
	"module/testdata/fixture.go":      "package fixture\n",
	"module/.hidden/hidden.go":        "package hidden\n",
	"module/_skipped/skipped.go":      "package skipped\n",
	"module/testdata/deeper/notes.go": "package deeper\n",
}

// inputsFixture is the module written and loaded once per test binary, with `GOWORK` empty, so go looks for a
// go.work above it and finds none.
var inputsFixture struct {
	once    sync.Once
	root    string
	module  string
	loaded  *Loaded
	failure error
}

// inputsFixtureEnvironment is what the fixture loads under: `GOWORK` empty, so go looks for a go.work and finds none,
// and `GOCACHE` inside the module.
func inputsFixtureEnvironment() []string {
	return []string{"GOWORK=", "CGO_ENABLED=0", "GOCACHE=" + inModule("gocache")}
}

// inputsFixtureCensus is the fixture module's own .go files, as the project's census would list them.
func inputsFixtureCensus() []string {
	return []string{inModule("p/p.go"), inModule("p/ignored.go"), inModule("p/p_test.go"), inModule("nested/nested.go")}
}

// loadedInputsFixture writes and loads the inputs fixture on first use.
func loadedInputsFixture(test *testing.T) {
	test.Helper()
	inputsFixture.once.Do(func() {
		inputsFixture.root, inputsFixture.failure = os.MkdirTemp("", "cohere-go-engine-inputs-")
		if inputsFixture.failure != nil {
			return
		}
		// Resolved, so the paths go reports, through /private on a Mac, are the paths the test compares.
		inputsFixture.root, inputsFixture.failure = filepath.EvalSymlinks(inputsFixture.root)
		if inputsFixture.failure != nil {
			return
		}
		for name, contents := range inputsFixtureFiles {
			path := filepath.Join(inputsFixture.root, filepath.FromSlash(name))
			if inputsFixture.failure = os.MkdirAll(filepath.Dir(path), 0o755); inputsFixture.failure != nil {
				return
			}
			if inputsFixture.failure = os.WriteFile(path, []byte(contents), 0o644); inputsFixture.failure != nil {
				return
			}
		}
		inputsFixture.module = filepath.Join(inputsFixture.root, "module")
		inputsFixture.loaded, inputsFixture.failure = Packages(inputsFixture.module, []string{"./..."},
			inputsFixtureEnvironment(), inputsFixtureCensus())
	})
	if inputsFixture.failure != nil {
		test.Fatalf("loading the inputs fixture: %v", inputsFixture.failure)
	}
}

// inModule is a path of the fixture module, from its slash-separated name.
func inModule(relative string) string {
	return filepath.Join(inputsFixture.module, filepath.FromSlash(relative))
}

// The completeness guard: every file any package of the load lists, in any of go/packages' lists of files, is one of
// the inputs' files. The lists are found by reflection, every []string field of packages.Package named for files, so
// a loader that fills one more list fails here rather than leaving the replay to go stale.
func TestInputsHoldEveryFileTheLoadListed(test *testing.T) {
	test.Parallel()

	loadedInputsFixture(test)
	named := map[string]bool{}
	for _, file := range inputsFixture.loaded.Inputs.Files {
		named[file] = true
	}
	packageType := reflect.TypeFor[packages.Package]()
	var lists []string
	for index := range packageType.NumField() {
		field := packageType.Field(index)
		if field.Type == reflect.TypeFor[[]string]() && strings.HasSuffix(field.Name, "Files") {
			lists = append(lists, field.Name)
		}
	}
	for _, want := range []string{"GoFiles", "CompiledGoFiles", "OtherFiles", "EmbedFiles", "IgnoredFiles"} {
		if !slices.Contains(lists, want) {
			test.Fatalf("packages.Package has no %s among its lists of files %v", want, lists)
		}
	}
	listed := map[string]bool{}
	packages.Visit(inputsFixture.loaded.Roots, nil, func(visited *packages.Package) {
		value := reflect.ValueOf(visited).Elem()
		for _, list := range lists {
			files, isFileList := value.FieldByName(list).Interface().([]string)
			if !isFileList {
				test.Fatalf("packages.Package's %s is not a list of files", list)
			}
			for _, file := range files {
				listed[list] = true
				if !named[filepath.Clean(file)] {
					test.Errorf("%s lists %s in %s, and the inputs do not name it", visited.ID, file, list)
				}
			}
		}
	})
	// Each list the fixture fills is one the guard was held to, so none passes for being empty.
	for _, list := range []string{"GoFiles", "CompiledGoFiles", "OtherFiles", "EmbedFiles", "IgnoredFiles"} {
		if !listed[list] {
			test.Errorf("no package of the fixture lists anything in %s, so the guard held nothing there", list)
		}
	}
}

// The inputs name the module's manifests, the census, the toolchain that ran and every directory `./...` could reach
// in the module and its local replacement, with an embedded directory, and no directory it never enters, none of the
// toolchain's and none above the module; with no go.work in use, a go.work above the module is absent.
func TestInputsNameWhatTheLoadRead(test *testing.T) {
	test.Parallel()

	loadedInputsFixture(test)
	inputs := inputsFixture.loaded.Inputs
	for _, list := range [][]string{inputs.Files, inputs.Directories, inputs.Absent} {
		if !slices.IsSorted(list) || len(slices.Compact(slices.Clone(list))) != len(list) {
			test.Errorf("a list is not sorted without duplicates: %q", list)
		}
		for _, path := range list {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				test.Errorf("%s is not absolute and clean", path)
			}
		}
	}

	for _, relative := range []string{"go.mod", "go.sum", "p/p.go", "p/p_test.go", "p/message.txt",
		"p/testdata/assets/page.html", "p/object.syso", "p/ignored.go", "nested/nested.go", "../sibling/go.mod",
		"../sibling/sibling.go"} {
		if !slices.Contains(inputs.Files, inModule(relative)) {
			test.Errorf("the files do not name %s", relative)
		}
	}
	// The build-tagged-out file is named because the load lists it among the ignored, not only through the census.
	ignoredListed := false
	packages.Visit(inputsFixture.loaded.Roots, nil, func(visited *packages.Package) {
		ignoredListed = ignoredListed || slices.Contains(visited.IgnoredFiles, inModule("p/ignored.go"))
	})
	if !ignoredListed {
		test.Error("no package lists p/ignored.go among its IgnoredFiles")
	}
	goRoot := ""
	for _, file := range inputs.Files {
		if strings.HasSuffix(file, filepath.Join("src", "embed", "embed.go")) {
			goRoot = filepath.Dir(filepath.Dir(filepath.Dir(file)))
		}
	}
	for _, name := range []string{"VERSION", "go.env", filepath.Join("bin", "go")} {
		if goRoot == "" || !slices.Contains(inputs.Files, filepath.Join(goRoot, name)) {
			test.Errorf("the files name no %s of the toolchain whose embed.go was loaded (root %q)", name, goRoot)
		}
	}
	if !slices.ContainsFunc(inputs.Files, func(file string) bool { return filepath.Base(file) == "go" }) {
		test.Error("the files do not name the go executable")
	}

	for _, relative := range []string{".", "p", "p/testdata", "p/testdata/assets", "nested", "nested/empty", "inner",
		"../sibling", "../sibling/deeper"} {
		if !slices.Contains(inputs.Directories, inModule(relative)) {
			test.Errorf("the directories do not name %s", relative)
		}
	}
	for _, relative := range []string{"testdata", "testdata/deeper", ".hidden", "_skipped", "inner/below", ".cache",
		".cache/cohere", "gocache", "gocache/ab", ".."} {
		if slices.Contains(inputs.Directories, inModule(relative)) {
			test.Errorf("the directories name %s, which ./... never enters or which is above the module", relative)
		}
	}
	for _, directory := range inputs.Directories {
		if goRoot != "" && strings.HasPrefix(directory, goRoot+string(filepath.Separator)) {
			test.Errorf("the directories name %s, the toolchain's, which its VERSION stands for", directory)
			break
		}
	}

	for _, ancestor := range []string{inputsFixture.module, inputsFixture.root, filepath.Dir(inputsFixture.root)} {
		if !slices.Contains(inputs.Absent, filepath.Join(ancestor, "go.work")) {
			test.Errorf("no go.work is in use, and %s's go.work is not absent", ancestor)
		}
	}
}

// Two loads of a tree nothing touched give the same inputs, byte for byte once encoded: a list that moved between them
// would make the front door's record miss on every run.
func TestInputsOfAnUnchangedTreeAreTheSameEveryLoad(test *testing.T) {
	test.Parallel()

	loadedInputsFixture(test)
	again, loadError := Packages(inputsFixture.module, []string{"./..."}, inputsFixtureEnvironment(),
		inputsFixtureCensus())
	if loadError != nil {
		test.Fatal(loadError)
	}
	first, firstError := json.Marshal(inputsFixture.loaded.Inputs)
	second, secondError := json.Marshal(again.Inputs)
	if firstError != nil || secondError != nil {
		test.Fatalf("encoding the inputs: %v, %v", firstError, secondError)
	}
	if !bytes.Equal(first, second) {
		test.Errorf("two loads of the same tree gave different inputs:\n%s\n%s", first, second)
	}
}

// The toolchain named is the one that ran, from the root go env reported in the load's environment: with `GOTOOLCHAIN`
// switching to one a go.mod asks for, that is a toolchain in the module cache, not PATH's go, so its go, `VERSION` and
// go.env are named whatever PATH holds, and the build cache's files never are.
func TestInputsNameTheToolchainThatRan(test *testing.T) {
	test.Parallel()

	switched := test.TempDir()
	buildCache := test.TempDir()
	for _, name := range []string{filepath.Join("bin", "go"), "VERSION", "go.env"} {
		path := filepath.Join(switched, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte("x\n"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	translated := filepath.Join(buildCache, "ab", "translated.go")
	inputs := collectInputs(test.TempDir(), reportedEnvironment{goRoot: switched, workspaceFile: "off",
		buildCache: buildCache}, []*packages.Package{{ID: "p", CompiledGoFiles: []string{translated}}}, nil)
	for _, name := range []string{filepath.Join("bin", "go"), "VERSION", "go.env"} {
		if !slices.Contains(inputs.Files, filepath.Join(switched, name)) {
			test.Errorf("the files do not name the toolchain's %s", name)
		}
	}
	if slices.Contains(inputs.Files, translated) {
		test.Errorf("the files name %s, which go writes into its build cache during the run", translated)
	}
}
