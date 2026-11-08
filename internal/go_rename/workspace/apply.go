package workspace

import (
	"bufio"
	"fmt"
	"os"
	"sort"

	"github.com/system-inc/cohere/internal/go_rename/rename"
)

// ApplyVerified rewrites the files a pass's edits touch, proves the result before writing any of it, and
// writes only a proven result. It fingerprints every root package now, with each edited identifier's
// expected new name; builds every rewritten file in memory; reloads the whole workspace with those files as
// an overlay; and compares every identifier's name and the declaration it resolves to. Only when the reload
// has no package errors and no identifier differs does it write the files. A pass that would mismatch
// leaves the tree exactly as it found it. It returns the exit code: 0 written and proven, 1 refused. Its report
// goes to output and its refusals to errorOutput, both buffered and the caller's to flush, so a write here cannot
// fail partway and the caller's one Flush says whether the report arrived.
func ApplyVerified(
	loaded *Workspace,
	root string,
	skips []string,
	edits []rename.Edit,
	editsByFile map[string][]rename.Edit,
	passName string,
	output *bufio.Writer,
	errorOutput *bufio.Writer,
) int {
	return ApplyVerifiedRetargeted(loaded, root, skips, edits, editsByFile, nil, nil, passName, output, errorOutput)
}

// ApplyVerifiedRetargeted is ApplyVerified for a pass that also gives some identifiers a new declaration, as
// rename.Expect reads retargets and insertions: each of them must resolve, after the rewrite, to the local
// declared by the identifier at the offset it maps to, or by the declaration inserted there. The insertions'
// text is among the edits; insertions only say what it declares.
func ApplyVerifiedRetargeted(
	loaded *Workspace,
	root string,
	skips []string,
	edits []rename.Edit,
	editsByFile map[string][]rename.Edit,
	retargets map[rename.EditKey]int,
	insertions []rename.Insertion,
	passName string,
	output *bufio.Writer,
	errorOutput *bufio.Writer,
) int {
	return applyVerified(
		loaded,
		root,
		skips,
		edits,
		editsByFile,
		retargets,
		insertions,
		nil,
		passName,
		output,
		errorOutput,
	)
}

// Verify is a pass's own proof of its rewrite, beyond what identifiers resolve to: given the reload with the rewrite
// in memory, it prints what it compared to output and returns how many mismatches it found, each enough to write
// nothing.
type Verify func(reloaded *Workspace, output *bufio.Writer) int

// ApplyVerifiedChecked is ApplyVerified for a pass whose edits change what no identifier records, as a literal's
// spelling: verify runs over the same reload once the identifiers compare, and a mismatch it finds writes nothing,
// as an identifier's would.
func ApplyVerifiedChecked(
	loaded *Workspace,
	root string,
	skips []string,
	edits []rename.Edit,
	editsByFile map[string][]rename.Edit,
	verify Verify,
	passName string,
	output *bufio.Writer,
	errorOutput *bufio.Writer,
) int {
	return applyVerified(loaded, root, skips, edits, editsByFile, nil, nil, verify, passName, output, errorOutput)
}

// applyVerified is ApplyVerifiedRetargeted with verify, when set, run over the reload as ApplyVerifiedChecked runs it.
func applyVerified(
	loaded *Workspace,
	root string,
	skips []string,
	edits []rename.Edit,
	editsByFile map[string][]rename.Edit,
	retargets map[rename.EditKey]int,
	insertions []rename.Insertion,
	verify Verify,
	passName string,
	output *bufio.Writer,
	errorOutput *bufio.Writer,
) int {
	newNameAt := map[rename.EditKey]string{}
	for _, edit := range edits {
		newNameAt[rename.EditKey{Filename: edit.Filename, Offset: edit.Offset}] = edit.NewText
	}
	before := map[string]map[string][]rename.IdentifierRecord{}
	for _, rootsPackage := range loaded.Roots {
		// Keyed under the root: a cgo package's translations in the build cache are named by a hash the rewrite
		// changes, and rename.FileKey keys each by the source it translates (see rename.FileKey).
		fingerprint := rename.Fingerprint(
			rootsPackage.Fset,
			rootsPackage.Types,
			rootsPackage.TypesInfo,
			rootsPackage.Syntax,
			loaded.Root,
			rootsPackage.GoFiles,
		)
		rename.Expect(
			fingerprint,
			rename.IdentifierOffsets(rootsPackage.Fset, rootsPackage.Syntax, loaded.Root, rootsPackage.GoFiles),
			newNameAt,
			retargets,
			insertions,
		)
		before[rootsPackage.ID] = fingerprint
	}

	overlay := map[string][]byte{}
	filenames := make([]string, 0, len(editsByFile))
	for filename := range editsByFile {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	for _, filename := range filenames {
		original, rewritten, rewriteError := rename.RewriteFile(filename, editsByFile[filename])
		if rewriteError != nil {
			fmt.Fprintf(errorOutput, "%s: %v (nothing written)\n", passName, rewriteError)
			return 1
		}
		if string(original) != string(rewritten) {
			overlay[filename] = rewritten
		}
	}

	reloaded, reloadError := LoadWithOverlay(root, skips, overlay)
	if reloadError != nil {
		fmt.Fprintf(
			errorOutput,
			"%s: the rewritten tree does not load cleanly, so nothing was written: %v\n",
			passName,
			reloadError,
		)
		return 1
	}
	fmt.Fprintf(
		output,
		"reloaded with the rewrite in memory: %d root packages, zero package errors\n",
		len(reloaded.Roots),
	)

	mismatches := 0
	compared := 0
	seen := map[string]bool{}
	for _, rootsPackage := range reloaded.Roots {
		seen[rootsPackage.ID] = true
		expected, found := before[rootsPackage.ID]
		if !found {
			fmt.Fprintf(output, "mismatch: package %s is new after the rewrite\n", rootsPackage.ID)
			mismatches++
			continue
		}
		after := rename.Fingerprint(rootsPackage.Fset, rootsPackage.Types, rootsPackage.TypesInfo, rootsPackage.Syntax,
			reloaded.Root, rootsPackage.GoFiles)
		expectedFiles := make([]string, 0, len(expected))
		for filename := range expected {
			expectedFiles = append(expectedFiles, filename)
		}
		sort.Strings(expectedFiles)
		for _, filename := range expectedFiles {
			expectedRecords, actualRecords := expected[filename], after[filename]
			if len(expectedRecords) != len(actualRecords) {
				fmt.Fprintf(output,
					"mismatch: %s in %s: %d identifiers before, %d after\n",
					filename,
					rootsPackage.ID,
					len(expectedRecords),
					len(actualRecords),
				)
				mismatches++
				continue
			}
			for index := range expectedRecords {
				compared++
				if expectedRecords[index] != actualRecords[index] {
					fmt.Fprintf(output,
						"mismatch: %s in %s, identifier %d: expected %+v, got %+v\n",
						filename,
						rootsPackage.ID,
						index,
						expectedRecords[index],
						actualRecords[index],
					)
					mismatches++
				}
			}
		}
		if len(after) != len(expected) {
			fmt.Fprintf(
				output,
				"mismatch: %s has %d files before, %d after\n",
				rootsPackage.ID,
				len(expected),
				len(after),
			)
			mismatches++
		}
	}
	for id := range before {
		if !seen[id] {
			fmt.Fprintf(output, "mismatch: package %s is gone after the rewrite\n", id)
			mismatches++
		}
	}
	fmt.Fprintf(output,
		"re-verification: %d identifiers compared across %d packages, %d mismatches\n",
		compared,
		len(reloaded.Roots),
		mismatches,
	)
	if verify != nil {
		mismatches += verify(reloaded, output)
	}
	if mismatches > 0 {
		fmt.Fprintf(errorOutput, "%s: %d mismatches, so nothing was written\n", passName, mismatches)
		return 1
	}

	written := 0
	for _, filename := range filenames {
		contents, changed := overlay[filename]
		if !changed {
			continue
		}
		information, statError := os.Stat(filename)
		if statError != nil {
			fmt.Fprintf(
				errorOutput,
				"%s: %v (%d of %d proven files written)\n",
				passName,
				statError,
				written,
				len(overlay),
			)
			return 1
		}
		if writeError := os.WriteFile(filename, contents, information.Mode()); writeError != nil {
			fmt.Fprintf(
				errorOutput,
				"%s: %v (%d of %d proven files written)\n",
				passName,
				writeError,
				written,
				len(overlay),
			)
			return 1
		}
		written++
	}
	fmt.Fprintf(output, "wrote %d files, each proven before writing\n", written)
	return 0
}
