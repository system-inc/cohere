// Package gitignore decides which paths a repository's ignore files exclude, the way git decides it,
// without running git.
//
// It reads what gitignore(5) names for a working tree: every `.gitignore` from the repository root down,
// each scoped to its own directory, and `.git/info/exclude` when `.git` is a directory. The user's global
// core.excludesFile is not read: it is configuration on one machine, not the repository's, so a check that
// read it would pass on one machine and fail on another. Matching is case sensitive, as git's is unless
// core.ignorecase is set, which is likewise a property of a clone rather than of the repository.
//
// That is the house rule for all three engines, Go and TypeScript through this package and Swift through its
// IgnoreRules (#jgk8ppa): the repository's rules alone, every `.gitignore` plus info/exclude, a worktree's in its
// common directory, matched case sensitively, never the global excludes file or core.ignorecase. A verdict belongs
// to the repository, never the machine, so one tree checks the same files on Kirk's Mac and on a Linux box. Where
// that differs from git on a case-insensitive Mac, it checks more, the safe side, and a tracked file is never
// ignored. A new engine reads the same rules.
//
// The semantics are gitignore(5)'s:
//
//   - Precedence, highest first: a `.gitignore` in the path's own directory, then each parent's up to the
//     root, then info/exclude. Within one file the last matching line decides, and a `!` line re-includes.
//   - A pattern with a `/` at its start or middle is anchored to its file's directory. One without matches
//     a base name at any depth below it. A trailing `/` matches directories only.
//   - Nothing below an excluded directory can be re-included: git never looks inside one, so the rule
//     that excluded the directory decides every path under it.
//   - Trailing spaces are dropped unless escaped, `#` starts a comment, and a backslash escapes `#`, `!`
//     and any other byte. A file's byte order mark and each line's carriage return are not part of a
//     pattern.
//
// What git would read differently from its documentation is refused by name rather than followed: a
// `.gitignore` that is a symbolic link (git does not follow one inside a working tree), one larger than
// 100 MiB (git skips it with a warning), and a line holding a NUL byte. Each is an error naming the file,
// because skipping one would leave its patterns unapplied without a word.
//
// A path git tracks is never ignored (gitignore(5): "Files already tracked by Git are not affected"), so New
// reads the repository's index too, natively, and Ignored answers false for a tracked path and for a directory
// holding one, as git check-ignore does. An index this package cannot read faithfully (none yet, a split index,
// a sparse one, or one that is not what git writes) is not guessed at: the matcher decides by the ignore files
// alone, as before it read the index, and IndexFallback says why.
//
// The matcher only answers. Walking, and pruning what it excludes, belong to the caller: it enters each
// directory it descends into with Enter, and asks Ignored about each entry it finds there.
package gitignore

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// IgnoreFileName is the per-directory ignore file.
const IgnoreFileName = ".gitignore"

// ExcludeFile is the repository's own exclude file, relative to its root, where a clone keeps it. A worktree's and a
// submodule's are in the git directory their `.git` file points to: see ExcludeFileOf.
const ExcludeFile = ".git/info/exclude"

// ExcludeFileOf is the exclude file git reads for the repository whose `.git` is marker, an absolute path, and the
// name a Source gives it: ExcludeFile below a `.git` directory, and below a `.git` file, `info/exclude` in the common
// directory of the git directory it points to, which a worktree shares with its main checkout, named by its absolute
// path as git check-ignore names it. A pointer that cannot be read gives "", since git cannot read that repository
// either.
func ExcludeFileOf(marker string, isDirectory bool) (path string, name string) {
	if isDirectory {
		return filepath.Join(filepath.Dir(marker), filepath.FromSlash(ExcludeFile)), ExcludeFile
	}
	gitDirectory, gitDirectoryError := gitDirectoryOf(marker, false)
	if gitDirectoryError != nil {
		return "", ""
	}
	path = filepath.Join(commonDirectoryOf(gitDirectory), "info", "exclude")
	return path, filepath.ToSlash(path)
}

// maximumFileSize is the largest ignore file read. Git skips one past 100 MiB with a warning; here it is
// refused, since skipping it would leave every pattern in it unapplied without a word.
const maximumFileSize = 100 << 20

// ErrNestedRepository is a refusal to enter a directory that is a repository of its own: its ignore
// files govern it, through its own New, and the parent's patterns do not.
var ErrNestedRepository = errors.New("is a repository of its own")

// RepositoryOf is the nearest directory at or above start holding a `.git`, a clone's directory or a worktree's or
// submodule's file: the repository whose ignore files and index govern start, as git finds it from a subdirectory.
// It is "" when there is none. A matcher for a project below its repository's root is New of it, entered into the
// project's directory, so the `.gitignore` files above the project and the repository's index apply there too.
func RepositoryOf(start string) string {
	directory := filepath.Clean(start)
	for {
		if _, statError := os.Lstat(filepath.Join(directory, ".git")); statError == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

// Listing is a directory's entries as os.ReadDir returned them, and whether the directory was listed, for an
// absolute path. A directory not listed is not empty: it is read from the disk.
type Listing func(directory string) ([]os.DirEntry, bool)

// New reads a repository's root `.gitignore`, its info/exclude, and its index, when it holds a `.git` of either kind:
// a worktree's info/exclude is the one it shares with its main checkout (ExcludeFileOf). An index it cannot read is a
// fallback, said by IndexFallback, never an error.
func New(repositoryRoot string) (*Matcher, error) {
	root, absolutePathError := filepath.Abs(repositoryRoot)
	if absolutePathError != nil {
		return nil, absolutePathError
	}
	matcher := &Matcher{root: root}

	rules, readTreeFileError := readTreeFile(root, "")
	if readTreeFileError != nil {
		return nil, readTreeFileError
	}
	matcher.files = append(matcher.files, rules)

	marker := filepath.Join(root, ".git")
	information, statError := os.Stat(marker)
	if statError != nil {
		matcher.indexFallback = fmt.Errorf("%w: %s holds no .git", ErrNoIndex, root)
		return matcher, nil
	}
	matcher.index, matcher.indexFile, matcher.indexFallback = readRepositoryIndex(marker, information.IsDir())

	if excludePath, excludeName := ExcludeFileOf(marker, information.IsDir()); excludePath != "" {
		// info/exclude lives outside the tree, so a symbolic link to it is followed, as git follows it.
		contents, readError := readIgnoreFile(excludePath, true)
		if readError != nil {
			return nil, readError
		}
		var parseRulesError error
		matcher.exclude, parseRulesError = parseRules(contents, excludeName, "")
		if parseRulesError != nil {
			return nil, parseRulesError
		}
	}
	return matcher, nil
}

// CompilePatterns reads lines as the lines of an ignore file named name, which is what each Source
// reports. A line holding a newline or a NUL byte cannot be an ignore-file line and is refused.
func CompilePatterns(lines []string, name string) (*Patterns, error) {
	patterns := &Patterns{}
	for index, line := range lines {
		if strings.ContainsAny(line, "\n\x00") {
			return nil, fmt.Errorf(
				"gitignore: %s entry %d, %q, holds a newline or a NUL byte, which no ignore-file line can",
				name,
				index+1,
				line,
			)
		}
		line = strings.TrimSuffix(line, "\r")
		if line == "" || line[0] == '#' {
			continue
		}
		patterns.rules = append(patterns.rules, compileRule(trimTrailingSpaces(line), name, index+1, ""))
	}
	return patterns, nil
}

func lastMatching(rules []rule, relativePath string, baseName string, isDirectory bool) *rule {
	for index := len(rules) - 1; index >= 0; index-- {
		candidate := &rules[index]
		if candidate.directoryOnly && !isDirectory {
			continue
		}
		if candidate.matches(relativePath, baseName) {
			return candidate
		}
	}
	return nil
}

// readTreeFile reads the `.gitignore` of directory, relative to root, returning no rules when it has
// none.
func readTreeFile(root string, directory string) ([]rule, error) {
	relativeFile := joinRelative(directory, IgnoreFileName)
	contents, readIgnoreFileError := readIgnoreFile(filepath.Join(root, filepath.FromSlash(relativeFile)), false)
	if readIgnoreFileError != nil || contents == nil {
		return nil, readIgnoreFileError
	}
	return parseRules(contents, relativeFile, directory)
}

// readIgnoreFile returns a file's contents, or nil when it does not exist. follow says whether a symbolic
// link is read through: git follows one for info/exclude and not for a `.gitignore` in the tree.
func readIgnoreFile(filePath string, follow bool) ([]byte, error) {
	info, statError := os.Lstat(filePath)
	if errors.Is(statError, fs.ErrNotExist) {
		return nil, nil
	}
	if statError != nil {
		return nil, fmt.Errorf("gitignore: reading %s: %w", filePath, statError)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		if !follow {
			return nil, fmt.Errorf(
				"gitignore: %s is a symbolic link, which git does not follow inside a working tree, so its patterns would not apply; replace it with the file it points to",
				filePath,
			)
		}
		if info, statError = os.Stat(filePath); errors.Is(statError, fs.ErrNotExist) {
			return nil, nil
		} else if statError != nil {
			return nil, fmt.Errorf("gitignore: reading %s: %w", filePath, statError)
		}
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("gitignore: %s is not a regular file, so it cannot be read as an ignore file", filePath)
	}
	if info.Size() > maximumFileSize {
		return nil, fmt.Errorf(
			"gitignore: %s is larger than 100 MiB, which git skips with a warning, leaving its patterns unapplied; cohere refuses it rather than skip it",
			filePath,
		)
	}
	contents, readError := os.ReadFile(filePath)
	if readError != nil {
		return nil, fmt.Errorf("gitignore: reading %s: %w", filePath, readError)
	}
	return contents, nil
}

// byteOrderMark opens a file written by an editor that marks its encoding.
var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// parseRules compiles an ignore file's lines. file names it in every Source, and base is the directory its
// anchored patterns are relative to.
func parseRules(contents []byte, file string, base string) ([]rule, error) {
	text := bytes.TrimPrefix(contents, byteOrderMark)
	var rules []rule
	for index, line := range strings.Split(string(text), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || line[0] == '#' {
			continue
		}
		if strings.IndexByte(line, 0) >= 0 {
			return nil, fmt.Errorf("gitignore: %s:%d holds a NUL byte, which no pattern can contain", file, index+1)
		}
		line = trimTrailingSpaces(line)
		rules = append(rules, compileRule(line, file, index+1, base))
	}
	return rules, nil
}

// trimTrailingSpaces drops the spaces ending a line, except those a backslash escapes. Only spaces: a
// trailing tab is part of the pattern.
func trimTrailingSpaces(line string) string {
	// spacesStart is where the current run of unescaped spaces began, or -1 outside one.
	spacesStart := -1
	for index := 0; index < len(line); index++ {
		switch line[index] {
		case ' ':
			if spacesStart < 0 {
				spacesStart = index
			}
		case '\\':
			// The escaped byte is kept whatever it is, a space included.
			index++
			spacesStart = -1
		default:
			spacesStart = -1
		}
	}
	if spacesStart >= 0 {
		return line[:spacesStart]
	}
	return line
}

// compileRule reads one pattern line: an optional `!`, the pattern, and an optional trailing `/`.
func compileRule(line string, file string, lineNumber int, base string) rule {
	compiled := rule{base: base, source: Source{File: file, Line: lineNumber}}
	pattern := line
	if strings.HasPrefix(pattern, "!") {
		compiled.negated = true
		pattern = pattern[1:]
	}
	if strings.HasSuffix(pattern, "/") {
		compiled.directoryOnly = true
		pattern = pattern[:len(pattern)-1]
	}

	compiled.source.Pattern = pattern
	if compiled.negated {
		compiled.source.Pattern = "!" + compiled.source.Pattern
	}
	if compiled.directoryOnly {
		compiled.source.Pattern += "/"
	}

	compiled.anchored = strings.Contains(pattern, "/")
	if compiled.anchored {
		compiled.glob = compileGlob(strings.TrimPrefix(pattern, "/"), true)
	} else {
		compiled.glob = compileGlob(pattern, false)
	}
	return compiled
}

// cleanRelative normalizes a relative path to the slash form the matcher keys on, "" for the root. A walk
// passes paths already in that form, which are returned as they are without allocating.
func cleanRelative(relative string) string {
	if isCleanRelative(relative) {
		return relative
	}
	cleaned := path.Clean(filepath.ToSlash(relative))
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return strings.TrimPrefix(cleaned, "/")
}

// isCleanRelative reports whether a path is already what cleanRelative would make it: slash separated, with
// no empty, `.` or `..` segment and no leading or trailing slash.
func isCleanRelative(relative string) bool {
	if relative == "" {
		return true
	}
	if strings.ContainsRune(relative, '\\') && filepath.Separator == '\\' {
		return false
	}
	start := 0
	for index := 0; index <= len(relative); index++ {
		if index < len(relative) && relative[index] != '/' {
			continue
		}
		if segment := relative[start:index]; segment == "" || segment == "." || segment == ".." {
			return false
		}
		start = index + 1
	}
	return true
}

func joinRelative(directory string, name string) string {
	if directory == "" {
		return name
	}
	return directory + "/" + name
}

func displayDirectory(directory string) string {
	if directory == "" {
		return "the repository root"
	}
	return directory
}
