package project

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/git_tree"
	"github.com/system-inc/cohere/internal/testing/written_repository"
)

// fixture is the package's one tree, built once and only read: a repository whose go.work uses its root
// module, inner, and a module inside the library submodule, beside a module in no repository at all.
type fixture struct {
	// repository holds the go.work. Beside the modules it uses it holds what neither the gate nor the census
	// may take for ours: testdata, a dot directory, an underscore directory, nested (a module go.work does not
	// use), and files its ignore files exclude. Its root go.mod's `ignore` directives name ./generated,
	// anchored, tools/scripts, at any depth, and ./deep/inside, a nested path; deep/generated is kept.
	repository string

	// lone is a go.mod with no go.work around it, outside the repository.
	lone string

	// outside is a go.work of two modules in no repository, as a fresh `go work init` in a temporary
	// directory is.
	outside string

	// broken is a go.work whose `.git` is a file pointing at a git directory that is not there, so it is in a
	// repository git cannot read.
	broken string
}

// fixtureBase is the temporary directory the fixture is built in, removed once every test is done.
var fixtureBase string

var sharedFixture = sync.OnceValues(func() (fixture, error) {
	base, baseError := os.MkdirTemp("", "cohere-go-language-project-")
	if baseError != nil {
		return fixture{}, baseError
	}
	fixtureBase = base
	return buildFixture(base)
})

// TestMain removes the fixture once every test is done with it.
func TestMain(suite *testing.M) {
	code := suite.Run()
	if fixtureBase != "" {
		// Ignored: a leftover temporary directory fails nothing, and the exit code is the run's.
		_ = os.RemoveAll(fixtureBase)
	}
	os.Exit(code)
}

// readFixture is the shared fixture, for a test that only reads it.
func readFixture(test *testing.T) fixture {
	test.Helper()
	built, buildError := sharedFixture()
	if buildError != nil {
		test.Fatalf("building the fixture: %v", buildError)
	}
	return built
}

func buildFixture(base string) (fixture, error) {
	built := fixture{
		repository: filepath.Join(base, "repository"),
		lone:       filepath.Join(base, "lone"),
		outside:    filepath.Join(base, "outside"),
		broken:     filepath.Join(base, "broken"),
	}
	repositoryFiles := map[string]string{
		"go.work": "go 1.25\n\nuse (\n\t.\n\t./inner\n\t./library/mod\n)\n",
		"go.mod": "module example.com/root\n\ngo 1.25\n\n" +
			"ignore (\n\t./generated\n\ttools/scripts\n\t./deep/inside\n)\n",
		".gitignore":                       "scratch.go\nbuild/\n",
		"main.go":                          "package root\n",
		"_draft.go":                        "package root\n",
		".draft.go":                        "package root\n",
		"drafts/_only.go":                  "package drafts\n",
		"drafts/.only.go":                  "package drafts\n",
		"notes.txt":                        "not go\n",
		"scratch.go":                       "package root\n",
		"build/generated.go":               "package build\n",
		"testdata/fixture/go.mod":          "module example.com/fixture\n\ngo 1.21\n",
		"testdata/fixture/fixture.go":      "package fixture\n",
		".cache/downloaded/go.mod":         "module example.com/downloaded\n\ngo 1.21\n",
		".cache/downloaded/downloaded.go":  "package downloaded\n",
		".hidden/hidden.go":                "package hidden\n",
		"_scratch/underscore.go":           "package underscore\n",
		"nested/go.mod":                    "module example.com/nested\n\ngo 1.21\n",
		"nested/nested.go":                 "package nested\n",
		"nested/scratch.go":                "package nested\n",
		"inner/go.mod":                     "module example.com/root/inner\n\ngo 1.21\n",
		"inner/.gitignore":                 "local.go\n",
		"inner/inner.go":                   "package inner\n",
		"inner/local.go":                   "package inner\n",
		"inner/deeper/deeper.go":           "package deeper\n",
		"inner/deeper/testdata/skipped.go": "package skipped\n",
		"vendor/vendored.go":               "package vendor\n",
		"vendor/library/library.go":        "package library\n",
		"generated/generated.go":           "package generated\n",
		"deep/deep.go":                     "package deep\n",
		"deep/generated/kept.go":           "package generated\n",
		"deep/inside/inside.go":            "package inside\n",
		"deep/inside/below/below.go":       "package below\n",
		"tools/tool.go":                    "package tools\n",
		"tools/scripts/script.go":          "package scripts\n",
		"deep/tools/scripts/script.go":     "package scripts\n",
	}
	if writeError := writeFiles(built.repository, repositoryFiles); writeError != nil {
		return fixture{}, writeError
	}
	// The library submodule as `git submodule add` leaves it, written without git: its files, its `.git` a file
	// pointing into the parent's git directory, which holds its repository with both files tracked, and the parent's
	// index tracking .gitmodules and the library's gitlink, pinned at a commit no repository holds.
	libraryFiles := map[string]string{
		"library/mod/go.mod":     "module example.com/library/mod\n\ngo 1.21\n",
		"library/mod/library.go": "package mod\n",
		"library/.git":           "gitdir: ../.git/modules/library\n",
		".gitmodules":            "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
	}
	if writeError := writeFiles(built.repository, libraryFiles); writeError != nil {
		return fixture{}, writeError
	}
	libraryGitDirectory := filepath.Join(built.repository, ".git", "modules", "library")
	libraryWorktree := filepath.Join(built.repository, "library")
	writeError := written_repository.WriteGitDirectory(libraryGitDirectory, libraryWorktree,
		trackedFile("mod/go.mod"), trackedFile("mod/library.go"))
	if writeError != nil {
		return fixture{}, writeError
	}
	writeError = written_repository.Write(built.repository,
		trackedFile(".gitmodules"), gitlink("library", strings.Repeat("1", 40)))
	if writeError != nil {
		return fixture{}, writeError
	}

	loneFiles := map[string]string{"go.mod": "module example.com/lone\n\ngo 1.21\n", "lone.go": "package lone\n"}
	if writeError := writeFiles(built.lone, loneFiles); writeError != nil {
		return fixture{}, writeError
	}
	outsideFiles := map[string]string{
		"go.work":     "go 1.25\n\nuse (\n\t.\n\t./tool\n)\n",
		"go.mod":      "module example.com/outside\n\ngo 1.25\n",
		"tool/go.mod": "module example.com/outside/tool\n\ngo 1.25\n",
	}
	if writeError := writeFiles(built.outside, outsideFiles); writeError != nil {
		return fixture{}, writeError
	}
	brokenFiles := map[string]string{
		"go.work": "go 1.25\n\nuse .\n",
		"go.mod":  "module example.com/broken\n\ngo 1.25\n",
		".git":    "gitdir: " + filepath.Join(base, "missing-git-directory") + "\n",
	}
	if writeError := writeFiles(built.broken, brokenFiles); writeError != nil {
		return fixture{}, writeError
	}
	return built, nil
}

func writeFiles(directory string, files map[string]string) error {
	for name, contents := range files {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			return makeError
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			return writeError
		}
	}
	return nil
}

// trackedFile is path tracked as `git add` records it, its mode and blob read from the file
// (written_repository.WriteGitDirectory).
func trackedFile(path string) git_tree.IndexEntry {
	return git_tree.IndexEntry{Entry: git_tree.Entry{Path: path}}
}

// gitlink is path tracked as a submodule pinned at commit, as `git update-index --cacheinfo` records one: the
// commit need be in no repository, as GitlinksOf reads the index alone.
func gitlink(path string, commit string) git_tree.IndexEntry {
	return git_tree.IndexEntry{Entry: git_tree.Entry{Mode: git_tree.ModeGitlink, Object: commit, Path: path}}
}
