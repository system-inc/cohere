package git_tree_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/git_tree"
)

// testRepository is a git repository in a test's temporary directory, which the test's git runs in
// with nothing of the machine's: no global or system configuration, no GIT_ variable of the
// process's, a fixed author and date, and core.fileMode and core.symlinks on, so git's answers are its
// defaults' on every machine.
type testRepository struct {
	test      *testing.T
	directory string
	format    git_tree.ObjectFormat
}

// testGitConfiguration is what every git command a test runs is given on its command line.
var testGitConfiguration = []string{
	"-c", "core.fileMode=true",
	"-c", "core.symlinks=true",
	"-c", "core.autocrlf=false",
	"-c", "commit.gpgsign=false",
	"-c", "init.defaultBranch=main",
	"-c", "protocol.file.allow=always",
}

// newTestRepository makes an empty repository of format.
func newTestRepository(test *testing.T, format git_tree.ObjectFormat) *testRepository {
	test.Helper()
	repository := &testRepository{test: test, directory: test.TempDir(), format: format}
	repository.git("init", "-q", "--object-format="+string(format))
	return repository
}

// git runs git in the repository and returns its standard output, failing the test with its standard
// error when it fails.
func (repository *testRepository) git(arguments ...string) []byte {
	repository.test.Helper()
	output, standardError, outputError := repository.run(arguments...)
	if outputError != nil {
		repository.test.Fatalf("git %q in %s: %v\n%s", arguments, repository.directory, outputError, standardError)
	}
	return output
}

// gitFails runs git in the repository for a command meant to fail, as a merge left in conflict does,
// failing the test when it succeeds.
func (repository *testRepository) gitFails(arguments ...string) {
	repository.test.Helper()
	if output, _, outputError := repository.run(arguments...); outputError == nil {
		repository.test.Fatalf(
			"git %q in %s succeeded, and was meant to fail:\n%s",
			arguments,
			repository.directory,
			output,
		)
	}
}

// run runs git in the repository, returning its standard output, its standard error and how it ended.
func (repository *testRepository) run(arguments ...string) ([]byte, string, error) {
	command := exec.Command("git", append(append([]string{}, testGitConfiguration...), arguments...)...)
	command.Dir = repository.directory
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "GIT_") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env,
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_AUTHOR_DATE=1700000000 +0000",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid", "GIT_COMMITTER_DATE=1700000000 +0000",
	)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, outputError := command.Output()
	return output, standardError.String(), outputError
}

// line is git's output with its one trailing newline trimmed.
func (repository *testRepository) line(arguments ...string) string {
	repository.test.Helper()
	return strings.TrimSuffix(string(repository.git(arguments...)), "\n")
}

// path is name's path on disk.
func (repository *testRepository) path(name string) string {
	return filepath.Join(repository.directory, filepath.FromSlash(name))
}

// write puts content at name in the repository with exactly permissions, whatever the umask.
func (repository *testRepository) write(name, content string, permissions os.FileMode) {
	repository.test.Helper()
	if mkdirError := os.MkdirAll(filepath.Dir(repository.path(name)), 0o755); mkdirError != nil {
		repository.test.Fatal(mkdirError)
	}
	if writeError := os.WriteFile(repository.path(name), []byte(content), permissions); writeError != nil {
		repository.test.Fatal(writeError)
	}
	repository.chmod(name, permissions)
}

// chmod sets name's permissions in the repository.
func (repository *testRepository) chmod(name string, permissions os.FileMode) {
	repository.test.Helper()
	if chmodError := os.Chmod(repository.path(name), permissions); chmodError != nil {
		repository.test.Fatal(chmodError)
	}
}

// link makes name in the repository a symbolic link to target.
func (repository *testRepository) link(name, target string) {
	repository.test.Helper()
	if mkdirError := os.MkdirAll(filepath.Dir(repository.path(name)), 0o755); mkdirError != nil {
		repository.test.Fatal(mkdirError)
	}
	if symlinkError := os.Symlink(target, repository.path(name)); symlinkError != nil {
		repository.test.Fatal(symlinkError)
	}
}

// remove deletes name, a file or an empty directory, in the repository.
func (repository *testRepository) remove(name string) {
	repository.test.Helper()
	if removeError := os.Remove(repository.path(name)); removeError != nil {
		repository.test.Fatal(removeError)
	}
}

// commit records everything in the work tree as a commit and returns its id.
func (repository *testRepository) commit(message string) string {
	repository.test.Helper()
	repository.git("add", "-A")
	repository.git("commit", "-q", "--allow-empty", "-m", message)
	return repository.line("rev-parse", "HEAD")
}
