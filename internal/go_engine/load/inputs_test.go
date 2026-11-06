package load

import (
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
// file, an object file go lists among its other files, a file the build constraints leave out, and a test; a
// directory with no Go files below one with some; a nested module, named and not walked; and testdata, a dot
// directory and an underscore directory, which `./...` never enters.
var inputsFixtureFiles = map[string]string{
	"module/go.mod": "module example.com/inputs\n\ngo 1.21\n",
	"module/go.sum": "",
	"module/p/p.go": "package p\n\nimport _ \"embed\"\n\n//go:embed message.txt\n" +
		"var Message string\n",
	"module/p/message.txt": "hello\n",
	"module/p/object.syso": "not an object file, which go list only lists\n",
	"module/p/ignored.go":  "//go:build ignore\n\npackage p\n",
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
		census := []string{
			filepath.Join(inputsFixture.module, "p", "p.go"),
			filepath.Join(inputsFixture.module, "p", "ignored.go"),
			filepath.Join(inputsFixture.module, "p", "p_test.go"),
			filepath.Join(inputsFixture.module, "nested", "nested.go"),
		}
		inputsFixture.loaded, inputsFixture.failure = Packages(inputsFixture.module, []string{"./..."},
			[]string{"GOWORK=", "CGO_ENABLED=0"}, census)
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
			for _, file := range value.FieldByName(list).Interface().([]string) {
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

// The inputs name the module's manifests, the census, the toolchain and every directory `./...` could reach, and
// nothing it never enters; with no go.work in use, a go.work above the module is absent.
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
		"p/object.syso", "p/ignored.go", "nested/nested.go"} {
		if !slices.Contains(inputs.Files, inModule(relative)) {
			test.Errorf("the files do not name %s", relative)
		}
	}
	goRoot := ""
	for _, file := range inputs.Files {
		if strings.HasSuffix(file, filepath.Join("src", "embed", "embed.go")) {
			goRoot = filepath.Dir(filepath.Dir(filepath.Dir(file)))
		}
	}
	if goRoot == "" || !slices.Contains(inputs.Files, filepath.Join(goRoot, "VERSION")) {
		test.Errorf("the files name no standard library's embed.go with its toolchain's VERSION beside it (root %q)",
			goRoot)
	}
	if !slices.ContainsFunc(inputs.Files, func(file string) bool { return filepath.Base(file) == "go" }) {
		test.Error("the files do not name the go executable")
	}

	for _, relative := range []string{".", "p", "nested", "nested/empty", "inner"} {
		if !slices.Contains(inputs.Directories, inModule(relative)) {
			test.Errorf("the directories do not name %s", relative)
		}
	}
	for _, relative := range []string{"testdata", "testdata/deeper", ".hidden", "_skipped", "inner/below"} {
		if slices.Contains(inputs.Directories, inModule(relative)) {
			test.Errorf("the directories name %s, which ./... never enters", relative)
		}
	}
	if goRoot != "" && !slices.Contains(inputs.Directories, filepath.Join(goRoot, "src", "embed")) {
		test.Errorf("the directories do not name the standard library's embed package")
	}

	for _, ancestor := range []string{inputsFixture.module, inputsFixture.root, filepath.Dir(inputsFixture.root)} {
		if !slices.Contains(inputs.Absent, filepath.Join(ancestor, "go.work")) {
			test.Errorf("no go.work is in use, and %s's go.work is not absent", ancestor)
		}
	}
}
