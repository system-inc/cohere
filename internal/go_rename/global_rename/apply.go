package global_rename

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"sort"

	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// Fingerprint records, for every root package and each of its files, every identifier in source order and the
// declaration it defines and uses, and for a selector the path go/types took to the member. A declaration in a
// root file is keyed by the ordinal of its declaring identifier in that file, which a rename cannot move, so a
// use in one package of a declaration in another compares across a rename; anything else by package path,
// kind, owner, name and file, which no rename here changes.
func Fingerprint(
	loaded *workspace.Workspace,
) (map[string]map[string][]rename.IdentifierRecord, map[string]map[string][]int) {
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
			// moves a declaration outside the root files. The base name is the one the file's //line directives
			// give: a cgo package such as runtime/cgo is read from cgo's translation in the build cache by one load
			// and from its source by another, and the translation names its source file in those directives.
			return fmt.Sprintf(
				"other:%s:%s:%s%s@%s",
				declared.Pkg().Path(),
				kindOf(declared),
				ownerOf(declared),
				declared.Name(),
				filepath.Base(loaded.Roots[0].Fset.PositionFor(declared.Pos(), true).Filename),
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
	fingerprints := map[string]map[string][]rename.IdentifierRecord{}
	offsets := map[string]map[string][]int{}
	for _, rootsPackage := range loaded.Roots {
		selections := map[*ast.Ident]*types.Selection{}
		for selector, selection := range rootsPackage.TypesInfo.Selections {
			selections[selector.Sel] = selection
		}
		byFile := map[string][]rename.IdentifierRecord{}
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
				byFile[filename] = append(byFile[filename], rename.IdentifierRecord{Name: identifier.Name, Key: key})
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

// Expectation is what a plan's rewrite must read back: every root package's fingerprint, by package ID and then
// file, with each identifier an edit renames given its new name.
type Expectation struct {
	Fingerprints map[string]map[string][]rename.IdentifierRecord
	// Offsets are, beside Fingerprints, each identifier's byte offset in its file before the rewrite, so a mismatch
	// names the edit made where the identifier stands.
	Offsets map[string]map[string][]int
	// newNameAt is each renamed identifier's new name by its file and offset, which a platform's expectation reads.
	newNameAt map[rename.EditKey]string
}

// Verification is a load of the rewrite read against an Expectation: how many identifiers were compared, and every
// place the load reads back other than expected, in the order found.
type Verification struct {
	Compared   int
	Mismatches []Mismatch
}

// Apply rewrites the files the result's edits touch, proves the result before writing any of it, and writes only
// a proven result, the shape of workspace.ApplyVerified with this engine's cross-package fingerprint. It
// fingerprints every root package with each edited identifier's expected new name, builds every rewritten file in
// memory, reloads the whole workspace with those files as an overlay, and requires every identifier to resolve to
// the same declaration as before under its expected name. Only when the reload has no package errors and no
// identifier differs does it write the files, so a plan that would mismatch leaves the tree as it found it. It
// returns the number of mismatches, or an error when rewriting, reloading or writing fails. A plan that read files
// under other platforms is proven the same way under each of them too, before anything is written. Its steps are
// Expect, RewrittenFiles, Compare and Write, which a caller that loads the rewrite its own way runs one by one.
func Apply(
	loaded *workspace.Workspace,
	root string,
	skips []string,
	result *Result,
	output *bufio.Writer,
) (int, error) {
	expectation := Expect(loaded, result)
	files, rewriteError := RewrittenFiles(result)
	if rewriteError != nil {
		return 0, rewriteError
	}

	reloaded, reloadError := workspace.LoadWithOverlay(root, skips, files)
	if reloadError != nil {
		return 0, fmt.Errorf("the rewritten tree does not load cleanly, so nothing was written: %w", reloadError)
	}
	fmt.Fprintf(
		output,
		"reloaded with the rewrite of %d files in memory: %d root packages, zero package errors\n",
		len(files),
		len(reloaded.Roots),
	)
	verification := Compare(expectation, reloaded)
	reportMismatches(verification.Mismatches, output)
	fmt.Fprintf(
		output,
		"re-verification: %d identifiers compared across %d packages, %d mismatches\n",
		verification.Compared,
		len(reloaded.Roots),
		len(verification.Mismatches),
	)
	mismatches := len(verification.Mismatches)
	for _, platform := range result.platforms {
		platformMismatches, platformError := verifyPlatform(loaded, platform, files, expectation.newNameAt, output)
		if platformError != nil {
			return 0, fmt.Errorf("%w (nothing written)", platformError)
		}
		mismatches += platformMismatches
	}
	if mismatches > 0 {
		fmt.Fprintf(output, "%d mismatches, so nothing was written\n", mismatches)
		return mismatches, nil
	}

	written, writeError := Write(files)
	if writeError != nil {
		return 0, writeError
	}
	fmt.Fprintf(output, "wrote %d files, each proven before writing\n", written)
	return 0, nil
}

// Expect is what the plan's rewrite must read back, taken from the load the plan was made over before anything is
// rewritten: its fingerprint, with each identifier an edit or a twin's edit renames given its new name.
func Expect(loaded *workspace.Workspace, result *Result) *Expectation {
	newNameAt := map[rename.EditKey]string{}
	for _, edit := range append(append([]rename.Edit(nil), result.Edits...), result.TwinEdits...) {
		newNameAt[rename.EditKey{Filename: edit.Filename, Offset: edit.Offset}] = edit.NewText
	}
	return expectedFingerprint(loaded, newNameAt)
}

// RewrittenFiles is every file the plan's edits change, by its absolute path, rewritten in memory and formatted,
// the edits of several variants deduplicated. A file the edits leave as it was is not among them. It reads the
// files and writes none; two edits of one span that disagree, or an edit whose old text its file does not hold, is
// an error.
func RewrittenFiles(result *Result) (map[string][]byte, error) {
	return RewrittenFilesOver(result, nil)
}

// RewrittenFilesOver is RewrittenFiles for a plan made over a load that read some files from current, by absolute
// path, in place of the disk, as a load of a rewrite still in memory does: each such file is rewritten from its bytes
// there, and every other file from disk.
func RewrittenFilesOver(result *Result, current map[string][]byte) (map[string][]byte, error) {
	all := append(
		append(append(append([]rename.Edit(nil), result.Edits...), result.TagEdits...), result.CommentEdits...),
		result.TwinEdits...,
	)
	edits, disagreements := rename.Deduplicate(all)
	if len(disagreements) > 0 {
		return nil, fmt.Errorf("edits disagree, so nothing was written: %v", disagreements)
	}
	byFile := rename.GroupByFile(edits)
	filenames := make([]string, 0, len(byFile))
	for filename := range byFile {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	files := map[string][]byte{}
	for _, filename := range filenames {
		original, held := current[filename]
		var rewritten []byte
		var rewriteError error
		if held {
			rewritten, rewriteError = rename.RewriteSource(filename, original, byFile[filename])
		} else {
			original, rewritten, rewriteError = rename.RewriteFile(filename, byFile[filename])
		}
		if rewriteError != nil {
			return nil, fmt.Errorf("%w (nothing written)", rewriteError)
		}
		if string(original) != string(rewritten) {
			files[filename] = rewritten
		}
	}
	return files, nil
}

// Compare reads reloaded, a load of the tree with the rewrite in place, against expectation, and returns every
// identifier compared and every mismatch. It loads nothing, so a caller that loads the rewrite its own way, as
// the Go engine does with its overlay, proves that load the same way Apply proves its reload. Whether the load
// type-checks is the caller's to require first, as LoadWithOverlay does.
func Compare(expectation *Expectation, reloaded *workspace.Workspace) Verification {
	after, _ := Fingerprint(reloaded)
	return compareFingerprints(expectation, after)
}

// Write writes each proven file over the one on disk, keeping its mode, in the order of their names, and returns
// how many it wrote. A failure says how many of them were written before it.
func Write(files map[string][]byte) (int, error) {
	filenames := make([]string, 0, len(files))
	for filename := range files {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	written := 0
	for _, filename := range filenames {
		information, statError := os.Stat(filename)
		if statError != nil {
			return written, fmt.Errorf("%w (%d of %d proven files written)", statError, written, len(files))
		}
		if writeError := os.WriteFile(filename, files[filename], information.Mode()); writeError != nil {
			return written, fmt.Errorf("%w (%d of %d proven files written)", writeError, written, len(files))
		}
		written++
	}
	return written, nil
}

// expectedFingerprint is a load's fingerprint with each identifier an edit renames given its new name: what the
// rewrite must read back.
func expectedFingerprint(loaded *workspace.Workspace, newNameAt map[rename.EditKey]string) *Expectation {
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
	return &Expectation{Fingerprints: expected, Offsets: offsets, newNameAt: newNameAt}
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
	for id := range before.Fingerprints {
		if reason, failed := afterLoad.Failed[id]; failed {
			fmt.Fprintf(
				output,
				"mismatch: package %s type-checks as %s before the rewrite and not after: %s\n",
				id,
				platform,
				reason,
			)
			mismatches++
			delete(before.Fingerprints, id)
		}
	}
	after, _ := Fingerprint(afterLoad.Clean)
	verification := compareFingerprints(before, after)
	reportMismatches(verification.Mismatches, output)
	mismatches += len(verification.Mismatches)
	fmt.Fprintf(
		output,
		"re-verification as %s: %d identifiers compared across %d packages, %d mismatches\n",
		platform,
		verification.Compared,
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
) (*Expectation, error) {
	load, loadError := loaded.LoadPlatform(platform, nil)
	if loadError != nil {
		return nil, loadError
	}
	return expectedFingerprint(load.Clean, newNameAt), nil
}

// compareFingerprints compares every identifier the expectation holds with after, and returns how many it compared
// and every one that differs, in the order found.
func compareFingerprints(
	expectation *Expectation,
	after map[string]map[string][]rename.IdentifierRecord,
) Verification {
	var verification Verification
	ids := make([]string, 0, len(expectation.Fingerprints))
	for id := range expectation.Fingerprints {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		expectedFiles := expectation.Fingerprints[id]
		actualFiles, found := after[id]
		if !found {
			verification.Mismatches = append(
				verification.Mismatches,
				Mismatch{Kind: MismatchKindPackageGone, Package: id},
			)
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
				verification.Mismatches = append(verification.Mismatches, Mismatch{
					Kind:          MismatchKindIdentifierCount,
					Package:       id,
					Filename:      filename,
					ExpectedCount: len(expected),
					ActualCount:   len(actual),
				})
				continue
			}
			for index := range expected {
				verification.Compared++
				if expected[index] != actual[index] {
					verification.Mismatches = append(verification.Mismatches, Mismatch{
						Kind:     MismatchKindIdentifier,
						Package:  id,
						Filename: filename,
						Index:    index,
						Offset:   expectation.Offsets[id][filename][index],
						Expected: expected[index],
						Actual:   actual[index],
					})
				}
			}
		}
		if len(actualFiles) != len(expectedFiles) {
			verification.Mismatches = append(verification.Mismatches, Mismatch{
				Kind:          MismatchKindFileCount,
				Package:       id,
				ExpectedCount: len(expectedFiles),
				ActualCount:   len(actualFiles),
			})
		}
	}
	for id := range after {
		if _, found := expectation.Fingerprints[id]; !found {
			verification.Mismatches = append(
				verification.Mismatches,
				Mismatch{Kind: MismatchKindPackageNew, Package: id},
			)
		}
	}
	return verification
}

// reportMismatches prints each mismatch on a line of its own.
func reportMismatches(mismatches []Mismatch, output *bufio.Writer) {
	for _, mismatch := range mismatches {
		fmt.Fprintf(output, "mismatch: %s\n", mismatch)
	}
}
