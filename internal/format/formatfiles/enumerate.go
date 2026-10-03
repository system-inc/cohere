// Package formatfiles decides which files a tree offers the formatter: the walk, its ignore layers
// (`.gitignore`, the house list, the project's ignorePatterns), and an account of every file it did
// not offer.
package formatfiles

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/format/formatoptions"
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
// which the Nexus tier's house list replaces (see Enumerate for how it retires). One home, because six
// copies of the old path kept pointing where the file used to be.
func StructureIgnorePath(root string) string {
	return filepath.Join(root, "libraries", "structure", "code-quality", "prettier", "PrettierIgnoreDefaults")
}

// ignoreLayer is one ignore list and how its patterns are read.
type ignoreLayer struct {
	name     string
	patterns []string

	// globsFrom, when set, reads the patterns as lint's globs, relative to this directory: the
	// `ignorePatterns` layer. Otherwise they read as lines of an ignore file, relative to the walk root.
	globsFrom string
}

// covers reports the pattern in the layer that covers a path, given relative to the walk root. A
// directory is covered when the layer prunes it whole.
func (layer ignoreLayer) covers(root string, relative string, directory bool) (string, bool) {
	if layer.globsFrom == "" {
		for _, pattern := range layer.patterns {
			if matchesIgnore(relative, pattern) || (directory && matchesIgnore(relative+"/", pattern)) {
				return pattern, true
			}
		}
		return "", false
	}

	fromSettings, err := filepath.Rel(layer.globsFrom, filepath.Join(root, relative))
	if err != nil || fromSettings == ".." || strings.HasPrefix(fromSettings, ".."+string(filepath.Separator)) {
		return "", false
	}
	fromSettings = filepath.ToSlash(fromSettings)
	for _, pattern := range layer.patterns {
		// A glob prunes a directory only when it takes everything below it, `name/**` or `**`: lint
		// matches files, and `*.code.js` matching a directory's name says nothing about its contents.
		if directory && pattern != "**" && !strings.HasSuffix(pattern, "/**") {
			continue
		}
		if configuration.Match(pattern, fromSettings) {
			return pattern, true
		}
	}
	return "", false
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
//
// relative is slash-separated on every platform, and the globs are matched with path.Match, which reads
// `/` as the separator and `\` as an escape everywhere, as gitignore does. filepath.Match on Windows
// reads `\` as the separator instead, so `*` crossed `/` there.
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
				if matched, _ := path.Match(directory, strings.Join(segments[:wanted], "/")); matched {
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
			matched, _ := path.Match(pattern, relative)
			return matched
		}
		matched, _ := path.Match(pattern, path.Base(relative))
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

// The names the walk's layers are counted under.
const (
	HouseIgnoreLayer    = "format.ignore"
	IgnorePatternsLayer = "ignorePatterns"
)

// Enumerate walks a project root and returns the files handles accepts.
//
// The walk is a function of the ignore layers and a file-type predicate, not of any engine, so the
// native formatter enumerates without building a goja runtime it would never run.
//
// The layers, in order: the repository's `.gitignore`; the house list, the `ignore` key of the format
// block in the Nexus tier; and the `ignorePatterns` of the CohereSettings.json governing root, the one
// list lint and the format walk share, read as lint reads it. Each is counted separately so a
// misconfigured layer shows as a suspicious zero rather than as a slightly smaller total. That is the
// difference between a corpus that measures the tree and one that measures a smaller subject while
// looking complete.
//
// structureIgnorePath (Structure's PrettierIgnoreDefaults) and root's `.prettierignore` are the two
// files those lists replace. Until the Nexus tier declares the house list they are still read as
// layers, after `.gitignore`, so a repository pinned to an older Nexus keeps its walk. Once it is
// declared they are read only to compare: a file either would skip that the lists offer is refused,
// naming the file, the pattern and the path, so an old file cannot quietly keep a rule the lists
// dropped. Deleting them is #dv5ng7g.
func Enumerate(root string, structureIgnorePath string, handles func(fileName string) bool) (Enumeration, error) {
	enumeration := Enumeration{
		Root:               root,
		IgnoredByLayer:     map[string]int{},
		DeclinedExtensions: map[string]int{},
	}

	resolution, err := formatoptions.Resolve(root)
	if err != nil {
		return enumeration, err
	}

	gitignore, err := readIgnoreFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return enumeration, err
	}
	layers := []ignoreLayer{{name: ".gitignore", patterns: gitignore}}

	// The retiring files, read as layers or only compared.
	var retiring []ignoreLayer
	for _, candidate := range []struct{ name, path string }{
		{"PrettierIgnoreDefaults", structureIgnorePath},
		{".prettierignore", filepath.Join(root, ".prettierignore")},
	} {
		if candidate.path == "" {
			continue
		}
		// A project's .prettierignore is optional, so only the file a caller named can be missing:
		// naming it says the project has one. Once the house list is declared, its absence is the goal.
		if candidate.name == "PrettierIgnoreDefaults" && !resolution.HouseIgnoreDeclared {
			if _, statError := os.Stat(candidate.path); os.IsNotExist(statError) {
				enumeration.MissingLayers = append(enumeration.MissingLayers, candidate.path)
			}
		}
		patterns, err := readIgnoreFile(candidate.path)
		if err != nil {
			return enumeration, err
		}
		retiring = append(retiring, ignoreLayer{name: candidate.path, patterns: patterns})
		if !resolution.HouseIgnoreDeclared {
			layers = append(layers, ignoreLayer{name: candidate.name, patterns: patterns})
		}
	}
	if resolution.HouseIgnoreDeclared {
		layers = append(layers, ignoreLayer{name: HouseIgnoreLayer, patterns: resolution.HouseIgnore})
	} else {
		retiring = nil
	}
	if resolution.Source != "" {
		layers = append(layers, ignoreLayer{
			name:      IgnorePatternsLayer,
			patterns:  resolution.IgnorePatterns,
			globsFrom: filepath.Dir(resolution.Source),
		})
	}
	for _, layer := range layers {
		enumeration.IgnoredByLayer[layer.name] = 0
	}

	var disagreement error

	walkError := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return nil
		}
		// Every ignore pattern is written with `/`, and matchesIgnore reads `/` as the separator. On
		// Windows Rel answers with `\`, which no nested pattern would match: `dist`, `.next/` and
		// `modules/*/data/` covered only the top level, and the walk formatted what they name.
		relative = filepath.ToSlash(relative)
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
			if relative == "." {
				return nil
			}
			for _, layer := range layers {
				if _, covered := layer.covers(root, relative, true); covered {
					enumeration.IgnoredByLayer[layer.name]++
					return filepath.SkipDir
				}
			}
			return nil
		}

		enumeration.Walked++

		for _, layer := range layers {
			if _, covered := layer.covers(root, relative, false); covered {
				enumeration.IgnoredByLayer[layer.name]++
				return nil
			}
		}

		// The lists offer this file. A retiring file that would skip it disagrees with them.
		for _, old := range retiring {
			if pattern, covered := old.covers(root, relative, false); covered {
				disagreement = fmt.Errorf("%s skips %s (pattern %q), but neither the format block's \"ignore\" in the Nexus tier nor the ignorePatterns of %s does; put it in one of those lists and delete %s, which cohere no longer reads",
					old.name, filepath.ToSlash(relative), pattern, resolution.Source, old.name)
				return filepath.SkipAll
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
	if disagreement != nil {
		return enumeration, disagreement
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
