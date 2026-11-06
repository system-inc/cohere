package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/format_files"
)

// wholeTreeScope is every file, for a deliberate normalization pass or a continuous-integration
// check that wants the whole corpus.
//
// It is a flag rather than a default, and it is allowed to cost five minutes, because someone who
// asks for the whole tree has said what they want. The default costing five minutes is what gets a
// gate bypassed.
func wholeTreeScope() formatScope {
	return formatScope{Everything: true, Description: "every file (--format-all)"}
}

// scopedTransform narrows a transform to the files a scope admits.
//
// Files outside the scope are skipped with the scope's own description as the reason, so they are
// counted and named rather than quietly passed over. That is the difference between "formatted 12
// files" with no denominator and a line that says what was in scope and why, which is the same
// discipline the skip channel already carries for unhandled file types.
func scopedTransform(inner edit.Transform, scope formatScope) edit.Transform {
	if inner == nil {
		return nil
	}
	if scope.Everything {
		return inner
	}

	return func(fileName string, text string, parsed *ast.SourceFile) (string, error) {
		if !scope.includes(fileName) && (scope.recorded == nil || scope.recorded(fileName, text)) {
			return "", fmt.Errorf("%w: outside the format scope, %s", edit.ErrSkipped, scope.Description)
		}
		return inner(fileName, text, parsed)
	}
}

// describeEnumeration renders what the walk actually did.
//
// Stated rather than summarized, because the requirement is that this line cannot become correct by
// accident. When the universe changes, this string has to be rewritten deliberately, so it names the
// root, the ignore layers that removed anything, the nested repositories it refused, and the
// declined extensions.
func describeEnumeration(enumeration format_files.Enumeration, formattable int) string {
	// One format string rather than two. An earlier version branched between a form that named the
	// formattable count and one that did not, and the branch made the root unobservable: a mutation
	// deleting the root from one string left the other correct, so no fixture could see it. Two
	// spellings of one sentence means neither can be shown to work.
	description := fmt.Sprintf(
		"walked %s, %d files, %d the formatter handles",
		enumeration.Root, enumeration.Walked, formattable,
	)

	// Ignore layers are named in the order they ran, and only when they removed something. A layer
	// that removed nothing is either correct or broken, and printing a zero for it every run trains
	// the reader to stop looking.
	layers := make([]string, 0, len(enumeration.IgnoredByLayer))
	for layer := range enumeration.IgnoredByLayer {
		if enumeration.IgnoredByLayer[layer] > 0 {
			layers = append(layers, fmt.Sprintf("%d by %s", enumeration.IgnoredByLayer[layer], layer))
		}
	}
	sort.Strings(layers)
	if len(layers) > 0 {
		description += ", ignored " + strings.Join(layers, ", ")
	}

	// Nested repositories are named rather than counted. "We skipped a repo" is a fact somebody may
	// want to argue with, and a number gives them nothing to argue with.
	if len(enumeration.NestedRepositories) > 0 {
		names := append([]string(nil), enumeration.NestedRepositories...)
		sort.Strings(names)
		description += ", skipped nested repositories " + strings.Join(names, ", ")
	}

	// A symbolic link is never followed or read (see Enumeration.SymbolicLinks), and says so.
	if enumeration.SymbolicLinks > 0 {
		description += fmt.Sprintf(", skipped %d symbolic links, which are never followed", enumeration.SymbolicLinks)
	}

	// Declines are the requirement this whole change exists for: a file the engine cannot handle is a
	// named zero rather than an absence.
	if len(enumeration.DeclinedExtensions) > 0 {
		extensions := make([]string, 0, len(enumeration.DeclinedExtensions))
		for extension := range enumeration.DeclinedExtensions {
			extensions = append(extensions, extension)
		}
		sort.Strings(extensions)

		declined := make([]string, 0, len(extensions))
		for _, extension := range extensions {
			declined = append(declined, fmt.Sprintf("%d %s", enumeration.DeclinedExtensions[extension], extension))
		}
		description += ", declined " + strings.Join(declined, ", ")
	}

	return description
}

// namedPathsScope resolves the paths a caller named on the command line.
//
// A positional path was parsed and discarded before this existed, so `cohere --lint OneFile.ts`
// checked all 3,542 files and reported every finding in the tree. Measured: 3.042s and 5,201
// findings for one named file, against 3.003s and 5,201 for the whole tree. Same cost, same output,
// and no way to tell from either that the argument had been ignored.
//
// A named directory expands to the files under it rather than matching by prefix, so membership
// stays one map lookup for every phase downstream. `.` is the whole tree and says so, which keeps
// `cohere --lint .` meaning what it has always meant instead of quietly becoming a subset of one
// directory entry.
//
// Resolution is against the working directory the caller gave, not the process's own, because
// `--directory` already redefines what a relative path means everywhere else in this binary and a
// second answer here would be a bug nobody could see from the output.
//
// The whole-tree answer belongs to the project root and not to the working directory, and the two
// stopped being the same directory once the root could be found by walking up. `cohere .` from
// `modules/tasks` means that directory, and reading it as the whole project would turn the
// narrowest thing a caller can type into the widest run there is.
func namedPathsScope(workingDirectory string, root string, names []string) (formatScope, error) {
	// `--directory` defaults to empty, meaning the process's own. Resolving against empty leaves a
	// relative path, which never matches a source file name because those are absolute, so every
	// named path fell out of scope and the run checked nothing while reporting success. That is the
	// green-over-zero-files failure this binary exists to have stopped making, and it was one
	// `filepath.Join` away from shipping.
	resolvedWorkingDirectory := workingDirectory
	if workingDirectory == "" {
		current, workingDirectoryError := os.Getwd()
		if workingDirectoryError != nil {
			return formatScope{}, fmt.Errorf("resolving the working directory: %w", workingDirectoryError)
		}
		resolvedWorkingDirectory = current
	}
	resolvedRoot := root
	if root == "" {
		resolvedRoot = resolvedWorkingDirectory
	}

	absolute := make([]string, 0, len(names))

	for _, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(resolvedWorkingDirectory, name)
		}
		path = filepath.Clean(path)

		information, statError := os.Stat(path)
		if statError != nil {
			// A path that does not exist is a loud failure rather than an empty scope. Silently
			// checking nothing because of a typo is the green-over-zero-files defect, and it is the
			// one this binary exists to have stopped making.
			return formatScope{}, fmt.Errorf("resolving %s: %w", name, statError)
		}

		if !information.IsDir() {
			absolute = append(absolute, path)
			continue
		}

		if path == filepath.Clean(resolvedRoot) {
			return wholeTreeScope(), nil
		}

		walkError := filepath.WalkDir(path, func(entry string, directoryEntry fs.DirEntry, pathError error) error {
			if pathError != nil {
				return pathError
			}
			if directoryEntry.IsDir() {
				return nil
			}
			absolute = append(absolute, filepath.Clean(entry))
			return nil
		})
		if walkError != nil {
			return formatScope{}, fmt.Errorf("walking %s: %w", name, walkError)
		}
	}

	sort.Strings(absolute)

	index := make(map[string]struct{}, len(absolute))
	for _, path := range absolute {
		index[path] = struct{}{}
	}

	description := fmt.Sprintf("%d named path%s", len(absolute), plural(len(absolute)))
	if len(absolute) == 0 {
		description = fmt.Sprintf("0 files under the named paths in %s", resolvedWorkingDirectory)
	}

	request := strings.Join(names, ", ")
	if len(names) > 3 {
		request = fmt.Sprintf("%s and %d more", strings.Join(names[:3], ", "), len(names)-3)
	}

	return formatScope{
		FileNames:          absolute,
		index:              index,
		Description:        description,
		RequestDescription: request,
	}, nil
}

// plural is the one-character suffix that keeps a count reading as English.
func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
