package main

import (
	"path/filepath"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/types/program"
)

// A run that checked nothing never reads like one that checked everything and found it clean (#08cv2mp). A
// TypeScript run decides it from what its checking phases checked, once they are done, and not from the program's
// file list: since #cxeq6jw a file an ignore matched stays in that list and is dropped by the type check and by
// lint, so a project whose every file an ignore matched read `0 checked`, passed, and said nothing of it. A file
// the type check alone reads, under an entry naming `"check": "types"`, is checked. Each file kept out is counted
// by what kept it out, a narrowing to named paths or the ignore entry that matched it, and the front door fails the
// run and says why (runGaps.nothingChecked). It fails on the program's own files having been there, never on a
// reason having been counted, so a file a path dropped without counting it is named as cohere's bug. A program with
// no file of its own has nothing kept out, and keeps the warning alone.

// outsideNamedPaths is the reason a narrowing gives, said of the program's files no named path reached.
const outsideNamedPaths = "outside the named paths"

// settleNothingChecked sets the summary's NothingToCheck once the phases are done, and, for a run lint checked
// without the type check, what kept each file out, which the type check otherwise counts (finishScopedTypeCheck).
// projectFiles is the run's scope, and lintConfiguration lint's settings, nil when lint did not run.
//
// A run whose checking phases were both left out, `--fix` alone, checks no file by design, so it keeps the old
// reading, nothing to check only when its program held no file in scope, with no reason that would fail it. A run
// with no program, `--format-only`'s, is never nothing to check here: its format scope is its account
// (format_only.go).
func settleNothingChecked(
	graph *program.Graph,
	projectFiles []*ast.SourceFile,
	lintConfiguration *configuration.Configuration,
	location projectLocation,
	typesRan bool,
	lintRan bool,
) {
	if graph == nil {
		return
	}
	if !typesRan && !lintRan {
		activeSummary.Gaps.NothingToCheck = len(projectFiles) == 0
		return
	}
	activeSummary.Gaps.NothingToCheck =
		activeSummary.FilesChecked+activeSummary.FilesCached+activeSummary.FilesTypesOnly == 0
	if !activeSummary.Gaps.NothingToCheck {
		return
	}
	activeSummary.Gaps.SourceFiles = len(graph.ProjectFiles())
	if !typesRan {
		addTypeScriptExcluded(graph, projectFiles, lintConfiguration, location.Root)
	}
	activeSummary.Gaps.countUnrecorded()
}

// addTypeScriptExcluded counts what kept each of the program's files out of a run over projectFiles that checked
// none of them: first the narrowing, which comes before any ignore is asked, then the ignore entry Resolve says
// decided each file in scope, with the settings file that wrote it, relative to root. Asked only of a run that
// checked nothing, so every file in scope is one an ignore kept out, and resolving them again costs that run alone.
func addTypeScriptExcluded(
	graph *program.Graph,
	projectFiles []*ast.SourceFile,
	settings *configuration.Configuration,
	root string,
) {
	if outside := len(graph.ProjectFiles()) - len(projectFiles); outside > 0 {
		activeSummary.Gaps.addExcluded(outsideNamedPaths, "", outside)
	}
	if settings == nil {
		return
	}
	for _, sourceFile := range projectFiles {
		resolved := settings.Resolve(sourceFile.FileName().AsString())
		if !resolved.Ignored || resolved.TypesOnly {
			continue
		}
		activeSummary.Gaps.addExcluded(
			`matched ignorePatterns "`+resolved.IgnoredBy+`"`,
			ignoreEntrySource(settings, resolved.IgnoredBy, root),
			1,
		)
	}
}

// ignoreEntrySource is the settings file that wrote the ignore entry with pattern, relative to root, or the set's
// name, and empty when no entry of these settings wrote it, as a house variant's own may.
func ignoreEntrySource(settings *configuration.Configuration, pattern string, root string) string {
	for _, entry := range settings.IgnoreEntries {
		if entry.Pattern != pattern {
			continue
		}
		if configuration.IsSet(entry.File) {
			return entry.File
		}
		if relative, relativePathError := filepath.Rel(root, entry.File); relativePathError == nil {
			return relative
		}
		return entry.File
	}
	return ""
}
