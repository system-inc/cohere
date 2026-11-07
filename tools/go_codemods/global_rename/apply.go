package global_rename

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"sort"

	"github.com/system-inc/cohere/tools/go_codemods/rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"
)

// identifierRecord is one identifier in a file, in source order, with what it resolves to.
type identifierRecord struct {
	Name string
	Key  string
}

// Fingerprint records, for every root package and each of its files, every identifier in source order and the
// declaration it defines and uses, and for a selector the path go/types took to the member. A declaration in a
// root file is keyed by the ordinal of its declaring identifier in that file, which a rename cannot move, so a
// use in one package of a declaration in another compares across a rename; anything else by package path,
// kind, owner, name and file, which no rename here changes.
func Fingerprint(loaded *workspace.Workspace) (map[string]map[string][]identifierRecord, map[string]map[string][]int) {
	type position struct {
		filename     string
		line, column int
	}
	ordinals := map[position]string{}
	seenFiles := map[string]bool{}
	for _, rootsPackage := range loaded.Roots {
		for _, file := range rootsPackage.Syntax {
			filename := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			if seenFiles[filename] {
				continue
			}
			seenFiles[filename] = true
			ordinal := 0
			ast.Inspect(file, func(node ast.Node) bool {
				if identifier, isIdentifier := node.(*ast.Ident); isIdentifier {
					where := rootsPackage.Fset.PositionFor(identifier.Pos(), false)
					ordinals[position{
						where.Filename,
						where.Line,
						where.Column,
					}] = fmt.Sprintf("%s#%d", filename, ordinal)
					ordinal++
				}
				return true
			})
		}
	}
	keyOf := func(object types.Object) string {
		if object == nil {
			return "-"
		}
		declared := origin(object)
		if declared.Pkg() == nil {
			return "universe:" + declared.Name()
		}
		if declared.Pos().IsValid() {
			where := loaded.Roots[0].Fset.PositionFor(declared.Pos(), false)
			if ordinal, found := ordinals[position{where.Filename, where.Line, where.Column}]; found {
				return "declared:" + ordinal
			}
			// Outside the root files, a declaration is named by kind, owner, name and its file's base name, never
			// by line, column or full path: export data spells them one way (module-relative, column 1) and a
			// source load, which an overlay reload can turn to, another, for the same declaration. No rename here
			// moves a declaration outside the root files.
			return fmt.Sprintf(
				"other:%s:%s:%s%s@%s",
				declared.Pkg().Path(),
				kindOf(declared),
				ownerOf(declared),
				declared.Name(),
				filepath.Base(where.Filename),
			)
		}
		return fmt.Sprintf(
			"other:%s:%s:%s%s",
			declared.Pkg().Path(),
			kindOf(declared),
			ownerOf(declared),
			declared.Name(),
		)
	}
	fingerprints := map[string]map[string][]identifierRecord{}
	offsets := map[string]map[string][]int{}
	for _, rootsPackage := range loaded.Roots {
		selections := map[*ast.Ident]*types.Selection{}
		for selector, selection := range rootsPackage.TypesInfo.Selections {
			selections[selector.Sel] = selection
		}
		byFile := map[string][]identifierRecord{}
		offsetsByFile := map[string][]int{}
		for _, file := range rootsPackage.Syntax {
			filename := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			ast.Inspect(file, func(node ast.Node) bool {
				identifier, isIdentifier := node.(*ast.Ident)
				if !isIdentifier {
					return true
				}
				key := keyOf(rootsPackage.TypesInfo.Defs[identifier]) +
					"|" +
					keyOf(rootsPackage.TypesInfo.Uses[identifier])
				if implicit := rootsPackage.TypesInfo.Implicits[identifier]; implicit != nil {
					key += "|implicit " + keyOf(implicit)
				}
				if selection, selected := selections[identifier]; selected {
					// The path through embedded fields is kept whole. A method's own index, the last, is its place
					// in its type's method list, which export data and a source load order differently for one
					// type, and the key already names the method.
					path := selection.Index()
					if selection.Kind() != types.FieldVal {
						path = path[:len(path)-1]
					}
					key += fmt.Sprintf("|selection %d %v %t", selection.Kind(), path, selection.Indirect())
				}
				byFile[filename] = append(byFile[filename], identifierRecord{Name: identifier.Name, Key: key})
				offsetsByFile[filename] = append(
					offsetsByFile[filename],
					rootsPackage.Fset.PositionFor(identifier.Pos(), false).Offset,
				)
				return true
			})
		}
		fingerprints[rootsPackage.ID] = byFile
		offsets[rootsPackage.ID] = offsetsByFile
	}
	return fingerprints, offsets
}

// ownerOf is a method's receiver type without its package, and a dot, so two methods of one name in one file
// stay apart; empty for anything else.
func ownerOf(object types.Object) string {
	function, isFunction := object.(*types.Func)
	if !isFunction || function.Signature().Recv() == nil {
		return ""
	}
	return types.TypeString(function.Signature().Recv().Type(), func(*types.Package) string { return "" }) + "."
}

// Apply rewrites the files the result's edits touch, proves the result before writing any of it, and writes only
// a proven result, the shape of workspace.ApplyVerified with this engine's cross-package fingerprint. It
// fingerprints every root package with each edited identifier's expected new name, builds every rewritten file in
// memory, reloads the whole workspace with those files as an overlay, and requires every identifier to resolve to
// the same declaration as before under its expected name. Only when the reload has no package errors and no
// identifier differs does it write the files, so a plan that would mismatch leaves the tree as it found it. It
// returns the number of mismatches, or an error when rewriting, reloading or writing fails. A plan that read files
// under other platforms is proven the same way under each of them too, before anything is written.
func Apply(
	loaded *workspace.Workspace,
	root string,
	skips []string,
	result *Result,
	output *bufio.Writer,
) (int, error) {
	newNameAt := map[rename.EditKey]string{}
	for _, edit := range append(append([]rename.Edit(nil), result.Edits...), result.TwinEdits...) {
		newNameAt[rename.EditKey{Filename: edit.Filename, Offset: edit.Offset}] = edit.NewText
	}
	before := expectedFingerprint(loaded, newNameAt)

	all := append(
		append(append(append([]rename.Edit(nil), result.Edits...), result.TagEdits...), result.CommentEdits...),
		result.TwinEdits...,
	)
	edits, disagreements := rename.Deduplicate(all)
	if len(disagreements) > 0 {
		return 0, fmt.Errorf("edits disagree, so nothing was written: %v", disagreements)
	}
	byFile := rename.GroupByFile(edits)
	filenames := make([]string, 0, len(byFile))
	for filename := range byFile {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	overlay := map[string][]byte{}
	for _, filename := range filenames {
		original, rewritten, rewriteError := rename.RewriteFile(filename, byFile[filename])
		if rewriteError != nil {
			return 0, fmt.Errorf("%w (nothing written)", rewriteError)
		}
		if string(original) != string(rewritten) {
			overlay[filename] = rewritten
		}
	}

	reloaded, reloadError := workspace.LoadWithOverlay(root, skips, overlay)
	if reloadError != nil {
		return 0, fmt.Errorf("the rewritten tree does not load cleanly, so nothing was written: %w", reloadError)
	}
	fmt.Fprintf(
		output,
		"reloaded with the rewrite of %d files in memory: %d root packages, zero package errors\n",
		len(overlay),
		len(reloaded.Roots),
	)
	after, _ := Fingerprint(reloaded)
	compared, mismatches := compareFingerprints(before, after, output)
	fmt.Fprintf(
		output,
		"re-verification: %d identifiers compared across %d packages, %d mismatches\n",
		compared,
		len(reloaded.Roots),
		mismatches,
	)
	for _, platform := range result.platforms {
		platformMismatches, platformError := verifyPlatform(loaded, platform, overlay, newNameAt, output)
		if platformError != nil {
			return 0, fmt.Errorf("%w (nothing written)", platformError)
		}
		mismatches += platformMismatches
	}
	if mismatches > 0 {
		fmt.Fprintf(output, "%d mismatches, so nothing was written\n", mismatches)
		return mismatches, nil
	}

	written := 0
	for _, filename := range filenames {
		contents, changed := overlay[filename]
		if !changed {
			continue
		}
		information, statError := os.Stat(filename)
		if statError != nil {
			return 0, fmt.Errorf("%w (%d of %d proven files written)", statError, written, len(overlay))
		}
		if writeError := os.WriteFile(filename, contents, information.Mode()); writeError != nil {
			return 0, fmt.Errorf("%w (%d of %d proven files written)", writeError, written, len(overlay))
		}
		written++
	}
	fmt.Fprintf(output, "wrote %d files, each proven before writing\n", written)
	return 0, nil
}

// expectedFingerprint is a load's fingerprint with each identifier an edit renames given its new name: what the
// rewrite must read back.
func expectedFingerprint(
	loaded *workspace.Workspace,
	newNameAt map[rename.EditKey]string,
) map[string]map[string][]identifierRecord {
	expected, offsets := Fingerprint(loaded)
	for id, byFile := range expected {
		for filename, records := range byFile {
			for index := range records {
				if newName, renamed := newNameAt[rename.EditKey{
					Filename: filename,
					Offset:   offsets[id][filename][index],
				}]; renamed {
					records[index].Name = newName
				}
			}
		}
	}
	return expected
}

// verifyPlatform proves the rewrite under one platform: it loads the tree as the platform builds it, before and
// with the rewrite in memory, and requires every package that type-checked before to type-check after, every
// identifier in it to resolve to the same declaration, under its expected name. It returns the mismatches.
func verifyPlatform(
	loaded *workspace.Workspace,
	platform workspace.Platform,
	overlay map[string][]byte,
	newNameAt map[rename.EditKey]string,
	output *bufio.Writer,
) (int, error) {
	before, beforeError := platformExpectation(loaded, platform, newNameAt)
	if beforeError != nil {
		return 0, beforeError
	}
	afterLoad, afterError := loaded.LoadPlatform(platform, overlay)
	if afterError != nil {
		return 0, afterError
	}
	mismatches := 0
	for id := range before {
		if reason, failed := afterLoad.Failed[id]; failed {
			fmt.Fprintf(
				output,
				"mismatch: package %s type-checks as %s before the rewrite and not after: %s\n",
				id,
				platform,
				reason,
			)
			mismatches++
			delete(before, id)
		}
	}
	after, _ := Fingerprint(afterLoad.Clean)
	compared, compareMismatches := compareFingerprints(before, after, output)
	mismatches += compareMismatches
	fmt.Fprintf(
		output,
		"re-verification as %s: %d identifiers compared across %d packages, %d mismatches\n",
		platform,
		compared,
		len(afterLoad.Clean.Roots),
		mismatches,
	)
	return mismatches, nil
}

// platformExpectation is the expected fingerprint of the tree as platform builds it, from a load of its own that
// is let go before the rewrite is loaded: the loads are several gigabytes each.
func platformExpectation(
	loaded *workspace.Workspace,
	platform workspace.Platform,
	newNameAt map[rename.EditKey]string,
) (map[string]map[string][]identifierRecord, error) {
	load, loadError := loaded.LoadPlatform(platform, nil)
	if loadError != nil {
		return nil, loadError
	}
	return expectedFingerprint(load.Clean, newNameAt), nil
}

// compareFingerprints counts every identifier compared and every one that differs, printing each difference.
func compareFingerprints(before, after map[string]map[string][]identifierRecord, output *bufio.Writer) (int, int) {
	mismatches := 0
	compared := 0
	ids := make([]string, 0, len(before))
	for id := range before {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		expectedFiles := before[id]
		actualFiles, found := after[id]
		if !found {
			fmt.Fprintf(output, "mismatch: package %s is gone after the rewrite\n", id)
			mismatches++
			continue
		}
		expectedFilenames := make([]string, 0, len(expectedFiles))
		for filename := range expectedFiles {
			expectedFilenames = append(expectedFilenames, filename)
		}
		sort.Strings(expectedFilenames)
		for _, filename := range expectedFilenames {
			expected, actual := expectedFiles[filename], actualFiles[filename]
			if len(actual) != len(expected) {
				fmt.Fprintf(
					output,
					"mismatch: %s in %s: %d identifiers before, %d after\n",
					filename,
					id,
					len(expected),
					len(actual),
				)
				mismatches++
				continue
			}
			for index := range expected {
				compared++
				if expected[index] != actual[index] {
					fmt.Fprintf(
						output,
						"mismatch: %s in %s, identifier %d: expected %+v, got %+v\n",
						filename,
						id,
						index,
						expected[index],
						actual[index],
					)
					mismatches++
				}
			}
		}
		if len(actualFiles) != len(expectedFiles) {
			fmt.Fprintf(
				output,
				"mismatch: %s has %d files before, %d after\n",
				id,
				len(expectedFiles),
				len(actualFiles),
			)
			mismatches++
		}
	}
	for id := range after {
		if _, found := before[id]; !found {
			fmt.Fprintf(output, "mismatch: package %s is new after the rewrite\n", id)
			mismatches++
		}
	}
	return compared, mismatches
}
