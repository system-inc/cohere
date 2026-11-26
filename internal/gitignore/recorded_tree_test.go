package gitignore

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
)

// A tree git answered for, as testdata/recorded keeps it: its files as a txtar archive, the index git wrote for each
// of its repositories, and git check-ignore's verbose answer for each path. How git made them is in that directory's
// README.md.

// recordedTrees is where the trees git answered for are kept, a directory per tree.
var recordedTrees = filepath.Join("testdata", "recorded")

// globCharacters are the bytes that make check-ignore read a path as a glob against the index. A path holding one was
// recorded with `--no-index`, its tracking left to the index (recordedTree.answers).
const globCharacters = `*?[\`

// verdict is one path's answer.
type verdict struct {
	ignored bool
	source  Source
}

// recordedTree is one tree git answered for: its files, from tree.txtar, and its repositories, from the archive's
// comment, each with the paths its index tracks.
type recordedTree struct {
	directory    string
	archive      *txtar.Archive
	repositories []recordedRepository
}

// recordedRepository is one repository of a recorded tree: its directory relative to the tree, "." for the root,
// and the paths its index tracks, relative to it, sorted by bytes as the index sorts them.
type recordedRepository struct {
	path    string
	tracked []string
}

// recordingName reads a name as the recordings write it: as it is, or Go-quoted when it would not survive as it
// is, as a name with a space at its end, which txtar trims from a file's name, would not.
func recordingName(test *testing.T, written string) string {
	test.Helper()
	if !strings.HasPrefix(written, `"`) {
		return written
	}
	name, unquoteError := strconv.Unquote(written)
	if unquoteError != nil {
		test.Fatalf("the recorded name %s: %v", written, unquoteError)
	}
	return name
}

// readRecordedTree reads the tree recorded in directory. Its archive's comment names each repository on a line
// `repository <path>`, followed by the paths its index tracks, one to a line, each indented by a tab.
func readRecordedTree(test *testing.T, directory string) recordedTree {
	test.Helper()
	archive, parseError := txtar.ParseFile(filepath.Join(directory, "tree.txtar"))
	if parseError != nil {
		test.Fatal(parseError)
	}
	tree := recordedTree{directory: directory, archive: archive}
	for _, line := range strings.Split(string(archive.Comment), "\n") {
		if repositoryPath, found := strings.CutPrefix(line, "repository "); found {
			tree.repositories = append(tree.repositories, recordedRepository{path: repositoryPath})
			continue
		}
		if tracked, found := strings.CutPrefix(line, "\t"); found && len(tree.repositories) > 0 {
			repository := &tree.repositories[len(tree.repositories)-1]
			repository.tracked = append(repository.tracked, recordingName(test, tracked))
		}
	}
	if len(tree.repositories) == 0 || tree.repositories[0].path != "." {
		test.Fatalf("%s names no repository at its root", directory)
	}
	return tree
}

// recordingBase is the start of a repository's recordings' names: "repository" for the root, and "repository-" and
// its path, slashes as dashes, for one nested in it.
func recordingBase(repositoryPath string) string {
	if repositoryPath == "." {
		return "repository"
	}
	return "repository-" + strings.ReplaceAll(repositoryPath, "/", "-")
}

// treePath is relative, a path in the repository at repositoryPath, as a path in its tree.
func treePath(repositoryPath string, relative string) string {
	if repositoryPath == "." {
		return relative
	}
	return repositoryPath + "/" + relative
}

// lay writes the tree under root, and places each repository's recorded index at its `.git/index`, as placeIndex
// places one.
func (tree recordedTree) lay(test *testing.T, root string) {
	test.Helper()
	files := map[string]string{}
	for _, file := range tree.archive.Files {
		files[recordingName(test, file.Name)] = string(file.Data)
	}
	writeTree(test, root, files)
	for _, repository := range tree.repositories {
		contents, readError := os.ReadFile(filepath.Join(tree.directory, recordingBase(repository.path)+".index"))
		if readError != nil {
			test.Fatal(readError)
		}
		placeIndex(test, filepath.Join(root, filepath.FromSlash(repository.path), ".git"), contents)
	}
}

// answers is git's verbose answer for every path in repository, in two parts. literal is check-ignore's, read with
// the index, for each path holding none of globCharacters. globbed is check-ignore's with `--no-index` for each path
// holding one, since check-ignore would read such a path as a glob against the index, and refuses
// `--literal-pathspecs`, which would fix that; one the index tracks as spelled, or a directory holding a path it
// tracks, takes no rule, which is check-ignore's answer for a tracked path.
func (tree recordedTree) answers(test *testing.T, repository recordedRepository) (literal map[string]verdict,
	globbed map[string]verdict,
) {
	test.Helper()
	base := filepath.Join(tree.directory, recordingBase(repository.path))
	literal = readCheckIgnore(test, base+".check-ignore")
	globbed = map[string]verdict{}
	if _, statError := os.Stat(base + ".check-ignore-no-index"); statError != nil {
		return literal, globbed
	}
	for relative, answer := range readCheckIgnore(test, base+".check-ignore-no-index") {
		if slices.ContainsFunc(repository.tracked, func(trackedPath string) bool {
			return trackedPath == relative || strings.HasPrefix(trackedPath, relative+"/")
		}) {
			answer = verdict{}
		}
		globbed[relative] = answer
	}
	return literal, globbed
}

// readCheckIgnore reads `git check-ignore --verbose --non-matching --stdin` as git printed it: a line per path,
// `file:line:pattern`, or `::` for none, a tab, and the path, the file and the path C-quoted when they hold a byte
// that needs it.
func readCheckIgnore(test *testing.T, file string) map[string]verdict {
	test.Helper()
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatal(readError)
	}
	answers := map[string]verdict{}
	for number, line := range strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n") {
		if line == "" {
			continue
		}
		relative, answer, parseError := parseVerboseLine(line)
		if parseError != nil {
			test.Fatalf("%s:%d: %v", file, number+1, parseError)
		}
		if _, repeated := answers[relative]; repeated {
			test.Fatalf("%s:%d: a second answer for %s", file, number+1, relative)
		}
		answers[relative] = answer
	}
	return answers
}

// parseVerboseLine reads one line of check-ignore's verbose answer. The path follows the last tab, since git quotes
// a path holding one.
func parseVerboseLine(line string) (string, verdict, error) {
	tab := strings.LastIndexByte(line, '\t')
	if tab < 0 {
		return "", verdict{}, fmt.Errorf("%q holds no tab", line)
	}
	source, quotedPath := line[:tab], line[tab+1:]
	relative, unquoteError := unquoteGitName(quotedPath)
	if unquoteError != nil {
		return "", verdict{}, unquoteError
	}
	if source == "::" {
		return relative, verdict{}, nil
	}
	file, rest := source, ""
	if strings.HasPrefix(source, `"`) {
		closing := 1
		for closing < len(source) && source[closing] != '"' {
			if source[closing] == '\\' {
				closing++
			}
			closing++
		}
		if closing >= len(source) {
			return "", verdict{}, fmt.Errorf("%q opens a quote it never closes", source)
		}
		file, rest = source[:closing+1], source[closing+1:]
		var cut bool
		if rest, cut = strings.CutPrefix(rest, ":"); !cut {
			return "", verdict{}, fmt.Errorf("%q has no line after its file", source)
		}
	} else {
		var cut bool
		if file, rest, cut = strings.Cut(source, ":"); !cut {
			return "", verdict{}, fmt.Errorf("%q has no line after its file", source)
		}
	}
	file, unquoteError = unquoteGitName(file)
	if unquoteError != nil {
		return "", verdict{}, unquoteError
	}
	lineText, pattern, cut := strings.Cut(rest, ":")
	if !cut {
		return "", verdict{}, fmt.Errorf("%q has no pattern after its line", source)
	}
	lineNumber, parseError := strconv.Atoi(lineText)
	if parseError != nil {
		return "", verdict{}, fmt.Errorf("%q names the line %q: %w", source, lineText, parseError)
	}
	return relative, verdict{
		ignored: !strings.HasPrefix(pattern, "!"),
		source:  Source{File: file, Line: lineNumber, Pattern: pattern},
	}, nil
}

// unquoteGitName reads a name git C-quoted, as it quotes one holding a backslash, a quote or a control byte, whose
// escapes are all Go's too, or one it left as it is.
func unquoteGitName(written string) (string, error) {
	if !strings.HasPrefix(written, `"`) {
		return written, nil
	}
	name, unquoteError := strconv.Unquote(written)
	if unquoteError != nil {
		return "", fmt.Errorf("git's quoted name %s: %w", written, unquoteError)
	}
	return name, nil
}

// recordedCaseDirectories is every tree recorded under family, by name.
func recordedCaseDirectories(test *testing.T, family string) []string {
	test.Helper()
	entries, readError := os.ReadDir(filepath.Join(recordedTrees, family))
	if readError != nil {
		test.Fatal(readError)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}
