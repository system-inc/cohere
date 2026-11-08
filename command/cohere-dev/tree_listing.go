package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/git_tree"
	"golang.org/x/mod/modfile"
)

// What makes a land main's is everything it links, not only command/cohere-dev (#ehjasc6). #j3bfnyt changed
// internal/go_engine/project, which the gate calls to read the modules it gates, and left command/cohere-dev as it
// was. So every branch from before it compared equal, ran its own land, and gated main's merged tree with code
// that predated the change: an hour of landings refused on 2026-11-07 for a module main's land accepts.
//
// The inputs are read from git, at the worktree's `HEAD` and at main, without the go command: the imports of
// every package reached from command/cohere-dev in our own modules, every file of each counted whatever its build
// tags, and the module and workspace files that pin the rest. A land whose inputs differ in any of them is not
// main's. One that can't be read is not main's either, since main's land is the one that decides.

// landModuleFiles pin what land links from outside our own modules, and how the workspace resolves.
var landModuleFiles = []string{"go.mod", "go.sum", "go.work", "go.work.sum"}

// landInputsDiffer names the first of land's inputs whose content differs between the worktree's `HEAD` and main,
// or is empty when every one is the same.
func landInputsDiffer(worktree string) string {
	heads, headError := readTreeListing(worktree, "HEAD")
	mains, mainError := readTreeListing(worktree, "main")
	if headError != nil || mainError != nil {
		return "the tree land is built from, which could not be read"
	}
	for _, file := range landModuleFiles {
		if heads.object(file) != mains.object(file) {
			return file
		}
	}
	reached := map[string]bool{}
	for _, listing := range []*treeListing{heads, mains} {
		directories, closureError := listing.importClosure(worktree, landSource)
		if closureError != nil {
			return "the packages land links, which could not be read: " + closureError.Error()
		}
		for _, directory := range directories {
			reached[directory] = true
		}
	}
	sorted := make([]string, 0, len(reached))
	for directory := range reached {
		sorted = append(sorted, directory)
	}
	sort.Strings(sorted)
	for _, directory := range sorted {
		if heads.object(directory) != mains.object(directory) {
			return directory
		}
	}
	return ""
}

// treeListing is one commit's tree: every path's object, a directory's tree, a file's blob and a submodule's
// commit, as `git ls-tree -r -t` reads them.
type treeListing struct {
	commit  string
	objects map[string]string
}

func readTreeListing(worktree string, revision string) (*treeListing, error) {
	commit, gitOutputError := gitOutput(worktree, "rev-parse", "--verify", "--quiet", revision+"^{commit}")
	if gitOutputError != nil {
		return nil, gitOutputError
	}
	listing, listError := gitOutput(worktree, "ls-tree", "-r", "-t", "-z", commit)
	if listError != nil {
		return nil, listError
	}
	entries, parseError := git_tree.ParseTreeListing([]byte(listing))
	if parseError != nil {
		return nil, parseError
	}
	objects := map[string]string{"": commit}
	for _, entry := range entries {
		objects[entry.Path] = entry.Object
	}
	return &treeListing{commit: commit, objects: objects}, nil
}

// object is a path's object, or, for a path inside a submodule, the submodule's commit, which is what decides
// its content; empty when the path is not in the tree at all.
func (listing *treeListing) object(entryPath string) string {
	for candidate := entryPath; candidate != "." && candidate != "/" && candidate != ""; candidate = path.Dir(
		candidate,
	) {
		if object, found := listing.objects[candidate]; found {
			if candidate == entryPath {
				return object
			}
			return "inside " + candidate + " at " + object
		}
	}
	return ""
}

// modules maps each of our modules' paths to its directory: the root's, and every one go.work uses.
func (listing *treeListing) modules(worktree string) (map[string]string, error) {
	directories := []string{"."}
	if object := listing.objects["go.work"]; object != "" {
		contents, readError := readBlobs(worktree, []string{object})
		if readError != nil {
			return nil, readError
		}
		work, parseError := modfile.ParseWork("go.work", contents[object], nil)
		if parseError != nil {
			return nil, fmt.Errorf("reading go.work: %w", parseError)
		}
		for _, use := range work.Use {
			directories = append(directories, path.Clean(use.Path))
		}
	}
	files := map[string]string{}
	objects := []string{}
	for _, directory := range directories {
		object := listing.objects[path.Join(directory, "go.mod")]
		if object != "" {
			files[directory] = object
			objects = append(objects, object)
		}
	}
	contents, readError := readBlobs(worktree, objects)
	if readError != nil {
		return nil, readError
	}
	modules := map[string]string{}
	for directory, object := range files {
		if modulePath := modfile.ModulePath(contents[object]); modulePath != "" {
			modules[modulePath] = directory
		}
	}
	return modules, nil
}

// importClosure is every directory of our own modules a package's imports reach from start, start included,
// reading each package's Go files that are not tests, whatever their build tags, so the set holds every file any
// build of land could link. A file that does not parse imports nothing; its directory is still compared whole.
func (listing *treeListing) importClosure(worktree string, start string) ([]string, error) {
	modules, modulesError := listing.modules(worktree)
	if modulesError != nil {
		return nil, modulesError
	}
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		directory := queue[0]
		queue = queue[1:]
		objects := []string{}
		prefix := directory + "/"
		for entryPath, object := range listing.objects {
			name, inDirectory := strings.CutPrefix(entryPath, prefix)
			if inDirectory && !strings.Contains(name, "/") && strings.HasSuffix(name, ".go") &&
				!strings.HasSuffix(name, "_test.go") {
				objects = append(objects, object)
			}
		}
		contents, readError := readBlobs(worktree, objects)
		if readError != nil {
			return nil, readError
		}
		for _, object := range objects {
			file, parseError := parser.ParseFile(token.NewFileSet(), "", contents[object], parser.ImportsOnly)
			if parseError != nil {
				continue
			}
			for _, imported := range file.Imports {
				importPath, unquoteError := strconv.Unquote(imported.Path.Value)
				if unquoteError != nil {
					continue
				}
				reached, ours := ourDirectory(modules, importPath)
				if ours && !seen[reached] {
					seen[reached] = true
					queue = append(queue, reached)
				}
			}
		}
	}
	directories := make([]string, 0, len(seen))
	for directory := range seen {
		directories = append(directories, directory)
	}
	return directories, nil
}

// ourDirectory is the directory of an import path in one of our modules, by the longest module path that holds it.
func ourDirectory(modules map[string]string, importPath string) (string, bool) {
	longest := ""
	for modulePath := range modules {
		if (importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/")) &&
			len(modulePath) > len(longest) {
			longest = modulePath
		}
	}
	if longest == "" {
		return "", false
	}
	return path.Join(modules[longest], strings.TrimPrefix(importPath, longest)), true
}

// readBlobs reads blobs by object name through one `git cat-file --batch`.
func readBlobs(worktree string, objects []string) (map[string][]byte, error) {
	contents := map[string][]byte{}
	if len(objects) == 0 {
		return contents, nil
	}
	command := exec.Command("git", "cat-file", "--batch")
	command.Dir = worktree
	command.Stdin = strings.NewReader(strings.Join(objects, "\n") + "\n")
	output, outputError := command.Output()
	if outputError != nil {
		return nil, fmt.Errorf("git cat-file --batch: %w", outputError)
	}
	reader := bufio.NewReader(bytes.NewReader(output))
	for range objects {
		header, readError := reader.ReadString('\n')
		if readError != nil {
			return nil, fmt.Errorf("reading git cat-file's output: %w", readError)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 {
			return nil, fmt.Errorf("git cat-file could not read %q", strings.TrimSpace(header))
		}
		size, sizeError := strconv.Atoi(fields[2])
		if sizeError != nil {
			return nil, fmt.Errorf("reading git cat-file's size in %q: %w", strings.TrimSpace(header), sizeError)
		}
		body := make([]byte, size+1)
		if _, readError := io.ReadFull(reader, body); readError != nil {
			return nil, fmt.Errorf("reading %s from git cat-file: %w", fields[0], readError)
		}
		contents[fields[0]] = body[:size]
	}
	return contents, nil
}
