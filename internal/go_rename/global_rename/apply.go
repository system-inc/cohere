package global_rename

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// Fingerprint records, for every root package and each of its files rename.FileKey keeps under the root, every
// identifier in source order and the declaration it defines and uses, and for a selector the path go/types took to
// the member. A file is keyed as rename.FileKey keys it, so cgo's translation of a source file, whose name in the
// build cache is a hash a rewrite changes, is keyed by its source. A declaration in a root file is keyed by the
// ordinal of its declaring identifier in that file, which a rename cannot move, so a use in one package of a
// declaration in another compares across a rename. One in a file FileKey leaves out, generated outside the root
// with no source under it, as cgo's _cgo_gotypes.go, is keyed by package path, kind, owner and name: no rename
// edits such a file. Anything else by package path, kind, owner, name and file, which no rename here changes. Every
// record of one key holds one string: a whole tree's fingerprint spells a declaration's key once per identifier
// naming it, about 2.5 million records on cohere, so each key is kept once per call and shared.
func Fingerprint(
	loaded *workspace.Workspace,
) (map[string]map[string][]rename.IdentifierRecord, map[string]map[string][]int) {
	type position struct {
		filename     string
		line, column int
	}
	ordinals := map[position]string{}
	seenFiles := map[string]bool{}
	// leftOut are the files FileKey keeps no key for, by the name they were read under.
	leftOut := map[string]bool{}
	for _, rootsPackage := range loaded.Roots {
		for _, file := range rootsPackage.Syntax {
			read := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			if seenFiles[read] {
				continue
			}
			seenFiles[read] = true
			filename, kept := rename.FileKey(rootsPackage.Fset, file, loaded.Root, rootsPackage.GoFiles)
			if !kept {
				leftOut[read] = true
				continue
			}
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
			if leftOut[where.Filename] {
				return fmt.Sprintf("generated:%s:%s:%s%s", declared.Pkg().Path(), kindOf(declared), ownerOf(declared),
					declared.Name())
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
	// interned holds each key once, so every record of one key shares its string.
	interned := map[string]string{}
	intern := func(key string) string {
		if held, found := interned[key]; found {
			return held
		}
		interned[key] = key
		return key
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
			filename, kept := rename.FileKey(rootsPackage.Fset, file, loaded.Root, rootsPackage.GoFiles)
			if !kept {
				continue
			}
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
				byFile[filename] = append(byFile[filename], rename.IdentifierRecord{
					Name: identifier.Name,
					Key:  intern(key),
				})
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
	// platformDifferences are the plan's, each platform's load before the rewrite fingerprinted while the plan read
	// it and kept as its difference from the host's, which ProvePlatform reads with Fingerprints in place of loading
	// the platform again. Only an expectation of the very workspace the plan read carries them (Expect).
	platformDifferences map[workspace.Platform]platformDifference
}

// unrenamedFingerprint is a load's Fingerprint and offsets as the load reads, before any rename is applied to it.
type unrenamedFingerprint struct {
	fingerprints map[string]map[string][]rename.IdentifierRecord
	offsets      map[string]map[string][]int
}

// platformDifference is a platform's unrenamedFingerprint as it differs from the host's, which a plan keeps per
// platform it read, so a proof there need not keep, or make again, a load of several gigabytes only to fingerprint
// it. Most of a platform's files read exactly as the host's (96 to 99% of each on cohere), and a whole fingerprint
// is about 480 MB there, so a file whose records and offsets are the host's, under the same package ID and file key,
// is only marked, and the rest are kept whole. Every package ID and file key of the platform is in one of the two,
// so a file the platform lacks is never taken from the host.
type platformDifference struct {
	// sameAsHost holds, per package ID, each file whose records and offsets equal the host's, element by element.
	sameAsHost map[string]map[string]bool
	// fingerprints and offsets hold, per package ID, every other file's records and offsets as the platform reads
	// them, and every package ID of the platform, with no files when all of them are marked.
	fingerprints map[string]map[string][]rename.IdentifierRecord
	offsets      map[string]map[string][]int
}

// differenceFrom is platform as it differs from host (platformDifference). Offsets are compared beside records, so a
// marked file's renames, made by offset, are the host's: a file keyed by its source may be cgo's translation in one
// load and the source itself in the other.
func differenceFrom(host unrenamedFingerprint, platform unrenamedFingerprint) platformDifference {
	difference := platformDifference{
		sameAsHost:   map[string]map[string]bool{},
		fingerprints: make(map[string]map[string][]rename.IdentifierRecord, len(platform.fingerprints)),
		offsets:      make(map[string]map[string][]int, len(platform.fingerprints)),
	}
	for id, byFile := range platform.fingerprints {
		kept := map[string][]rename.IdentifierRecord{}
		keptOffsets := map[string][]int{}
		for filename, records := range byFile {
			hostRecords, hostHas := host.fingerprints[id][filename]
			if hostHas && slices.Equal(records, hostRecords) &&
				slices.Equal(platform.offsets[id][filename], host.offsets[id][filename]) {
				if difference.sameAsHost[id] == nil {
					difference.sameAsHost[id] = map[string]bool{}
				}
				difference.sameAsHost[id][filename] = true
				continue
			}
			kept[filename] = records
			keptOffsets[filename] = platform.offsets[id][filename]
		}
		difference.fingerprints[id] = kept
		difference.offsets[id] = keptOffsets
	}
	return difference
}

// Verification is a load of the rewrite read against an Expectation: how many identifiers were compared, and every
// place the load reads back other than expected, in the order found.
type Verification struct {
	Compared   int
	Mismatches []Mismatch
	// Packages counts the root packages of the load compared, and Files holds every file they type-checked, by
	// absolute path, which only a platform's proof sets: a file no proof holds was proven by none.
	Packages int
	Files    map[string]bool
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
		platformMismatches, platformError := verifyPlatform(loaded, platform, files, expectation, output)
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
// rewritten: its fingerprint, with each identifier an edit or a twin's edit renames given its new name. Only when
// loaded is the very workspace the plan read does it carry the plan's platform differences, which are differences
// from that workspace's fingerprint, and take that fingerprint from the plan rather than make it again; over any
// other workspace, as a codemod that plans over one load and applies over a reload gives it, a platform's proof
// loads its before side fresh.
//
// Mutates: result the host fingerprint the plan kept, handed to the expectation and released from result
func Expect(loaded *workspace.Workspace, result *Result) *Expectation {
	newNameAt := map[rename.EditKey]string{}
	for _, edit := range append(append([]rename.Edit(nil), result.Edits...), result.TwinEdits...) {
		newNameAt[rename.EditKey{Filename: edit.Filename, Offset: edit.Offset}] = edit.NewText
	}
	if loaded != result.planned {
		return expectedFingerprint(loaded, newNameAt)
	}
	var expectation *Expectation
	if result.hostFingerprint != nil {
		expectation = renamedExpectation(*result.hostFingerprint, newNameAt)
		result.hostFingerprint = nil
	} else {
		expectation = expectedFingerprint(loaded, newNameAt)
	}
	expectation.platformDifferences = result.platformDifferences
	return expectation
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
// rewrite must read back. The fingerprint is its own, so it is renamed in place.
func expectedFingerprint(loaded *workspace.Workspace, newNameAt map[rename.EditKey]string) *Expectation {
	return renamedExpectation(fingerprintBefore(loaded), newNameAt)
}

// renamedExpectation is an unrenamed fingerprint with each identifier an edit renames given its new name, renamed in
// place: the fingerprint is the expectation's from then on.
func renamedExpectation(unrenamed unrenamedFingerprint, newNameAt map[rename.EditKey]string) *Expectation {
	renameRecords(unrenamed.fingerprints, unrenamed.offsets, newNameAt)
	return &Expectation{Fingerprints: unrenamed.fingerprints, Offsets: unrenamed.offsets, newNameAt: newNameAt}
}

// fingerprintBefore is a load's fingerprint before any rename, for a plan to keep for a platform's proof.
func fingerprintBefore(loaded *workspace.Workspace) unrenamedFingerprint {
	fingerprints, offsets := Fingerprint(loaded)
	return unrenamedFingerprint{fingerprints: fingerprints, offsets: offsets}
}

// expectedFrom is what the rewrite must read back under a platform given its difference from the host and host, the
// host's expectation of the same rewrite: a marked file's records are the host's already renamed, since the same
// records at the same offsets take the same renames, and every kept file's are a copy of the platform's with each
// identifier an edit renames given its new name. The copy keeps the difference as it was, since one serves every
// proof of a round, each with its own renames. The maps are the expectation's own, since a proof drops the packages
// that fail from what it is given, and the slices are shared: nothing writes records once renamed, or offsets.
func expectedFrom(difference platformDifference, host *Expectation) *Expectation {
	expected := make(map[string]map[string][]rename.IdentifierRecord, len(difference.fingerprints))
	offsets := make(map[string]map[string][]int, len(difference.fingerprints))
	for id, byFile := range difference.fingerprints {
		copied := make(map[string][]rename.IdentifierRecord, len(byFile)+len(difference.sameAsHost[id]))
		copiedOffsets := make(map[string][]int, len(byFile)+len(difference.sameAsHost[id]))
		for filename, records := range byFile {
			copied[filename] = slices.Clone(records)
			copiedOffsets[filename] = difference.offsets[id][filename]
		}
		expected[id] = copied
		offsets[id] = copiedOffsets
	}
	renameRecords(expected, offsets, host.newNameAt)
	for id, sameAsHost := range difference.sameAsHost {
		for filename := range sameAsHost {
			expected[id][filename] = host.Fingerprints[id][filename]
			offsets[id][filename] = host.Offsets[id][filename]
		}
	}
	return &Expectation{Fingerprints: expected, Offsets: offsets, newNameAt: host.newNameAt}
}

// renameRecords gives each record of fingerprints an edit renames, by its file and offset, its new name, in place.
func renameRecords(
	fingerprints map[string]map[string][]rename.IdentifierRecord,
	offsets map[string]map[string][]int,
	newNameAt map[rename.EditKey]string,
) {
	for id, byFile := range fingerprints {
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
}

// verifyPlatform proves the rewrite under one platform (provePlatform) through workspace's own platform load, and
// reports each mismatch and the count. It returns the mismatches, or the error of a load that failed. It loads the
// platform before the rewrite itself rather than read the plan's fingerprint: Apply may be given a workspace other
// than the one the plan read, as a caller that reloads between the two does.
func verifyPlatform(
	loaded *workspace.Workspace,
	platform workspace.Platform,
	overlay map[string][]byte,
	expectation *Expectation,
	output *bufio.Writer,
) (int, error) {
	verification, proveError := provePlatform(loaded.LoadPlatform, platform, overlay, expectation, nil)
	if proveError != nil {
		return 0, proveError
	}
	reportMismatches(verification.Mismatches, output)
	fmt.Fprintf(
		output,
		"re-verification as %s: %d identifiers compared across %d packages, %d mismatches\n",
		platform,
		verification.Compared,
		verification.Packages,
		len(verification.Mismatches),
	)
	return len(verification.Mismatches), nil
}

// checkDifference is nil, and a build with the rename_differential tag sets it (platform_differential.go) to check
// each proof's expectation from a stored difference against one from the platform's before side loaded fresh. It is
// a function a tagged file sets, never code the untagged build carries, so a proof there pays one nil check.
var checkDifference func(
	loadPlatform func(platform workspace.Platform, overlay map[string][]byte) (*workspace.PlatformLoad, error),
	platform workspace.Platform,
	difference platformDifference,
	fromDifference *Expectation,
)

// provePlatform proves the rewrite under one platform: it loads the tree as the platform builds it, before and with
// the rewrite in memory, and requires every package that type-checked before to type-check after, and every
// identifier in it to resolve to the same declaration, under its expected name. Nothing reads the load before once
// its fingerprint is taken, so it is collected before the rewrite loads: the loads are several gigabytes each. Given
// stored, the plan's difference of that same load from the host's, read with host, the host's expectation, it loads
// only the rewrite. A package that fails after is a PlatformPackage mismatch, by ID, and every mismatch names the
// platform.
func provePlatform(
	loadPlatform func(platform workspace.Platform, overlay map[string][]byte) (*workspace.PlatformLoad, error),
	platform workspace.Platform,
	overlay map[string][]byte,
	host *Expectation,
	stored *platformDifference,
) (Verification, error) {
	var before *Expectation
	if stored != nil {
		before = expectedFrom(*stored, host)
		if checkDifference != nil {
			checkDifference(loadPlatform, platform, *stored, before)
		}
	} else {
		beforeLoad, beforeError := loadPlatform(platform, nil)
		if beforeError != nil {
			return Verification{}, beforeError
		}
		before = expectedFingerprint(beforeLoad.Clean, host.newNameAt)
	}
	afterLoad, afterError := loadPlatform(platform, overlay)
	if afterError != nil {
		return Verification{}, afterError
	}
	var verification Verification
	for _, id := range slices.Sorted(maps.Keys(before.Fingerprints)) {
		if reason, failed := afterLoad.Failed[id]; failed {
			verification.Mismatches = append(verification.Mismatches, Mismatch{
				Kind: MismatchKindPlatformPackage, Package: id, Platform: platform.String(), Reason: reason,
			})
			delete(before.Fingerprints, id)
		}
	}
	after, _ := Fingerprint(afterLoad.Clean)
	compared := compareFingerprints(before, after)
	for index := range compared.Mismatches {
		compared.Mismatches[index].Platform = platform.String()
	}
	verification.Compared = compared.Compared
	verification.Mismatches = append(verification.Mismatches, compared.Mismatches...)
	verification.Packages = len(afterLoad.Clean.Roots)
	verification.Files = map[string]bool{}
	for _, rootsPackage := range afterLoad.Clean.Roots {
		for _, file := range rootsPackage.Syntax {
			verification.Files[filepath.Clean(rootsPackage.Fset.PositionFor(file.Pos(), false).Filename)] = true
		}
	}
	return verification, nil
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
