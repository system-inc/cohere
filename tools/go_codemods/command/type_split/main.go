// Command typesplit splits Go files so each serves at most one type (ruling 13 (c): the named types with
// methods it declares, together with the receiver types of the methods it writes), without changing
// what the code is: only type declarations, methods and their doc comments move, and a package whose
// result fails the equivalence check is left alone.
//
//	typesplit -root <repository>            measure and report what a split would do
//	typesplit -root <repository> -write     do it
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/streams"
)

// alwaysSkipped is the TypeScript submodule, upstream's code rather than cohere's.
const alwaysSkipped = "TypeScript"

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code typesplit exits with: runCommandLine returns its exit code here, and
// streams.Finish delivers what was printed and turns a clean exit into 1 when it did not arrive.
func finishedExitCode() int {
	// Deferred calls run while a panic unwinds, though not on os.Exit, so this delivers what was written before
	// the panic's own report, and main exits only after it has run.
	defer func() {
		streams.Show(streams.Errors)
		streams.Show(streams.Output)
	}()
	return streams.Finish(runCommandLine())
}

// runCommandLine reads the flags and runs typesplit, returning the code it exits with.
func runCommandLine() int {
	root := flag.String("root", ".", "the repository root")
	write := flag.Bool("write", false, "write the split; without it, only report")
	reportPath := flag.String("report", "", "also write the report to this file")
	servedPath := flag.String(
		"served",
		"",
		"write each judged file serving more than one type, with the types, to this file",
	)
	renamesPath := flag.String(
		"renames",
		"",
		"write every file move, old path, a tab and new path per line, to this file",
	)
	misnamedPath := flag.String(
		"misnamed",
		"",
		"write each file the file name rule flags before the split, with its type, to this file",
	)
	skip := skipPrefixes{alwaysSkipped}
	flag.Var(&skip, "skip", "a path prefix from the root to leave alone; repeatable")
	flag.Parse()
	// Flag parsing stops at the first argument that is not a flag, so a stray word in a generated
	// skip list would drop every flag after it, -write included, and the run would quietly do nothing.
	if flag.NArg() > 0 {
		fmt.Fprintf(streams.Errors, "typesplit: unexpected argument %q; every flag after it was ignored\n", flag.Arg(0))
		return 2
	}
	if runError := run(*root, *write, *reportPath, *servedPath, *misnamedPath, *renamesPath, skip); runError != nil {
		fmt.Fprintln(streams.Errors, "typesplit:", runError)
		return 1
	}
	return 0
}

func run(root string, write bool, reportPath string, servedPath string, misnamedPath string, renamesPath string,
	skip skipPrefixes) error {
	names := newPackageNames(root)
	pathsInCode, pathsError := goPathsInCode(root)
	if pathsError != nil {
		return pathsError
	}
	around := surroundings{importName: names.name, builds: hostBuild(build.Default), pathsInCode: pathsInCode}

	var directories []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if !entry.IsDir() {
			return nil
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		base := entry.Name()
		if relative != "." && (skip.matches(relative) || base == "testdata" || base == "vendor" ||
			strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_")) {
			return filepath.SkipDir
		}
		directories = append(directories, relative)
		return nil
	})
	if walkError != nil {
		return walkError
	}

	var report bytes.Buffer
	var before counts
	var after counts
	var newFiles []string
	var deleted []string
	var conflicts []string
	var families []string
	var failures []string
	var pinnedList []string
	var servedLines []string
	var misnamedLines []string
	var renamed []string
	var merged []string
	var nameConflicts []string
	var moveLines []string
	typeMoves, methodMovesToDeclaration, methodMovesToThemed, extractions := 0, 0, 0, 0
	// pending is every file the pass changes, by its path from the root, nil to remove it, written at the end.
	pending := map[string][]byte{}
	var allMoves []fileMove
	for _, directory := range directories {
		entries, readDirectoryError := os.ReadDir(filepath.Join(root, directory))
		if readDirectoryError != nil {
			return readDirectoryError
		}
		sources := map[string][]byte{}
		var otherNames []string
		pinned := map[string]string{}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() ||
				!strings.HasSuffix(name, ".go") ||
				strings.HasPrefix(name, ".") ||
				strings.HasPrefix(name, "_") {
				otherNames = append(otherNames, name)
				continue
			}
			source, readError := os.ReadFile(filepath.Join(root, directory, name))
			if readError != nil {
				return readError
			}
			sources[name] = source
			if skip.matches(joinPath(directory, name)) {
				pinned[name] = "skipped: other work owns it"
			}
		}
		if len(sources) == 0 {
			continue
		}
		result, splitDirectoryError := splitDirectory(directory, sources, otherNames, pinned, around)
		if splitDirectoryError != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", directory, splitDirectoryError))
			continue
		}
		before.add(result.before)
		after.add(result.after)
		typeMoves += result.typeMoves
		extractions += result.extractions
		methodMovesToDeclaration += result.methodMovesToDeclaration
		methodMovesToThemed += result.methodMovesToThemed
		for _, name := range result.newFiles {
			newFiles = append(newFiles, joinPath(directory, name))
		}
		for _, name := range result.deleted {
			deleted = append(deleted, joinPath(directory, name))
		}
		conflicts = append(conflicts, result.conflicts...)
		families = append(families, result.families...)
		failures = append(failures, result.failures...)
		pinnedList = append(pinnedList, result.pinnedFiles...)
		for _, line := range result.servingSeveral {
			servedLines = append(servedLines, joinPath(directory, line))
		}
		for _, move := range result.moves {
			moveLines = append(moveLines, joinPath(directory, move.from)+"\t"+joinPath(directory, move.to))
		}
		renamed = append(renamed, result.renamed...)
		merged = append(merged, result.merged...)
		nameConflicts = append(nameConflicts, result.nameConflicts...)
		for _, line := range result.misnamedBefore {
			misnamedLines = append(misnamedLines, joinPath(directory, line))
		}
		for name, content := range result.changes {
			pending[joinPath(directory, name)] = content
		}
		for _, move := range append(append([]fileMove(nil), result.moves...), result.continuations...) {
			allMoves = append(allMoves, fileMove{from: joinPath(directory, move.from), to: joinPath(directory, move.to),
				continues: move.continues})
		}
	}

	// The mentions of removed files are rewritten over the tree as the pass leaves it, since a path from the root
	// may sit in any directory.
	exists := func(relative string) bool {
		if content, isPending := pending[relative]; isPending {
			return content != nil
		}
		_, statError := os.Stat(filepath.Join(root, relative))
		return statError == nil
	}
	tree, treeError := mentionTree(root, skip, pending)
	if treeError != nil {
		return treeError
	}
	mentions := rewriteMentions(tree, referents(allMoves, exists))
	for relative, content := range mentions.changes {
		pending[relative] = content
	}

	if write {
		changed := make([]string, 0, len(pending))
		for relative := range pending {
			changed = append(changed, relative)
		}
		sort.Strings(changed)
		for _, relative := range changed {
			path := filepath.Join(root, relative)
			if pending[relative] == nil {
				if removeError := os.Remove(path); removeError != nil {
					return removeError
				}
				continue
			}
			if writeError := os.WriteFile(path, pending[relative], 0o644); writeError != nil {
				return writeError
			}
		}
	}

	section := func(title string, lines []string) {
		fmt.Fprintf(&report, "\n%s (%d)\n", title, len(lines))
		for _, line := range lines {
			fmt.Fprintf(&report, "  %s\n", line)
		}
	}
	mode := "measured, nothing written"
	if write {
		mode = "written"
	}
	fmt.Fprintf(&report, "typesplit over %s: %s\n\n", root, mode)
	fmt.Fprintf(&report, "%-48s %8s %8s\n", "", "before", "after")
	row := func(label string, beforeValue int, afterValue int) {
		fmt.Fprintf(&report, "%-48s %8d %8d\n", label, beforeValue, afterValue)
	}
	row("types with methods", before.typesWithMethods, after.typesWithMethods)
	row("files serving more than one (ruling 13 (c))", before.filesServingSeveral, after.filesServingSeveral)
	row("types those files serve", before.typesInFilesServingSeveral, after.typesInFilesServingSeveral)
	row("types after a file's first (the rule's findings)", before.extraTypes, after.extraTypes)
	row("files not named for the one type they serve", before.misnamed, after.misnamed)
	row(fmt.Sprintf("files over %d lines", lineLimit), before.filesOverLineLimit, after.filesOverLineLimit)
	row(
		"types declared once per build (do not move)",
		before.ambiguousTypesWithMethods,
		after.ambiguousTypesWithMethods,
	)
	row("files pinned (generated, skipped, unknown import)", before.pinnedFiles, after.pinnedFiles)
	row("  of which serving more than one", before.pinnedFilesServingSeveral, after.pinnedFilesServingSeveral)
	fmt.Fprintf(&report, "\ntypes moved with their methods from that file: %d\n", typeMoves)
	fmt.Fprintf(&report, "files whose one type, a minority of the file, moved out: %d\n", extractions)
	fmt.Fprintf(&report, "groups of methods moved to their type's declaration file: %d\n", methodMovesToDeclaration)
	fmt.Fprintf(&report, "groups of methods moved to a themed file: %d\n", methodMovesToThemed)
	section("new files", newFiles)
	section("files emptied to their package clause and deleted", deleted)
	section("conflicts, left in place", conflicts)
	section("files renamed for the one type they serve", renamed)
	section("files merged into the file named for their type", merged)
	section("files the name pass left alone", nameConflicts)
	section("mentions of removed files rewritten", mentions.rewritten)
	section("mentions of removed files left, for a person", mentions.left)
	section("family candidates, for a person to decide", families)
	section("files pinned, nothing moves into or out of them", pinnedList)
	section("packages left alone: the equivalence check or parsing failed", failures)

	if servedPath != "" {
		if writeError := os.WriteFile(
			servedPath,
			[]byte(strings.Join(servedLines, "\n")+"\n"),
			0o644,
		); writeError != nil {
			return writeError
		}
	}
	if renamesPath != "" {
		if writeError := os.WriteFile(
			renamesPath,
			[]byte(strings.Join(moveLines, "\n")+"\n"),
			0o644,
		); writeError != nil {
			return writeError
		}
	}
	if misnamedPath != "" {
		if writeError := os.WriteFile(
			misnamedPath,
			[]byte(strings.Join(misnamedLines, "\n")+"\n"),
			0o644,
		); writeError != nil {
			return writeError
		}
	}
	streams.Output.Write(report.Bytes())
	if reportPath != "" {
		return os.WriteFile(reportPath, report.Bytes(), 0o644)
	}
	return nil
}

func joinPath(directory string, name string) string {
	if directory == "." {
		return name
	}
	return directory + "/" + name
}

// packageNames resolves an import path to the name its package clause declares when the package is
// one of the repository's own modules, and to goimports' assumed name otherwise.
type packageNames struct {
	modules map[string]string
	cache   map[string]string
}

func newPackageNames(root string) *packageNames {
	names := &packageNames{modules: map[string]string{}, cache: map[string]string{}}
	var moduleFiles []string
	// Ignored: the callback returns nil for every entry, an unreadable one too, so the walk cannot fail, and a go.mod
	// it cannot reach leaves only its module's name to goimports' assumption.
	filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError == nil && !entry.IsDir() && entry.Name() == "go.mod" {
			moduleFiles = append(moduleFiles, path)
		}
		return nil
	})
	// The TypeScript submodule is a symbolic link in a scratch worktree, which the walk does not follow.
	moduleFiles = append(moduleFiles, filepath.Join(root, "TypeScript", "tsc", "go.mod"))
	for _, path := range moduleFiles {
		content, readError := os.ReadFile(path)
		if readError != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "module" {
				names.modules[strings.Trim(fields[1], "\"")] = filepath.Dir(path)
			}
		}
	}
	return names
}

func (names *packageNames) name(importPath string) string {
	if cached, ok := names.cache[importPath]; ok {
		return cached
	}
	resolved := assumedImportName(importPath)
	longest := ""
	for modulePath := range names.modules {
		if (importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/")) &&
			len(modulePath) > len(longest) {
			longest = modulePath
		}
	}
	if longest != "" {
		directory := filepath.Join(names.modules[longest], strings.TrimPrefix(importPath, longest))
		if entries, readDirectoryError := os.ReadDir(directory); readDirectoryError == nil {
			for _, entry := range entries {
				name := entry.Name()
				if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
					continue
				}
				syntax, parseFileError := parser.ParseFile(
					token.NewFileSet(),
					filepath.Join(directory, name),
					nil,
					parser.PackageClauseOnly,
				)
				if parseFileError == nil && syntax.Name.Name != "main" && syntax.Name.Name != "documentation" {
					resolved = syntax.Name.Name
					break
				}
			}
		}
	}
	names.cache[importPath] = resolved
	return resolved
}

// goPathsInCode is every path from root ending in .go that a Go file under root spells in a string literal, read
// as a path from root. testdata and vendor are read too, since code there may name a source file it checks.
func goPathsInCode(root string) (map[string]bool, error) {
	paths := map[string]bool{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative == alwaysSkipped || strings.HasPrefix(entry.Name(), ".") && relative != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		syntax, parseError := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if parseError != nil {
			return nil
		}
		for _, literal := range goFileLiterals(syntax) {
			if strings.Contains(literal, "/") {
				paths[strings.TrimPrefix(filepath.ToSlash(filepath.Clean(literal)), "./")] = true
			}
		}
		return nil
	})
	return paths, walkError
}

// mentionTree is every file under root that rewriteMentions reads, by its path from the root, with its content as
// the pass leaves it. TypeScript, hidden directories, testdata, whose comments a fixture's test may read, and the
// skipped prefixes are left out.
func mentionTree(root string, skip skipPrefixes, pending map[string][]byte) (map[string][]byte, error) {
	tree := map[string][]byte{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		relative, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			name := entry.Name()
			if relative != "." && (skip.matches(relative) || name == "testdata" || name == "vendor" ||
				strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !inMentionScope(relative) || skip.matches(relative) {
			return nil
		}
		if _, isPending := pending[relative]; isPending {
			return nil
		}
		content, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		tree[relative] = content
		return nil
	})
	for relative, content := range pending {
		if content != nil && inMentionScope(relative) {
			tree[relative] = content
		}
	}
	return tree, walkError
}
