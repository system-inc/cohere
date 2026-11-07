package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/streams"
)

// A landing whose merge with main conflicts only in generated files is resolved by regenerating them (#y84h1te).
// On 2026-11-07 a unit reformatting 1,083 data files reached the head of the land line again and again to find
// main had changed one of them, cli.json or sets.json among them, and every conflict sent it back to the end of a
// six-deep line to be merged by hand. Every file under docs/data is one the docs data generator writes whole
// (TestEveryFileUnderDocsDataIsGenerated), so a conflict there has one right answer: what the generator writes
// from the merged sources. Land takes main's side of each conflicted file, reruns the generator in a scratch
// worktree, requires a second run to change nothing, and gates that tree as it gates any other. A conflict in any
// other file still refuses, naming it, since only a person can say what a hand-written merge means.

// generatedDirectory is the directory whose every file the docs data generator writes.
const generatedDirectory = "docs/data/"

// isGeneratedPath reports whether a path is one the docs data generator writes.
func isGeneratedPath(path string) bool {
	return strings.HasPrefix(path, generatedDirectory)
}

// resolveGeneratedConflict resolves a conflict confined to generated files, returning the resolved tree and the
// sentence the landing commit carries about it, or prints why it can't and returns a nonzero exit code.
func resolveGeneratedConflict(worktree string, head string, conflict *mergeConflict) (string, string, int) {
	var handWritten []string
	for _, path := range conflict.Paths {
		if !isGeneratedPath(path) {
			handWritten = append(handWritten, path)
		}
	}
	if len(handWritten) > 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", conflict)
		fmt.Fprintf(streams.Errors, "cohere-dev: land resolves a conflict only when every file in it is generated, "+
			"under %s, and these are written by hand: %s\n", generatedDirectory, strings.Join(handWritten, ", "))
		return "", "", 1
	}
	if _, statError := os.Stat(filepath.Join(worktree, docsDataGenerator)); statError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\ncohere-dev: there is no %s here to regenerate them with\n",
			conflict, docsDataGenerator)
		return "", "", 1
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: main at %s conflicts with this work only in %s, which the docs "+
		"data generator writes, so land regenerates them: %s\n", shortCommit(conflict.Main), generatedDirectory,
		strings.Join(conflict.Paths, ", "))

	pending, commitTreeError := commitTree(worktree, conflict.Tree, "Being resolved by cohere-dev land.",
		conflict.Main, head)
	if commitTreeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", commitTreeError)
		return "", "", 1
	}
	scratch, temporaryDirectoryError := os.MkdirTemp("", "cohere-dev-resolve-")
	if temporaryDirectoryError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: making a place to resolve the conflict: %v\n", temporaryDirectoryError)
		return "", "", 1
	}
	resolving := filepath.Join(scratch, "tree")
	if output, gitCombinedError := gitCombined(worktree, "worktree", "add", "--quiet", "--detach", resolving,
		pending); gitCombinedError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: making a worktree to resolve the conflict in: %v\n%s\n",
			gitCombinedError, output)
		return "", "", 1
	}
	defer removeScratchWorktree(worktree, scratch, resolving)
	// Each submodule linked to the checkout the worktree being landed uses, as cohere-dev worktree links them.
	if linkSubmodulesError := linkSubmodules(worktree, resolving); linkSubmodulesError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", linkSubmodulesError)
		return "", "", 1
	}
	for _, path := range conflict.Paths {
		if takeMainsSideError := takeMainsSide(worktree, resolving, conflict.Main, path); takeMainsSideError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", takeMainsSideError)
			return "", "", 1
		}
	}

	var trees []string
	code := withSteppedToken("resolving a docs data conflict in "+resolving, func(step func(string) []string) int {
		for run := 1; run <= 2; run++ {
			generate := exec.Command("go", "run", docsDataGenerator, "-capture")
			generate.Dir, generate.Env = resolving, gateEnvironment(step("regenerating the docs data"))
			generate.Stdout, generate.Stderr = os.Stdout, os.Stderr
			streams.Show(streams.Output)
			streams.Show(streams.Errors)
			if runError := generate.Run(); runError != nil {
				fmt.Fprintln(streams.Errors,
					"cohere-dev: the docs data generator failed on the merged tree, so nothing landed")
				return exitCodeOf(runError)
			}
			tree, stagedTreeError := stagedGeneratedTree(resolving)
			if stagedTreeError != nil {
				fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", stagedTreeError)
				return 1
			}
			trees = append(trees, tree)
		}
		return 0
	})
	if code != 0 {
		return "", "", code
	}
	if trees[0] != trees[1] {
		fmt.Fprintln(streams.Errors, "cohere-dev: the docs data generator did not reach a fixed point on the merged "+
			"tree, a second run changing what the first wrote, so nothing landed: merge main by hand")
		return "", "", 1
	}
	resolution := fmt.Sprintf("Its conflicts with main were all in %s, which the docs data generator writes, so "+
		"land took main's side of %s, regenerated the docs data from the merged sources, and a second run changed "+
		"nothing: %s.", generatedDirectory, countedFiles(len(conflict.Paths)), strings.Join(conflict.Paths, ", "))
	return trees[1], resolution, 0
}

func countedFiles(count int) string {
	if count == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", count)
}

// takeMainsSide writes main's version of path into the resolving tree, or removes it there when main has none.
func takeMainsSide(worktree string, resolving string, mainCommit string, path string) error {
	destination := filepath.Join(resolving, filepath.FromSlash(path))
	contents, gitOutputError := exec.Command("git", "-C", worktree, "show", mainCommit+":"+path).Output()
	if gitOutputError != nil {
		var exited *exec.ExitError
		if errors.As(gitOutputError, &exited) {
			if removeError := os.Remove(destination); removeError != nil && !os.IsNotExist(removeError) {
				return fmt.Errorf("removing %s, which main does not have: %w", path, removeError)
			}
			return nil
		}
		return fmt.Errorf("reading main's %s: %w", path, gitOutputError)
	}
	if createDirectoryError := os.MkdirAll(filepath.Dir(destination), 0o755); createDirectoryError != nil {
		return createDirectoryError
	}
	return os.WriteFile(destination, contents, 0o644)
}

// stagedGeneratedTree stages the generated directory in the resolving tree and returns the tree its index now
// writes. Only that directory is staged, so a submodule linked in on disk is never recorded as a symlink.
func stagedGeneratedTree(resolving string) (string, error) {
	directory := strings.TrimSuffix(generatedDirectory, "/")
	if output, gitCombinedError := gitCombined(resolving, "add", "--all", "--", directory); gitCombinedError != nil {
		return "", fmt.Errorf("staging the regenerated docs data: %w\n%s", gitCombinedError, output)
	}
	tree, gitOutputError := gitOutput(resolving, "write-tree")
	if gitOutputError != nil {
		return "", fmt.Errorf("writing the regenerated tree: %w", gitOutputError)
	}
	return tree, nil
}

// removeScratchWorktree removes the worktree a resolution ran in and the directory holding it, each submodule link
// first, so nothing it pointed at is touched.
func removeScratchWorktree(worktree string, scratch string, resolving string) {
	if links, gitlinksOfError := project.GitlinksOf(worktree); gitlinksOfError == nil {
		for _, link := range links {
			// Ignored: a link already gone is the state this wants.
			_ = os.Remove(filepath.Join(resolving, link.Path))
		}
	}
	output, gitCombinedError := gitCombined(worktree, "worktree", "remove", "--force", resolving)
	if gitCombinedError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: removing the worktree the conflict was resolved in, %s: %v\n%s\n",
			resolving, gitCombinedError, output)
		return
	}
	// Ignored: an empty directory left behind costs nothing, and the landing is already decided.
	_ = os.Remove(scratch)
}
