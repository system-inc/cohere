package project

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/system-inc/cohere/internal/gitignore"
)

// IgnoredByGit is the reason a file git ignores is not ours.
const IgnoredByGit = "ignored by git"

// InRepositoryOfItsOwn is the reason a file below a directory that is a repository of its own, a submodule's
// checkout or a vendored clone with no go.mod, is not ours: go reads it, and no ignore file of ours governs it.
const InRepositoryOfItsOwn = "in a repository of its own"

// Census is every `.go` file a workspace's modules hold, as absolute paths in order, and which are ours.
type Census struct {
	// Root is the workspace's root, which every file is under.
	Root string

	// Modules are the workspace's modules, each with its path, read from its go.mod where the workspace left it
	// unnamed, so a file's import path can be told from its directory.
	Modules []Module

	// Files are every file go would see in an owned module's packages.
	Files []string

	// Ours are the Files nothing excludes.
	Ours []string

	// Excluded are the Files that are not ours, each with its reason.
	Excluded []Exclusion
}

// Exclusion is a file left out of what is ours, and why.
type Exclusion struct {
	File   string
	Reason string

	// Source is the rule that decided, as `path:line` with the path relative to the workspace's root, such as
	// `.gitignore:1`, so a reader can find the line that left the file out.
	Source string
}

// TakeCensus lists every `.go` file the workspace's modules hold, the way ./... sees packages: under each
// owned module's directory, never entering testdata, a directory whose name starts with `.` or `_`, one
// below a `vendor` directory, one the module's go.mod names in an `ignore` directive, or a directory with a
// go.mod of its own (an owned one is walked as itself, and an unowned one is another project), and never
// counting a file whose name starts with `.` or `_`, which go does not read either. A file below a directory
// that is a repository of its own with no go.mod, a submodule's checkout or a vendored clone, is one go reads,
// so it is counted and excluded as in a repository of its own (#hd5bsqh). A file git ignores is excluded, by
// the ignore files of the repository holding its real path, as git judges it, never through a link, and a file
// that repository's index tracks never is, as git never ignores a tracked file; a file in no repository is
// ignored by nothing. Generated files are counted as ours here: the format and lint phases decide about those.
func TakeCensus(workspace Workspace) (Census, error) {
	census := Census{Root: workspace.Root}
	for _, module := range workspace.Modules {
		if module.Path == "" {
			goModPath := filepath.Join(workspace.Root, filepath.FromSlash(module.Directory), "go.mod")
			goMod, goModError := os.ReadFile(goModPath)
			if goModError != nil {
				return Census{}, fmt.Errorf("reading the module at %s: %w", module.Directory, goModError)
			}
			module.Path = modfile.ModulePath(goMod)
		}
		census.Modules = append(census.Modules, module)
	}
	// An exclusion's source is named from the root's real path, as every ignore file is found at its own.
	resolvedRoot, rootError := filepath.EvalSymlinks(workspace.Root)
	if rootError != nil {
		return Census{}, fmt.Errorf("resolving the root %s: %w", workspace.Root, rootError)
	}
	// The matchers of each repository a module's real directory lies in, by that repository's root.
	repositories := map[string]*repositoryScopes{}

	for _, module := range workspace.Modules {
		moduleDirectory := filepath.Join(workspace.Root, filepath.FromSlash(module.Directory))
		directives, directivesError := readIgnoreDirectives(moduleDirectory)
		if directivesError != nil {
			return Census{}, directivesError
		}
		// A walk never follows a symbolic link, its starting path included, so a module a go.work uses through one is
		// walked at its real directory, and each file named through the link, as go names it (#hd5bsqh). Go follows a
		// link at a use and nowhere below it, so neither does the walk.
		walked, resolveError := filepath.EvalSymlinks(moduleDirectory)
		if resolveError != nil {
			return Census{}, fmt.Errorf("resolving the module at %s: %w", module.Directory, resolveError)
		}
		repositoryRoot := gitignore.RepositoryOf(walked)
		scopes := repositories[repositoryRoot]
		if repositoryRoot != "" && scopes == nil {
			rootMatcher, matcherError := gitignore.New(repositoryRoot)
			if matcherError != nil {
				return Census{}, fmt.Errorf("reading %s's ignore files: %w", repositoryRoot, matcherError)
			}
			scopes = &repositoryScopes{root: rootMatcher, entered: map[string]*gitignore.Matcher{}}
			repositories[repositoryRoot] = scopes
		}
		// The directories below the module that are repositories of their own, whose files are excluded unjudged:
		// the ignore files of ours never govern them.
		var ownRepositories []string
		inOwnRepository := func(walkedPath string) bool {
			return slices.ContainsFunc(ownRepositories, func(repository string) bool {
				return strings.HasPrefix(walkedPath, repository+string(filepath.Separator))
			})
		}
		walkError := filepath.WalkDir(walked, func(walkedPath string, entry fs.DirEntry, pathError error) error {
			if pathError != nil {
				return pathError
			}
			visited := moduleDirectory + strings.TrimPrefix(walkedPath, walked)
			if entry.IsDir() {
				if walkedPath != walked {
					moduleRelative, relativeError := filepath.Rel(walked, walkedPath)
					if relativeError != nil {
						return relativeError
					}
					switch roleOf(walkedPath, filepath.ToSlash(moduleRelative), directives) {
					case "NotPartOfModule":
						return filepath.SkipDir
					case "RepositoryOfItsOwn":
						ownRepositories = append(ownRepositories, walkedPath)
						return nil
					}
				}
				if scopes == nil || inOwnRepository(walkedPath) {
					return nil
				}
				return enterScope(scopes.root, scopes.entered, repositoryRoot, walkedPath)
			}
			if !goSees(entry.Name()) {
				return nil
			}
			census.Files = append(census.Files, visited)
			if inOwnRepository(walkedPath) {
				census.Excluded = append(census.Excluded, Exclusion{File: visited, Reason: InRepositoryOfItsOwn})
				return nil
			}
			if scopes != nil {
				relative, relativeError := repositoryRelative(repositoryRoot, walkedPath)
				if relativeError != nil {
					return relativeError
				}
				scope, entered := scopes.entered[parentOf(relative)]
				if !entered {
					// Invariant: WalkDir visits a directory before its files, and every visit enters it.
					panic(fmt.Sprintf("project: %s was reached before its directory was entered", visited))
				}
				if ignored, source := scope.Ignored(relative, false); ignored {
					decided, sourceError := sourceFromRoot(resolvedRoot, repositoryRoot, source)
					if sourceError != nil {
						return sourceError
					}
					census.Excluded = append(census.Excluded,
						Exclusion{File: visited, Reason: IgnoredByGit, Source: decided})
					return nil
				}
			}
			census.Ours = append(census.Ours, visited)
			return nil
		})
		if walkError != nil {
			return Census{}, fmt.Errorf("walking the module at %s: %w", moduleDirectory, walkError)
		}
	}
	slices.Sort(census.Files)
	slices.Sort(census.Ours)
	slices.SortFunc(census.Excluded, func(left Exclusion, right Exclusion) int {
		return cmp.Compare(left.File, right.File)
	})
	return census, nil
}

// repositoryScopes are one repository's matchers: its root's own, and for each directory the walk entered, by its
// path relative to the root, the one holding every ignore file from the root down to it, so a file is asked about
// through its directory's.
type repositoryScopes struct {
	root    *gitignore.Matcher
	entered map[string]*gitignore.Matcher
}

// directoryRole is what a directory below a module's own is to the module, as go sees it.
type directoryRole string

// roleOf is what a directory below a module's own is to it, as go sees it. Testdata, a directory whose name starts
// with `.` or `_`, one below a `vendor` directory, one an `ignore` directive names, and one with a go.mod of its own
// are NotPartOfModule, and the walk never enters them. A directory with a `.git` of its own and no go.mod, a
// checked-out submodule or a clone, is RepositoryOfItsOwn: go reads its packages as the module's, so its files are
// counted, and no ignore file of ours governs them. It is found by its `.git` rather than by the index's gitlinks,
// which only a go.work root reads. A submodule never checked out is an empty directory, with nothing to count. Any
// other directory is PartOfModule.
//
// A `vendor` directory's own files are a package ./... matches, which go keeps for historical reasons, and
// nothing below it is, so the walk stops one level in (go 1.27, cmd/go/internal/modload/search.go).
func roleOf(directory string, moduleRelative string, directives []string) directoryRole {
	name := path.Base(moduleRelative)
	if name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return "NotPartOfModule"
	}
	if path.Base(path.Dir(moduleRelative)) == "vendor" || ignoredByDirective(moduleRelative, directives) {
		return "NotPartOfModule"
	}
	if _, statError := os.Lstat(filepath.Join(directory, "go.mod")); statError == nil {
		return "NotPartOfModule"
	}
	if _, statError := os.Lstat(filepath.Join(directory, ".git")); statError == nil {
		return "RepositoryOfItsOwn"
	}
	return "PartOfModule"
}

// goSees reports whether go reads a file of a package's directory as Go source: a `.go` file whose name does not
// start with `_` or `.`, which go/build's matchFile drops before it even lists one as ignored.
func goSees(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasPrefix(name, "_") && !strings.HasPrefix(name, ".")
}

// readIgnoreDirectives is the paths a module's go.mod names in `ignore` directives, read leniently as go
// reads a go.mod's other directives for the module's path. A module directory with no go.mod ignores
// nothing.
func readIgnoreDirectives(moduleDirectory string) ([]string, error) {
	goModPath := filepath.Join(moduleDirectory, "go.mod")
	contents, readError := os.ReadFile(goModPath)
	if errors.Is(readError, fs.ErrNotExist) {
		return nil, nil
	}
	if readError != nil {
		return nil, fmt.Errorf("reading %s: %w", goModPath, readError)
	}
	goMod, parseError := modfile.ParseLax(goModPath, contents, nil)
	if parseError != nil {
		return nil, fmt.Errorf("parsing %s: %w", goModPath, parseError)
	}
	directives := make([]string, 0, len(goMod.Ignore))
	for _, ignore := range goMod.Ignore {
		directives = append(directives, ignore.Path)
	}
	return directives, nil
}

// ignoredByDirective reports whether a directory, relative to its module, is one an `ignore` directive
// names or is inside one, as go matches them (cmd/go/internal/search, IgnorePatterns): a path starting
// `./` is anchored at the module's root, and any other names that path at any depth.
func ignoredByDirective(moduleRelative string, directives []string) bool {
	directory := "/" + strings.Trim(moduleRelative, "/") + "/"
	for _, directive := range directives {
		directivePath, anchored := strings.CutPrefix(directive, "./")
		pattern := "/" + strings.Trim(directivePath, "/") + "/"
		if (anchored && strings.HasPrefix(directory, pattern)) || (!anchored && strings.Contains(directory, pattern)) {
			return true
		}
	}
	return false
}

// enterScope records the matcher for a directory the walk enters, from its parent's when the walk came
// through it, and from the repository root's for a module's own directory.
//
// Mutates: scopes gains the matcher for directory, by its repository-relative path, for the directories below it
func enterScope(
	rootMatcher *gitignore.Matcher, scopes map[string]*gitignore.Matcher, repositoryRoot string, directory string,
) error {
	relative, relativeError := repositoryRelative(repositoryRoot, directory)
	if relativeError != nil {
		return relativeError
	}
	from, parentEntered := scopes[parentOf(relative)]
	if !parentEntered || relative == "" {
		from = rootMatcher
	}
	entered, enterError := from.Enter(relative)
	if enterError != nil {
		return fmt.Errorf("reading the ignore files above %s: %w", directory, enterError)
	}
	scopes[relative] = entered
	return nil
}

// repositoryRelative is a path relative to the repository root, slash separated, "" for the root itself, as
// the matcher keys on.
func repositoryRelative(repositoryRoot string, entryPath string) (string, error) {
	relative, relativeError := filepath.Rel(repositoryRoot, entryPath)
	if relativeError != nil {
		return "", fmt.Errorf("placing %s in the repository at %s: %w", entryPath, repositoryRoot, relativeError)
	}
	if relative == "." {
		return "", nil
	}
	return filepath.ToSlash(relative), nil
}

// sourceFromRoot is the ignore rule that decided, as `path:line` with its file relative to the workspace's root
// rather than the repository's, which a root below the repository's makes `../.gitignore:1`.
func sourceFromRoot(workspaceRoot string, repositoryRoot string, source gitignore.Source) (string, error) {
	ignoreFile := filepath.FromSlash(source.File)
	// A worktree's shared info/exclude is named by its absolute path, as git names it (gitignore.ExcludeFileOf).
	if !filepath.IsAbs(ignoreFile) {
		ignoreFile = filepath.Join(repositoryRoot, ignoreFile)
	}
	relative, relativeError := filepath.Rel(workspaceRoot, ignoreFile)
	if relativeError != nil {
		return "", fmt.Errorf("placing %s from the root at %s: %w", ignoreFile, workspaceRoot, relativeError)
	}
	return fmt.Sprintf("%s:%d", filepath.ToSlash(relative), source.Line), nil
}

// parentOf is the directory holding a repository-relative path, "" at the root.
func parentOf(relative string) string {
	index := strings.LastIndexByte(relative, '/')
	if index < 0 {
		return ""
	}
	return relative[:index]
}
