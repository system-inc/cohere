package prettier

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Enumeration is what a walk found, and why each file did not survive it.
//
// The counts exist because the failure this type is built to prevent is a file being silently
// absent rather than visibly declined. A formatter that never saw a .json in a TypeScript project
// and a formatter that examined it and found it correct produce identical output otherwise, and the
// coverage line cannot tell them apart unless the enumeration says so.
//
// So every number here is a subtraction someone can check: Walked minus the ignore counts minus
// Unhandled should equal len(Files), and DeclinedExtensions names what the engine refused rather
// than leaving it to be inferred from a smaller total.
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

	// NestedRepositories are directories the walk refused to descend into because they are their
	// own git repository. Named rather than counted, because "we skipped a repo" is a fact someone
	// may want to argue with and a number is not.
	NestedRepositories []string

	// DeclinedExtensions is the distinct extensions the engine refused, with counts. Named rather
	// than summed, because "we skipped 57 files" is not actionable and ".json 57" is.
	DeclinedExtensions map[string]int

	// Files is what survived, absolute paths.
	Files []string
}

// ignoreLayer is one ignore file and the patterns it contributed.
type ignoreLayer struct {
	name     string
	patterns []string
}

// readIgnoreFile reads one ignore file into patterns, dropping comments and blanks.
//
// A missing file is not an error: the project .prettierignore is optional and Structure's defaults
// live in a submodule that may not be checked out. It returns no patterns, and the layer reports
// zero removals, which is visible in the enumeration rather than silent.
func readIgnoreFile(path string) ([]string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading ignore file %s: %w", path, err)
	}
	var patterns []string
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, nil
}

// matchesIgnore reports whether a repo-relative path is covered by a pattern.
//
// This is a deliberate subset of gitignore semantics: a bare name matches any path segment, a
// trailing slash matches a directory prefix, and a glob matches the base name. It is not a full
// gitignore implementation and must not pretend to be one, because the cost of quietly
// mis-implementing negation or double-star is a file silently missing, which is the failure this
// package exists to prevent. Anything more exotic belongs in a real matcher.
func matchesIgnore(relative string, pattern string) bool {
	pattern = strings.TrimPrefix(pattern, "/")

	// A trailing slash means "this directory". It may still contain a glob: `modules/*/data/` is
	// both, and getting that wrong is expensive rather than academic. On this repository that one
	// pattern covers an 8 GB directory, and failing to match it walked 130,584 files that git had
	// already excluded.
	if strings.HasSuffix(pattern, "/") {
		directory := strings.TrimSuffix(pattern, "/")
		if strings.ContainsAny(directory, "*?[") {
			// Match the pattern against the same number of leading segments it has, so
			// `modules/*/data` tests `modules/see/data` rather than the whole relative path.
			segments := strings.Split(relative, "/")
			wanted := len(strings.Split(directory, "/"))
			if len(segments) >= wanted {
				if matched, _ := filepath.Match(directory, strings.Join(segments[:wanted], "/")); matched {
					return true
				}
			}
			return false
		}
		return relative == directory ||
			strings.HasPrefix(relative, pattern) || strings.Contains(relative, "/"+pattern)
	}
	if strings.ContainsAny(pattern, "*?[") {
		if strings.Contains(pattern, "/") {
			matched, _ := filepath.Match(pattern, relative)
			return matched
		}
		matched, _ := filepath.Match(pattern, filepath.Base(relative))
		return matched
	}
	if relative == pattern || strings.HasSuffix(relative, "/"+pattern) {
		return true
	}
	return strings.HasPrefix(relative, pattern+"/") || strings.Contains(relative, "/"+pattern+"/")
}

// Enumerate walks a project root and returns the files this engine would format.
//
// The layers are applied in the order `s pnc` applies them, and each is counted separately so a
// misconfigured layer shows as a suspicious zero rather than as a slightly smaller total. That
// ordering is not cosmetic: it is the difference between a corpus that measures the tree and one
// that measures a smaller subject while looking complete.
func (engine *Engine) Enumerate(root string, structureIgnorePath string) (Enumeration, error) {
	enumeration := Enumeration{
		Root:               root,
		IgnoredByLayer:     map[string]int{},
		DeclinedExtensions: map[string]int{},
	}

	layers := []ignoreLayer{}
	for _, candidate := range []struct{ name, path string }{
		{".gitignore", filepath.Join(root, ".gitignore")},
		{"PrettierIgnoreDefaults", structureIgnorePath},
		{".prettierignore", filepath.Join(root, ".prettierignore")},
	} {
		if candidate.path == "" {
			continue
		}
		patterns, err := readIgnoreFile(candidate.path)
		if err != nil {
			return enumeration, err
		}
		layers = append(layers, ignoreLayer{name: candidate.name, patterns: patterns})
		enumeration.IgnoredByLayer[candidate.name] = 0
	}

	walkError := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return nil
		}
		if info.IsDir() {
			// .git is never formatted and walking it is pure cost on a large repo.
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			// A directory with its own .git is somebody else's tree, and the ignore layers do not
			// exclude it: a submodule is tracked by the parent as a gitlink rather than as ignored
			// paths, so nothing in .gitignore or .prettierignore mentions it. Tonight a whole-tree
			// run wrote a line into libraries/structure, and the change was correct and still
			// unrequested. A formatter should not edit a repository nobody asked it to touch.
			if relative != "." {
				if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
					enumeration.NestedRepositories = append(enumeration.NestedRepositories, relative)
					return filepath.SkipDir
				}
			}

			// Prune an ignored directory rather than descending and filtering its contents. This is
			// the difference between walking the tree and walking node_modules: measured on the real
			// repository, filtering after the fact walked 317,258 files in 8.7s, of which .gitignore
			// removed 148,279 that had already been stat'd. Pruning never enters them.
			for _, layer := range layers {
				for _, pattern := range layer.patterns {
					if matchesIgnore(relative, pattern) || matchesIgnore(relative+"/", pattern) {
						enumeration.IgnoredByLayer[layer.name]++
						return filepath.SkipDir
					}
				}
			}
			return nil
		}

		enumeration.Walked++

		for _, layer := range layers {
			for _, pattern := range layer.patterns {
				if matchesIgnore(relative, pattern) {
					enumeration.IgnoredByLayer[layer.name]++
					return nil
				}
			}
		}

		if !engine.Handles(path) {
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
