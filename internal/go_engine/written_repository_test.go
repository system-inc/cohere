package go_engine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// writtenRepositoryFiles are the files writeRepository writes below `.git`, by path, with their bytes: what `git init`
// writes there that git needs to read the repository, `HEAD` and the config, and the info/exclude git's template gives
// every repository, which the engine names among a run's inputs (pipeline_inputs.go). The config is the one `git init`
// writes under no config of the person's but for the lines it probes the file system for, and the engine reads only its
// object format, SHA-1's when it names none. No index, as `git init` writes none: the engine names the index absent.
var writtenRepositoryFiles = map[string]string{
	"HEAD":   "ref: refs/heads/main\n",
	"config": "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n\tlogallrefupdates = true\n",
	filepath.Join("info", "exclude"): "# git ls-files --others --exclude-from=.git/info/exclude\n" +
		"# Lines that start with '#' are comments.\n" +
		"# For a project mostly in C, the following would be a good set of\n" +
		"# exclude patterns (uncomment them if you want to use them):\n" +
		"# *.[oa]\n" +
		"# *~\n",
}

// writtenRepositoryDirectories are the directories writeRepository makes below `.git`, empty: git reads a directory as
// a repository only when it holds `HEAD`, objects and refs.
var writtenRepositoryDirectories = []string{"objects", filepath.Join("refs", "heads"), filepath.Join("refs", "tags")}

// writeRepository makes directory a git repository of its own, empty, without running git: the `.git` `git init`
// would write, as far as git and the engine read it. A test's git child forks the test process, and on darwin each
// fork makes every page of the heap fault again on its next write, so each fixture's `git init` cost the package's
// process kernel time it spent on nothing (#2htryg8). A repository with gitlinks needs git's own index, which
// git writes (copyFixture). TestAWrittenRepositoryReadsAsTheOneGitInitMakes holds the two alike.
func writeRepository(directory string) error {
	gitDirectory := filepath.Join(directory, ".git")
	for _, relative := range writtenRepositoryDirectories {
		if makeError := os.MkdirAll(filepath.Join(gitDirectory, relative), 0o755); makeError != nil {
			return makeError
		}
	}
	for relative, contents := range writtenRepositoryFiles {
		path := filepath.Join(gitDirectory, relative)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			return makeError
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			return writeError
		}
	}
	return nil
}

// A repository written without git (writeRepository) reads as the one `git init` makes: git reads it, finding it,
// telling its status and listing its index, as the engine lists a go.work root's gitlinks, and a run over a fixture's
// copy in each writes the same stream, its inputs record naming the same `.git` files.
func TestAWrittenRepositoryReadsAsTheOneGitInitMakes(test *testing.T) {
	test.Parallel()

	fixture := filepath.Join("testdata", "clean")
	written := copyFixtureFiles(test, fixture)
	if writeError := writeRepository(written); writeError != nil {
		test.Fatal(writeError)
	}
	initialized := copyFixtureFiles(test, fixture)
	runGit(test, initialized, "init", "-q")
	for _, arguments := range [][]string{
		{"rev-parse", "--git-dir"}, {"status", "--porcelain"}, {"ls-files", "-s", "-z"},
	} {
		runGit(test, written, arguments...)
	}
	streams := map[string]string{}
	for _, root := range []string{written, initialized} {
		var standardOutput bytes.Buffer
		var standardError bytes.Buffer
		exitCode := runIsolated(test, []string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-fix"},
			&standardOutput, &standardError, rules.All(), nil)
		if exitCode != exitClean {
			test.Fatalf("the run over %s exited %d: %s", root, exitCode, standardError.String())
		}
		streams[root] = normalize(standardOutput.String(), root)
	}
	if streams[written] != streams[initialized] {
		test.Errorf("over the written repository the run wrote\n%s\nover the one git init made\n%s", streams[written],
			streams[initialized])
	}
}
