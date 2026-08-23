package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/system-inc/verify/internal/fix"
)

// formatScope is which files the format phase considers, and how that set was decided.
//
// Formatting the whole tree is not a tuning problem, it is a different tool. Measured: the goja
// formatter runs 83 to 100ms per file with no warm-up, so 3,407 files is 4.7 to 5.7 minutes against
// a lint phase that finishes in 392ms. That is roughly 700 times the rest of the run, and a gate
// nobody will wait for is a gate that does not exist. Actual churn on this tree is one file per
// commit and 35 across five, so scoping to changed files puts the common case in the tens of
// milliseconds.
//
// The set is named as well as computed, because working-tree modifications, staged changes, and a
// diff against a base branch are three different answers to "what changed" and a reader cannot tell
// which one they got from the file count alone.
type formatScope struct {
	// FileNames is the absolute paths in scope, sorted. Nil means every file is in scope.
	FileNames []string

	// index is FileNames as a set, so membership is a map lookup rather than a scan.
	//
	// The transform asks about every project file, so a linear scan here would be quadratic across
	// the tree: 3,407 files against a few hundred changed ones is a million comparisons to answer a
	// question a map answers in one.
	index map[string]struct{}

	// Description says how the set was decided, in words that go straight into the coverage line.
	Description string

	// Everything is whether the scope is the whole tree rather than a subset.
	Everything bool
}

// includes reports whether a file is in scope. A whole-tree scope includes everything.
// includes reports whether a file is in a narrow scope.
//
// It deliberately does not consult Everything. That check lives in scopedTransform, which returns
// the unwrapped transform for a whole-tree scope so the filter is never reached at all. Testing it
// in both places looked defensive and was worse than useless: it made the bypass unobservable, so a
// mutant that deleted the fast path survived every fixture because the redundant check produced the
// same answer. Two guards where one is load-bearing means neither can be shown to work.
//
// A whole-tree scope never reaches this method. If one does, it has no index and answers false,
// which is a wrong answer arriving loudly rather than a right answer arriving by accident.
func (s formatScope) includes(fileName string) bool {
	_, inScope := s.index[fileName]
	return inScope
}

// wholeTreeScope is every file, for a deliberate normalization pass or a continuous-integration
// check that wants the whole corpus.
//
// It is a flag rather than a default, and it is allowed to cost five minutes, because someone who
// asks for the whole tree has said what they want. The default costing five minutes is what gets a
// gate bypassed.
func wholeTreeScope() formatScope {
	return formatScope{Everything: true, Description: "every file (--format-all)"}
}

// changedFilesScope resolves the files git reports as changed in the working tree.
//
// Working-tree modifications plus staged changes plus untracked files, which is the set a person
// means by "what I am working on" and the set `s p` formats today. A base-branch diff is a different
// question and would need its own flag rather than silently redefining this one.
//
// Git failing is not a silent fallback to the whole tree. Formatting 3,407 files because a
// subprocess failed would turn a five-minute surprise into the default, so the failure is returned
// and the caller reports the phase as skipped with the reason.
func changedFilesScope(workingDirectory string) (formatScope, error) {
	names, err := gitChangedFiles(workingDirectory)
	if err != nil {
		return formatScope{}, err
	}

	absolute := make([]string, 0, len(names))
	for _, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDirectory, name)
		}
		absolute = append(absolute, filepath.Clean(path))
	}
	sort.Strings(absolute)

	index := make(map[string]struct{}, len(absolute))
	for _, path := range absolute {
		index[path] = struct{}{}
	}

	return formatScope{
		FileNames:   absolute,
		index:       index,
		Description: fmt.Sprintf("%d changed files (working tree, staged, and untracked)", len(absolute)),
	}, nil
}

// gitChangedFiles asks git what has changed, in one call.
//
// `git status --porcelain` covers all three states at once: modified in the working tree, staged,
// and untracked. Three separate commands would be three chances for the sets to disagree if a file
// changed between them, which on a tree with nine writers is not hypothetical.
func gitChangedFiles(workingDirectory string) ([]string, error) {
	command := exec.Command("git", "status", "--porcelain=v1", "--no-renames")
	command.Dir = workingDirectory

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("asking git what changed: %w", err)
	}

	names := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		// Porcelain v1 is two status characters, a space, then the path. A line shorter than that is
		// not a record.
		if len(line) < 4 {
			continue
		}
		// A deleted file has nothing to format, and reaching for it would be a read of a path that is
		// gone.
		if line[0] == 'D' || line[1] == 'D' {
			continue
		}
		names = append(names, unquoteGitPath(strings.TrimSpace(line[3:])))
	}
	return names, nil
}

// unquoteGitPath undoes the quoting git applies to paths with unusual characters.
//
// Git wraps such a path in double quotes and escapes inside it. Left alone, the quotes become part
// of the path and the file silently drops out of scope, which is the shape of a file that is never
// formatted and never reported as skipped.
func unquoteGitPath(path string) string {
	if len(path) < 2 || path[0] != '"' || path[len(path)-1] != '"' {
		return path
	}
	inner := path[1 : len(path)-1]
	inner = strings.ReplaceAll(inner, `\"`, `"`)
	inner = strings.ReplaceAll(inner, `\\`, `\`)
	return inner
}

// scopedTransform narrows a transform to the files a scope admits.
//
// Files outside the scope are skipped with the scope's own description as the reason, so they are
// counted and named rather than quietly passed over. That is the difference between "formatted 12
// files" with no denominator and a line that says what was in scope and why, which is the same
// discipline the skip channel already carries for unhandled file types.
func scopedTransform(inner fix.Transform, scope formatScope) fix.Transform {
	if inner == nil {
		return nil
	}
	if scope.Everything {
		return inner
	}

	return func(fileName string, text string) (string, error) {
		if !scope.includes(fileName) {
			return "", fmt.Errorf("%w: outside the format scope, %s", fix.ErrSkipped, scope.Description)
		}
		return inner(fileName, text)
	}
}
