package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/formatfiles"
)

/*
 * Nested repositories: a library checked out inside a project, such as libraries/structure in ahra
 * and libraries/structure/libraries/nexus inside it.
 *
 * The rule (@system_cohere, 2026-10-03): writes stay inside one repository, reads reach everywhere.
 *
 * A library has no tsconfig of its own, so a run started inside it finds the project above it and
 * checks that project's program, which holds the library's code. Writing is a different question. A
 * run inside a library formats that library, rooted at its own `.git`, and writes nothing outside it,
 * so the commit it leaves belongs to the library. A run in the project never writes into a library,
 * but its check reads every library's files and reports the ones the library's own run would rewrite,
 * so drift there fails the project's check rather than staying invisible to every gate.
 *
 * Before this, no gate formatted Structure or Nexus. A run in the project skipped them as somebody
 * else's tree, and a run inside them formatted the project above, 3,086 of ahra's files and none of
 * their own: 8 of Structure's 1,683 files and 8 of Nexus's 323 had drifted with nothing saying so.
 */

// writeRepositoryRoot is the repository a run started at start may write: the nearest directory at or
// above start that is a repository of its own, when that is below the project root, and the project
// root otherwise.
//
// Only a boundary strictly inside the project counts. The project's own `.git` is its root, and a
// project that is not a repository at all (a test fixture, a tarball) writes where it always did.
func writeRepositoryRoot(start string, projectRoot string) string {
	projectRoot = filepath.Clean(projectRoot)
	for directory := filepath.Clean(start); ; directory = filepath.Dir(directory) {
		relative, err := filepath.Rel(projectRoot, directory)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return projectRoot
		}
		if formatfiles.HasOwnRepository(directory) {
			return directory
		}
	}
}

// unwritableRepository says why a run writing repositoryRoot may not write fileName, as the phrase a
// note prints: "nested repository X" for a repository below the root that holds it, or "the project
// outside X" when the file lies outside the root altogether, as the project's files do for a run inside
// a library. Empty means the file is this run's to write.
func unwritableRepository(repositoryRoot string, fileName string) string {
	relative, err := filepath.Rel(filepath.Clean(repositoryRoot), filepath.Clean(fileName))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "the project outside " + repositoryRoot
	}
	if nested := formatfiles.NestedRepositoryContaining(repositoryRoot, fileName); nested != "" {
		return "nested repository " + nested
	}
	return ""
}

// nestedDrift is one file in a nested repository that the repository's own run would rewrite.
type nestedDrift struct {
	// Repository is the nested repository's root, relative to the project.
	Repository string
	FileName   string
}

// nestedDriftCheck is what reading the nested repositories found.
type nestedDriftCheck struct {
	Drift []nestedDrift

	// Repositories and Files are what was read, so a check that found nothing still says it looked.
	Repositories int
	Files        int
}

// checkNestedRepositories reads every submodule root declares, and every submodule those declare, and
// reports each file the submodule's own run would rewrite. It writes nothing.
//
// Only declared submodules, read from each repository's own `.gitmodules`: a walk also finds
// repositories cloned into ignored directories (ahra holds six under `projects/`), and those belong to
// nobody running here. Each submodule is walked as a run rooted there would walk it, with its own ignore
// layers. Only the formatter is asked: a fix in a library's code is a finding the project's lint already
// reports, at its position, so repeating it here would count one problem twice.
func checkNestedRepositories(engine formatEngine, root string) (nestedDriftCheck, error) {
	var check nestedDriftCheck
	transform := formatTransform(engine)

	pending, err := declaredBelow(root, "")
	if err != nil {
		return check, err
	}
	for len(pending) > 0 {
		relative := pending[0]
		pending = pending[1:]
		repositoryRoot := filepath.Join(root, relative)
		if !formatfiles.HasOwnRepository(repositoryRoot) {
			// Declared and not checked out: there is nothing on disk to read.
			continue
		}
		check.Repositories++

		inner, err := declaredBelow(root, relative)
		if err != nil {
			return check, err
		}
		pending = append(pending, inner...)

		enumeration, err := engine.Enumerate(repositoryRoot)
		if err != nil {
			return check, fmt.Errorf("reading nested repository %s: %w", relative, err)
		}
		declareFormatWalk(enumeration)
		for _, fileName := range enumeration.Files {
			check.Files++
			contents, err := os.ReadFile(fileName)
			if err != nil {
				return check, fmt.Errorf("reading %s: %w", fileName, err)
			}
			text := string(contents)
			formatted, err := transform(fileName, text)
			if errors.Is(err, edit.ErrSkipped) {
				continue
			}
			if err != nil {
				return check, fmt.Errorf("formatting %s in nested repository %s: %w", fileName, relative, err)
			}
			if formatted != text {
				check.Drift = append(check.Drift, nestedDrift{Repository: relative, FileName: fileName})
			}
		}
	}

	sort.Slice(check.Drift, func(first, second int) bool {
		return check.Drift[first].FileName < check.Drift[second].FileName
	})
	return check, nil
}

// declaredBelow is the submodules the repository at root/relative declares, each relative to root,
// sorted so the read and its report are in a stable order.
func declaredBelow(root string, relative string) ([]string, error) {
	declared, err := declaredSubmodules(filepath.Join(root, relative))
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(declared))
	for path := range declared {
		paths = append(paths, filepath.Join(relative, filepath.FromSlash(path)))
	}
	sort.Strings(paths)
	return paths, nil
}

// printNestedDrift reports each drifted file as a finding, in the shape every other finding prints,
// naming the repository whose own run fixes it.
func printNestedDrift(out io.Writer, check nestedDriftCheck) {
	for _, drift := range check.Drift {
		fmt.Fprintf(out, "%s:1:1 - the formatter would rewrite this file in nested repository %s, which a run here never writes: run cohere there [format/nested-drift]\n",
			drift.FileName, drift.Repository)
	}
}
