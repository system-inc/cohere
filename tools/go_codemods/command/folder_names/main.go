// Command foldernames moves package folders whose names run words together to folders that separate them with
// underscores, as the -map file says: each line names a folder relative to the root, the folder it becomes, and the
// name its package clause takes there, tab-separated. A folder nested in a moved one moves with it, and its own
// line, if it has one, renames it inside the new parent.
//
// It loads every package of the root's go.work with its tests, and every platform whose own files a move touches,
// and plans every change with types: each moved folder's package clause (an external test package's `_test` kept)
// and package doc, every import path naming a moved folder or one under it, and every qualifier of an import whose
// package takes a new name. An alias spelling just the old name is dropped; any other alias stays. A local
// variable that would capture a qualifier under its new name is renamed through the rename engine, to the new
// name's words in camel case, never aliased around; anything else in the way is refused. Files no load reads, such
// as testdata fixtures, are read without types.
//
// Nothing is written until the result is proven: the rewritten files, and every file of the moved folders, are
// laid over their new paths in memory, the whole workspace is reloaded, and every identifier must resolve to the
// same declaration as before, with the moved paths and the new names translated, and every file must import the
// same packages. Then the folders move with `git mv`, so history follows, the rewritten files are written, and the
// tree is loaded from disk and compared once more.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/tools/go_codemods/rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code foldernames exits with: runCommandLine returns its exit code here, and
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

// runCommandLine reads the flags and runs foldernames, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "plan and prove the moves in memory, write and move nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	mappingPath := flag.String(
		"map",
		"",
		"the moves: old folder, new folder, new package name, tab-separated, one per line",
	)
	flag.Parse()
	if *mappingPath == "" {
		fmt.Fprintln(streams.Errors, "foldernames: -map is required")
		return 2
	}
	moves, readError := readMoves(*mappingPath)
	if readError != nil {
		fmt.Fprintf(streams.Errors, "foldernames: %v\n", readError)
		return 1
	}
	return run(*root, moves, *dryRun, streams.Output)
}

// run plans the moves, proves them and, unless dryRun, makes them, writing its report and its errors to output,
// and returns the code foldernames exits with. main hands it streams.Output, a test a writer of its own. The report
// goes through buffered, whose error is sticky, so the one Flush here says whether all of it arrived, and a report
// that did not arrive is a run that did not finish. A failure of the streams is said where streams.Finish runs.
func run(root string, moves []folderMove, dryRun bool, output io.Writer) int {
	bufferedOutput := buffered(output)
	exitCode := moveFolders(root, moves, dryRun, bufferedOutput)
	if flushError := bufferedOutput.Flush(); flushError != nil {
		return max(exitCode, 1)
	}
	return exitCode
}

// buffered is writer itself when it already buffers, as streams.Output does, so a Show of it reaches the
// terminal, and a new buffer over writer otherwise.
func buffered(writer io.Writer) *bufio.Writer {
	if stream, isBuffered := writer.(*bufio.Writer); isBuffered {
		return stream
	}
	return bufio.NewWriter(writer)
}

// moveFolders is run with its report buffered.
func moveFolders(root string, moves []folderMove, dryRun bool, output *bufio.Writer) int {
	loaded, loadError := workspace.Load(root, nil)
	if loadError != nil {
		fmt.Fprintf(output, "foldernames: loading: %v\n", loadError)
		return 1
	}
	fmt.Fprintf(output, "loaded %d root packages\n", len(loaded.Roots))
	// The plan and its proof take a while on a whole worktree, so the load's count is seen before them.
	streams.Show(output)
	modulePath, moduleError := readModulePath(loaded.Root)
	if moduleError != nil {
		fmt.Fprintf(output, "foldernames: %v\n", moduleError)
		return 1
	}
	mapping, mappingError := newMapping(modulePath, moves)
	if mappingError != nil {
		fmt.Fprintf(output, "foldernames: %v\n", mappingError)
		return 1
	}
	for _, move := range mapping.moves {
		if information, statError := os.Stat(
			filepath.Join(loaded.Root, move.OldFolder),
		); statError != nil || !information.IsDir() {
			fmt.Fprintf(output, "foldernames: %s is not a folder\n", move.OldFolder)
			return 1
		}
		if _, statError := os.Stat(filepath.Join(loaded.Root, move.NewFolder)); statError == nil {
			fmt.Fprintf(output, "foldernames: %s already exists\n", move.NewFolder)
			return 1
		}
	}

	sources := fileSource{}
	plan := newPlan()
	typed := map[string]bool{}
	plan.planLoad(loaded, loaded.Roots, mapping, sources, typed, nil)
	platformLoads := plan.planPlatforms(loaded, mapping, sources, typed)
	if walkError := plan.planSyntacticFiles(loaded.Root, mapping, sources, typed); walkError != nil {
		fmt.Fprintf(output, "foldernames: %v\n", walkError)
		return 1
	}
	edits, disagreements := rename.Deduplicate(plan.edits)
	plan.refusals = append(plan.refusals, disagreements...)

	newPath := func(filename string) string {
		folder, inside := relativeFolderOf(loaded.Root, filename)
		if !inside {
			return filename
		}
		return filepath.Join(loaded.Root, filepath.FromSlash(mapping.NewFolder(folder)), filepath.Base(filename))
	}
	movedFiles, movedError := filesUnder(loaded.Root, mapping)
	if movedError != nil {
		fmt.Fprintf(output, "foldernames: %v\n", movedError)
		return 1
	}
	report(output, loaded.Root, mapping, plan, edits, movedFiles)
	if len(plan.refusals) > 0 {
		fmt.Fprintf(output, "foldernames: %d refusals, so nothing was moved or written\n", len(plan.refusals))
		return 1
	}

	overlay, rewritten, overlayError := buildOverlay(edits, movedFiles, newPath)
	if overlayError != nil {
		fmt.Fprintf(output, "foldernames: %v (nothing written)\n", overlayError)
		return 1
	}
	translate := translation{
		file:           newPath,
		packagePath:    mapping.NewPackagePath,
		names:          plan.names,
		renamedObjects: plan.renamedObjects,
	}
	expected := expect(loaded.Roots, translate, plan.droppedAliases, mapping)
	for index := range platformLoads {
		platformLoads[index].expected = expect(
			platformLoads[index].load.Clean.Roots,
			translate,
			plan.droppedAliases,
			mapping,
		)
		platformLoads[index].expected.files = platformLoads[index].files
	}

	if !prove(output, "in memory", loaded, mapping, overlay, expected, platformLoads) {
		fmt.Fprintln(output, "foldernames: the moves did not prove, so nothing was moved or written")
		return 1
	}
	if dryRun {
		fmt.Fprintln(output, "dry run: proven in memory, nothing moved or written")
		return 0
	}

	for _, move := range mapping.moves {
		source := path.Join(mapping.NewFolder(path.Dir(move.OldFolder)), path.Base(move.OldFolder))
		command := exec.Command("git", "-C", loaded.Root, "mv", source, move.NewFolder)
		if combined, moveError := command.CombinedOutput(); moveError != nil {
			fmt.Fprintf(output, "foldernames: git mv %s %s: %v: %s\n", source, move.NewFolder, moveError, combined)
			return 1
		}
	}
	fmt.Fprintf(output, "moved %d folders with git mv\n", len(mapping.moves))
	for _, filename := range sortedKeys(rewritten) {
		information, statError := os.Stat(filename)
		if statError != nil {
			fmt.Fprintf(output, "foldernames: %v\n", statError)
			return 1
		}
		if writeError := os.WriteFile(filename, rewritten[filename], information.Mode()); writeError != nil {
			fmt.Fprintf(output, "foldernames: %v\n", writeError)
			return 1
		}
	}
	fmt.Fprintf(output, "wrote %d rewritten files\n", len(rewritten))

	reloaded, reloadError := workspace.Load(root, nil)
	if reloadError != nil {
		fmt.Fprintf(output, "foldernames: the moved tree does not load cleanly: %v\n", reloadError)
		return 1
	}
	if !prove(output, "on disk", reloaded, mapping, nil, expected, platformLoads) {
		return 1
	}
	return 0
}

// platformLoad is a platform whose own files a move touches, loaded before the moves, with the files only it
// reads and what they must read after.
type platformLoad struct {
	load     *workspace.PlatformLoad
	files    map[string]bool
	expected *expectation
}

// planLoad plans every file of roots under the root that no earlier load planned, and only files in limit when
// it is set. Each file is planned once, from the first variant that holds it, since every variant reads its
// imports the same.
// Mutates: typed each file planned here joins the set every later load reads, so no file is planned twice
func (plan *plan) planLoad(
	loaded *workspace.Workspace,
	roots []*packages.Package,
	mapping *mapping,
	sources fileSource,
	typed map[string]bool,
	limit map[string]bool,
) {
	for _, rootsPackage := range roots {
		var proposals []rename.Proposal
		var files []*ast.File
		for _, file := range rootsPackage.Syntax {
			filename := filepath.Clean(rootsPackage.Fset.PositionFor(file.Pos(), false).Filename)
			folder, inside := relativeFolderOf(loaded.Root, filename)
			if !inside || folder == "TypeScript" || strings.HasPrefix(folder, "TypeScript/") || typed[filename] ||
				limit != nil && !limit[filename] {
				continue
			}
			typed[filename] = true
			files = append(files, file)
			proposals = append(proposals, plan.planTypedFile(
				mapping,
				sources,
				typedFile{goPackage: rootsPackage, file: file, filename: filename, relativeFolder: folder},
			)...)
		}
		if len(proposals) == 0 {
			continue
		}
		renamePackage := rename.Package{
			FileSet: rootsPackage.Fset,
			Types:   rootsPackage.Types,
			Info:    rootsPackage.TypesInfo,
			Files:   files,
		}
		checked := rename.Check(renamePackage, proposals)
		for _, rejection := range checked.Rejections {
			plan.refusals = append(plan.refusals, fmt.Sprintf("%s: renaming %s to %s, out of an import's way: %s",
				rejection.Position, rejection.Object.Name(), rejection.NewName, rejection.Reason))
		}
		for object, newName := range checked.Accepted {
			plan.renamedObjects[object] = newName
			plan.counts.LocalsRenamed++
		}
		for identifier, object := range rootsPackage.TypesInfo.Defs {
			plan.nameRenamed(rootsPackage, identifier, object, checked.Accepted)
		}
		for identifier, object := range rootsPackage.TypesInfo.Uses {
			plan.nameRenamed(rootsPackage, identifier, object, checked.Accepted)
		}
		plan.edits = append(plan.edits, rename.Edits(renamePackage, checked.Accepted)...)
	}
}

// nameRenamed records an identifier of a renamed local under its new name.
func (plan *plan) nameRenamed(
	goPackage *packages.Package,
	identifier *ast.Ident,
	object types.Object,
	accepted map[types.Object]string,
) {
	newName, renamed := accepted[object]
	if !renamed {
		return
	}
	position := goPackage.Fset.PositionFor(identifier.Pos(), false)
	plan.names[rename.EditKey{Filename: position.Filename, Offset: position.Offset}] = newName
}

// planPlatforms loads each platform whose own files a move touches, a file only that platform builds that sits in
// a moved folder or imports one, and plans those files with its types. A file no platform builds, or whose
// package does not type-check under its platform, is left to the reading without types.
func (plan *plan) planPlatforms(
	loaded *workspace.Workspace,
	mapping *mapping,
	sources fileSource,
	typed map[string]bool,
) []platformLoad {
	hostBuilt := map[string]bool{}
	for _, rootsPackage := range loaded.Roots {
		for _, list := range [][]string{rootsPackage.GoFiles, rootsPackage.CompiledGoFiles, rootsPackage.OtherFiles} {
			for _, filename := range list {
				hostBuilt[filepath.Clean(filename)] = true
			}
		}
	}
	needed := map[workspace.Platform]map[string]bool{}
	ignored := 0
	for _, rootsPackage := range loaded.Roots {
		for _, filename := range rootsPackage.IgnoredFiles {
			filename = filepath.Clean(filename)
			folder, inside := relativeFolderOf(loaded.Root, filename)
			if !inside ||
				!strings.HasSuffix(filename, ".go") ||
				hostBuilt[filename] ||
				folder == "TypeScript" ||
				strings.HasPrefix(folder, "TypeScript/") {
				continue
			}
			ignored++
			if !touched(mapping, folder, filename, sources) {
				continue
			}
			platform, builds := workspace.PlatformFor(filename)
			if !builds {
				continue
			}
			if needed[platform] == nil {
				needed[platform] = map[string]bool{}
			}
			needed[platform][filename] = true
		}
	}
	plan.notes = append(plan.notes, fmt.Sprintf(
		"%d files the host does not build; %d platforms loaded for the ones a move touches",
		ignored,
		len(needed),
	))
	var loads []platformLoad
	for _, platform := range workspace.Platforms {
		files := needed[platform]
		if files == nil {
			continue
		}
		load, loadError := loaded.LoadPlatform(platform, nil)
		if loadError != nil {
			plan.refusals = append(plan.refusals, fmt.Sprintf("platform %s: %v", platform, loadError))
			continue
		}
		plan.planLoad(loaded, load.Clean.Roots, mapping, sources, typed, files)
		translated := map[string]bool{}
		for filename := range files {
			if typed[filename] {
				folder, _ := relativeFolderOf(loaded.Root, filename)
				translated[filepath.Join(
					loaded.Root,
					filepath.FromSlash(mapping.NewFolder(folder)),
					filepath.Base(filename),
				)] = true
			} else {
				plan.notes = append(plan.notes, fmt.Sprintf(
					"platform %s does not type-check the package of %s, so it is read without types",
					platform,
					filename,
				))
			}
		}
		loads = append(loads, platformLoad{load: load, files: translated})
	}
	return loads
}

// touched reports whether a file sits in a moved folder or imports one, from its import block alone.
func touched(mapping *mapping, folder string, filename string, sources fileSource) bool {
	if mapping.Moved(folder) {
		return true
	}
	contents, readError := sources.read(filename)
	if readError != nil {
		return true
	}
	file, parseError := parser.ParseFile(token.NewFileSet(), filename, contents, parser.ImportsOnly)
	if parseError != nil {
		return true
	}
	for _, spec := range file.Imports {
		importPath, unquoteError := strconv.Unquote(spec.Path.Value)
		if unquoteError == nil && mapping.NewImportPath(importPath) != importPath {
			return true
		}
	}
	return false
}

// filesUnder lists every regular file in a moved folder, which moves with it.
func filesUnder(root string, mapping *mapping) ([]string, error) {
	var files []string
	for _, move := range mapping.moves {
		if mapping.Moved(path.Dir(move.OldFolder)) {
			continue
		}
		walkError := filepath.WalkDir(filepath.Join(
			root,
			filepath.FromSlash(move.OldFolder),
		), func(filename string, entry fs.DirEntry, pathError error) error {
			if pathError != nil {
				return pathError
			}
			if entry.Type().IsRegular() {
				files = append(files, filename)
			}
			return nil
		})
		if walkError != nil {
			return nil, walkError
		}
	}
	sort.Strings(files)
	return files, nil
}

// buildOverlay rewrites each edited file and lays every moved or rewritten file over its new path. It returns the
// overlay and the rewritten files by their new paths.
func buildOverlay(
	edits []rename.Edit,
	movedFiles []string,
	newPath func(string) string,
) (map[string][]byte, map[string][]byte, error) {
	overlay := map[string][]byte{}
	rewritten := map[string][]byte{}
	byFile := rename.GroupByFile(edits)
	for _, filename := range sortedKeys(byFile) {
		_, contents, rewriteError := rename.RewriteFile(filename, byFile[filename])
		if rewriteError != nil {
			return nil, nil, rewriteError
		}
		overlay[newPath(filename)] = contents
		rewritten[newPath(filename)] = contents
	}
	for _, filename := range movedFiles {
		if _, edited := byFile[filename]; edited {
			continue
		}
		contents, readError := os.ReadFile(filename)
		if readError != nil {
			return nil, nil, readError
		}
		overlay[newPath(filename)] = contents
	}
	return overlay, rewritten, nil
}

// prove loads the tree, through overlay when one is given, and compares it, and each platform's own files, with
// what the moves must leave.
func prove(
	output *bufio.Writer,
	where string,
	loaded *workspace.Workspace,
	mapping *mapping,
	overlay map[string][]byte,
	expected *expectation,
	platformLoads []platformLoad,
) bool {
	after := loaded
	if overlay != nil {
		var reloadError error
		after, reloadError = workspace.LoadWithOverlay(loaded.Root, nil, overlay)
		if reloadError != nil {
			fmt.Fprintf(output, "the moved tree does not load cleanly %s: %v\n", where, reloadError)
			return false
		}
	}
	compared, mismatches := expected.compare(after.Roots, mapping, output)
	fmt.Fprintf(
		output,
		"proof %s: %d root packages, zero package errors, %d identifiers compared, %d mismatches\n",
		where,
		len(after.Roots),
		compared,
		mismatches,
	)
	for _, platformLoad := range platformLoads {
		reloaded, reloadError := after.LoadPlatform(platformLoad.load.Platform, overlay)
		if reloadError != nil {
			fmt.Fprintf(output, "platform %s %s: %v\n", platformLoad.load.Platform, where, reloadError)
			return false
		}
		platformCompared, platformMismatches := platformLoad.expected.compare(reloaded.Clean.Roots, mapping, output)
		fmt.Fprintf(output, "proof %s, platform %s: %d files, %d identifiers compared, %d mismatches\n", where,
			platformLoad.load.Platform, len(platformLoad.files), platformCompared, platformMismatches)
		mismatches += platformMismatches
	}
	return mismatches == 0
}

// readModulePath is the root module's path, from its go.mod.
func readModulePath(root string) (string, error) {
	contents, readError := os.ReadFile(filepath.Join(root, "go.mod"))
	if readError != nil {
		return "", readError
	}
	modulePath := modfile.ModulePath(contents)
	if modulePath == "" {
		return "", fmt.Errorf("%s/go.mod names no module", root)
	}
	return modulePath, nil
}

// report prints the counts, every refusal and every note.
func report(output *bufio.Writer, root string, mapping *mapping, plan *plan, edits []rename.Edit, movedFiles []string) {
	editedFiles := map[string]bool{}
	for _, edit := range edits {
		editedFiles[edit.Filename] = true
	}
	fmt.Fprintf(output, "folders: %d moves, %d files moved with them\n", len(mapping.moves), len(movedFiles))
	fmt.Fprintf(
		output,
		"edits: %d in %d files: %d package clauses, %d package docs, %d import paths, %d aliases dropped, %d aliases kept, %d qualifiers, %d locals renamed, %d files read without types\n",
		len(edits),
		len(editedFiles),
		plan.counts.PackageClauses,
		plan.counts.PackageDocs,
		plan.counts.ImportPaths,
		plan.counts.AliasesDropped,
		plan.counts.AliasesKept,
		plan.counts.Qualifiers,
		plan.counts.LocalsRenamed,
		plan.counts.SyntacticFiles,
	)
	trim := func(text string) string { return strings.ReplaceAll(text, root+"/", "") }
	fmt.Fprintf(output, "refused: %d\n", len(plan.refusals))
	for _, refusal := range plan.refusals {
		fmt.Fprintln(output, "  "+trim(refusal))
	}
	fmt.Fprintln(output, "notes:")
	for _, note := range plan.notes {
		fmt.Fprintln(output, "  "+trim(note))
	}
}
