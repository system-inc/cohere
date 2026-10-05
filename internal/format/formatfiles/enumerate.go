// Package formatfiles decides which files a tree offers the formatter: the walk, its ignore layers
// (git's ignore files, the house list, the project's ignorePatterns), and an account of every file it did
// not offer.
package formatfiles

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/format/formatoptions"
	"github.com/system-inc/cohere/internal/gitignore"
	"github.com/system-inc/cohere/internal/lint/configuration"
)

// Enumeration is what a walk found, and why each file did not survive it.
//
// The counts exist because the failure this type is built to prevent is a file being silently
// absent rather than visibly declined. A formatter that never saw a .json in a TypeScript project
// and a formatter that examined it and found it correct produce identical output otherwise, and the
// coverage line cannot tell them apart unless the enumeration says so.
//
// So every number here is a subtraction someone can check: Walked minus the ignore counts minus
// Unhandled minus SymbolicLinks should equal len(Files), and DeclinedExtensions names what the engine
// refused rather than leaving it to be inferred from a smaller total.
type Enumeration struct {
	// Root is the directory that was walked. A count means nothing without the root it was taken
	// against, which is how a corpus silently measures half a tree.
	Root string

	// Walked is every file seen before any filter.
	Walked int

	// IgnoredByLayer is how many files each ignore layer removed, in the order they were applied.
	IgnoredByLayer map[string]int

	// Unhandled is how many surviving files the engine does not format.
	Unhandled int

	// SymbolicLinks is how many surviving entries were symbolic links, which the walk never follows or
	// reads, as Prettier 3 skips them when it expands a directory and as git stores a link rather than
	// its target. A link to a directory looked like a file named for it: pnpm's node_modules/zone.js was
	// offered to the formatter as a .js file and the run failed reading a directory.
	SymbolicLinks int

	// NestedRepositories are directories the walk refused to descend into because they are their
	// own git repository. Named rather than counted, because "we skipped a repo" is a fact someone
	// may want to argue with and a number is not.
	NestedRepositories []string

	// DeclinedExtensions is the distinct extensions the engine refused, with counts. Named rather
	// than summed, because "we skipped 57 files" is not actionable and ".json 57" is.
	DeclinedExtensions map[string]int

	// Directories is every directory the walk entered, the root included, absolute paths: each was
	// read rather than pruned or skipped as a nested repository. A file added, removed or renamed in
	// one, or a `.git` or a config file appearing there, changes that directory's modification time,
	// which is how a cache replaying the walk knows the tree it walked is no longer the tree on disk.
	Directories []string

	// IgnoreFiles is every git ignore file the walk read or looked for, absolute paths, present or not:
	// the `.gitignore` of the root and of each directory it entered, and `.git/info/exclude` when `.git`
	// is a directory. An edit to one changes what the walk skips without moving any directory's
	// modification time, so a cache replaying the walk reads them too. A pruned directory's file is not
	// read, and not listed.
	IgnoreFiles []string

	// Files is what survived, absolute paths.
	Files []string
}

// ignoreLayer is one of the project's ignore lists after git's, and how its patterns are read.
type ignoreLayer struct {
	name string

	// lines, when set, is the house list: lines of an ignore file, read with git's syntax relative to the
	// walk root. Otherwise globs are lint's globs, relative to globsFrom: the `ignorePatterns` layer.
	lines     *gitignore.Patterns
	globs     []string
	globsFrom string
}

// covers reports whether the layer excludes a path, given relative to the walk root. A directory is
// covered when the layer prunes it whole.
func (layer ignoreLayer) covers(root string, relative string, directory bool) bool {
	if layer.lines != nil {
		ignored, _ := layer.lines.Ignored(relative, directory)
		return ignored
	}

	fromSettings, err := filepath.Rel(layer.globsFrom, filepath.Join(root, relative))
	if err != nil || fromSettings == ".." || strings.HasPrefix(fromSettings, ".."+string(filepath.Separator)) {
		return false
	}
	fromSettings = filepath.ToSlash(fromSettings)
	for _, pattern := range layer.globs {
		// A glob prunes a directory only when it takes everything below it, `name/**` or `**`: lint
		// matches files, and `*.code.js` matching a directory's name says nothing about its contents.
		if directory && pattern != "**" && !strings.HasSuffix(pattern, "/**") {
			continue
		}
		if configuration.Match(pattern, fromSettings) {
			return true
		}
	}
	return false
}

// The names the walk's layers are counted under. GitignoreLayer counts every git ignore source together:
// the `.gitignore` files from the root down and info/exclude.
const (
	GitignoreLayer      = ".gitignore"
	HouseIgnoreLayer    = "format.ignore"
	IgnorePatternsLayer = "ignorePatterns"
)

// Enumerate walks a project root and returns the files handles accepts.
//
// The walk is a function of the ignore layers and a file-type predicate, not of any engine, so the
// native formatter enumerates without building a goja runtime it would never run.
//
// The layers, in order: git's ignore rules, read as git reads them (every `.gitignore` from the root down,
// each scoped to its directory, and info/exclude; see internal/gitignore); the house list, the `ignore`
// key of the format block in the Nexus tier, read with the same syntax; and the `ignorePatterns` of the CohereSettings.json governing root, the one
// list lint and the format walk share, read as lint reads it. Each is counted separately so a
// misconfigured layer shows as a suspicious zero rather than as a slightly smaller total. That is the
// difference between a corpus that measures the tree and one that measures a smaller subject while
// looking complete.
//
// Every resolution carries a house list: our tiers' from the Nexus tier, an outsider's from its own
// block or else the house's, and zero config's from cohere:typescript (#bfxz13m). A `.prettierignore` in
// root is refused, since cohere no longer reads it and the walk would skip less than the project believes.
func Enumerate(root string, handles func(fileName string) bool) (Enumeration, error) {
	enumeration := Enumeration{
		Root:               root,
		IgnoredByLayer:     map[string]int{},
		DeclinedExtensions: map[string]int{},
	}

	resolution, err := formatoptions.Resolve(root)
	if err != nil {
		return enumeration, err
	}
	leftover := filepath.Join(root, ".prettierignore")
	if _, statError := os.Lstat(leftover); statError == nil {
		return enumeration, fmt.Errorf("%s: %w; delete it, since the format block's \"ignore\" in the Nexus tier (%s) and the ignorePatterns of CohereSettings.json say what the walk skips",
			leftover, formatoptions.ErrPrettierConfigRemains, formatoptions.NexusTierFileName)
	}

	matcher, err := gitignore.New(root)
	if err != nil {
		return enumeration, err
	}
	enumeration.IgnoreFiles = append(enumeration.IgnoreFiles, filepath.Join(root, gitignore.IgnoreFileName))
	if information, statError := os.Stat(filepath.Join(root, ".git")); statError == nil && information.IsDir() {
		enumeration.IgnoreFiles = append(enumeration.IgnoreFiles, filepath.Join(root, filepath.FromSlash(gitignore.ExcludeFile)))
	}
	enumeration.IgnoredByLayer[GitignoreLayer] = 0
	var layers []ignoreLayer
	if resolution.HouseIgnoreDeclared {
		house, err := gitignore.CompilePatterns(resolution.HouseIgnore, HouseIgnoreLayer)
		if err != nil {
			return enumeration, err
		}
		layers = append(layers, ignoreLayer{name: HouseIgnoreLayer, lines: house})
	}
	if resolution.Source != "" && !repositoryBoundaryBetween(root, filepath.Dir(resolution.Source)) {
		layers = append(layers, ignoreLayer{
			name:      IgnorePatternsLayer,
			globs:     resolution.IgnorePatterns,
			globsFrom: filepath.Dir(resolution.Source),
		})
	}
	for _, layer := range layers {
		enumeration.IgnoredByLayer[layer.name] = 0
	}

	// The matcher for each directory entered, by its path relative to root: an entry is asked about
	// through its directory's matcher, which holds every ignore file from the root down to it.
	scopes := map[string]*gitignore.Matcher{"": matcher}

	walkError := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return nil
		}
		// Every ignore pattern is written with `/`, and the matchers read `/` as the separator. On
		// Windows Rel answers with `\`, which no nested pattern would match: `dist`, `.next/` and
		// `modules/*/data/` covered only the top level, and the walk formatted what they name.
		relative = filepath.ToSlash(relative)
		scope := scopes[parentOf(relative)]
		if info.IsDir() {
			// .git is never formatted and walking it is pure cost on a large repo.
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			// A directory with its own .git is somebody else's tree, and the ignore layers do not
			// exclude it: a submodule is tracked by the parent as a gitlink rather than as ignored
			// paths, so nothing in .gitignore or the ignore lists mentions it. Tonight a whole-tree
			// run wrote a line into libraries/structure, and the change was correct and still
			// unrequested. A formatter should not edit a repository nobody asked it to touch.
			if relative != "." && HasOwnRepository(path) {
				enumeration.NestedRepositories = append(enumeration.NestedRepositories, relative)
				return filepath.SkipDir
			}

			// Prune an ignored directory rather than descending and filtering its contents. This is
			// the difference between walking the tree and walking node_modules: measured on the real
			// repository, filtering after the fact walked 317,258 files in 8.7s, of which .gitignore
			// removed 148,279 that had already been stat'd. Pruning never enters them.
			if relative == "." {
				enumeration.Directories = append(enumeration.Directories, path)
				return nil
			}
			if ignored, _ := scope.Ignored(relative, true); ignored {
				enumeration.IgnoredByLayer[GitignoreLayer]++
				return filepath.SkipDir
			}
			for _, layer := range layers {
				if layer.covers(root, relative, true) {
					enumeration.IgnoredByLayer[layer.name]++
					return filepath.SkipDir
				}
			}
			entered, err := scope.Enter(relative)
			if err != nil {
				return err
			}
			scopes[relative] = entered
			enumeration.Directories = append(enumeration.Directories, path)
			enumeration.IgnoreFiles = append(enumeration.IgnoreFiles, filepath.Join(path, gitignore.IgnoreFileName))
			return nil
		}

		// A symbolic link is never a directory to filepath.Walk, which does not follow it, so a link to a
		// directory arrives here as a file. Whatever it points at, it is skipped, after the ignore layers
		// have had their say about its name.
		enumeration.Walked++

		if ignored, _ := scope.Ignored(relative, false); ignored {
			enumeration.IgnoredByLayer[GitignoreLayer]++
			return nil
		}
		for _, layer := range layers {
			if layer.covers(root, relative, false) {
				enumeration.IgnoredByLayer[layer.name]++
				return nil
			}
		}

		if info.Mode()&os.ModeSymlink != 0 {
			enumeration.SymbolicLinks++
			return nil
		}

		if !handles(path) {
			enumeration.Unhandled++
			extension := strings.ToLower(filepath.Ext(path))
			if extension == "" {
				extension = "(none)"
			}
			enumeration.DeclinedExtensions[extension]++
			return nil
		}

		enumeration.Files = append(enumeration.Files, path)
		return nil
	})
	if walkError != nil {
		return enumeration, fmt.Errorf("walking %s: %w", root, walkError)
	}

	return enumeration, nil
}

// repositoryBoundaryBetween reports whether a repository of its own begins at root or between root and
// the directory of the settings governing it. Then the settings belong to an outer repository: its
// options still apply, the house formats one way, but its ignorePatterns describe its own tree, and
// `projects/**` in ahra's list would otherwise leave a repository under projects/ offering nothing.
func repositoryBoundaryBetween(root string, settingsDirectory string) bool {
	root = filepath.Clean(root)
	settingsDirectory = filepath.Clean(settingsDirectory)
	for directory := root; directory != settingsDirectory; directory = filepath.Dir(directory) {
		if HasOwnRepository(directory) {
			return true
		}
		if parent := filepath.Dir(directory); parent == directory {
			return false
		}
	}
	return false
}

// NestedRepositoriesBelow finds every repository of its own below root, the outermost of each, relative
// to root: what a corpus harness measures beside root, each as its own corpus.
//
// It checks a directory for a repository before pruning it, as Enumerate does, and prunes only by git's
// ignore rules, read as Enumerate reads them. The project's own lists (the house list, ignorePatterns) are not read: they say
// what root formats, and a repository under a path root never formats is still a body of code a printer
// can be measured on. Reading them is how ahra's `projects/**` hid five repositories from the
// differential (#k6vebep).
func NestedRepositoriesBelow(root string) ([]string, error) {
	matcher, err := gitignore.New(root)
	if err != nil {
		return nil, err
	}
	scopes := map[string]*gitignore.Matcher{"": matcher}
	var nested []string
	walkError := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return nil
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil || relative == "." {
			return nil
		}
		if entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if HasOwnRepository(path) {
			nested = append(nested, relative)
			return filepath.SkipDir
		}
		relative = filepath.ToSlash(relative)
		scope := scopes[parentOf(relative)]
		if ignored, _ := scope.Ignored(relative, true); ignored {
			return filepath.SkipDir
		}
		entered, err := scope.Enter(relative)
		if err != nil {
			return err
		}
		scopes[relative] = entered
		return nil
	})
	if walkError != nil {
		return nil, fmt.Errorf("walking %s for nested repositories: %w", root, walkError)
	}
	return nested, nil
}

// HasOwnRepository reports whether a directory is the root of a git repository of its own: it holds a
// `.git`, a directory for a clone or a file for a submodule's gitlink.
//
// This is the boundary both writing phases share. The format walk refuses to descend past it, and the
// fix phase refuses to write past it on a whole-tree run, so neither edits a repository nobody asked
// it to touch.
func HasOwnRepository(directory string) bool {
	_, err := os.Stat(filepath.Join(directory, ".git"))
	return err == nil
}

// NestedRepositoryContaining returns the outermost repository of its own that holds fileName below
// root, relative to root, or "" when the file belongs to root's repository or lies outside root.
//
// The outermost rather than the nearest, because that is the directory the format walk skips: a file
// in libraries/structure/libraries/nexus is reported under libraries/structure, the repository a run
// in the parent would have had to name.
func NestedRepositoryContaining(root string, fileName string) string {
	root = filepath.Clean(root)
	nested := ""
	for directory := filepath.Dir(filepath.Clean(fileName)); ; directory = filepath.Dir(directory) {
		relative, err := filepath.Rel(root, directory)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nested
		}
		if HasOwnRepository(directory) {
			nested = relative
		}
		if parent := filepath.Dir(directory); parent == directory {
			return nested
		}
	}
}

// parentOf is a slash path's directory, "" for an entry of the root, as the matchers key directories.
func parentOf(relative string) string {
	parent := path.Dir(relative)
	if parent == "." {
		return ""
	}
	return parent
}
