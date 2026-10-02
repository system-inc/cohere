// Package formatfiles decides which files a tree offers the formatter: the walk, its three ignore layers
// in the order `s pnc` applies them, and an account of every file it did not offer.
package formatfiles

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

	// MissingLayers names each ignore file the caller pointed at that was not there, by path. A layer
	// that cannot be read removes nothing, and a zero reads like a layer with nothing to remove: Structure's
	// defaults moved in August and every walk since offered pnpm-lock.yaml with no word said.
	MissingLayers []string

	// Files is what survived, absolute paths.
	Files []string
}

// StructureIgnorePath is where a Structure-using project keeps the ignore defaults `s pnc` applies,
// the second of the three layers Enumerate reads. One home, because six copies of the old path kept
// pointing where the file used to be.
func StructureIgnorePath(root string) string {
	return filepath.Join(root, "libraries", "structure", "code-quality", "prettier", "PrettierIgnoreDefaults")
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

// IgnoringLine names the first pattern in an ignore file that covers a path, given relative to the
// file's directory, with the pattern's line number. It answers 0 when no pattern does or the file does
// not exist.
//
// It applies the walk's own subset of gitignore semantics, the file itself and every directory above
// it, so it says what the walk would skip. That is not always what git would say: a negation is read
// as a pattern of its own rather than as an exception.
func IgnoringLine(ignoreFile string, relative string) (int, string, error) {
	contents, err := os.ReadFile(ignoreFile)
	if os.IsNotExist(err) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("reading ignore file %s: %w", ignoreFile, err)
	}
	relative = filepath.ToSlash(relative)
	for index, line := range strings.Split(string(contents), "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		if matchesIgnore(relative, pattern) {
			return index + 1, pattern, nil
		}
		for directory := pathDirectory(relative); directory != ""; directory = pathDirectory(directory) {
			if matchesIgnore(directory, pattern) || matchesIgnore(directory+"/", pattern) {
				return index + 1, pattern, nil
			}
		}
	}
	return 0, "", nil
}

// pathDirectory is a slash path's parent, or "" at the top.
func pathDirectory(path string) string {
	slash := strings.LastIndexByte(path, '/')
	if slash < 0 {
		return ""
	}
	return path[:slash]
}

// Enumerate walks a project root and returns the files handles accepts.
//
// The walk is a function of the ignore layers and a file-type predicate, not of any engine, so the
// native formatter enumerates without building a goja runtime it would never run.
//
// The layers are applied in the order `s pnc` applies them, and each is counted separately so a
// misconfigured layer shows as a suspicious zero rather than as a slightly smaller total. That
// ordering is not cosmetic: it is the difference between a corpus that measures the tree and one
// that measures a smaller subject while looking complete.
func Enumerate(root string, structureIgnorePath string, handles func(fileName string) bool) (Enumeration, error) {
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
		// The repository's own ignore files are optional, so only the layer a caller named can be
		// missing: naming it says the project has one.
		if candidate.name == "PrettierIgnoreDefaults" {
			if _, statError := os.Stat(candidate.path); os.IsNotExist(statError) {
				enumeration.MissingLayers = append(enumeration.MissingLayers, candidate.path)
			}
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
			if relative != "." && HasOwnRepository(path) {
				enumeration.NestedRepositories = append(enumeration.NestedRepositories, relative)
				return filepath.SkipDir
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
