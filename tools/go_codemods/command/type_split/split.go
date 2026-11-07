package main

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"sort"
	"strings"
)

// directoryResult is what splitting one directory would change, and what it found.
type directoryResult struct {
	// changes maps a file name to its new content; a nil content deletes the file.
	changes   map[string][]byte
	newFiles  []string
	deleted   []string
	typeMoves int
	// methodMoves counts groups of a type's methods that left a file without the type's declaration:
	// toDeclaration joined the declaration's file, the rest made or joined a themed file.
	methodMovesToDeclaration int
	methodMovesToThemed      int
	conflicts                []string
	families                 []string
	failures                 []string
	pinnedFiles              []string
	before                   counts
	after                    counts
	// servingSeveral is each judged file that served more than one type before the split, as
	// servingSeveral reports it.
	servingSeveral []string
	// misnamedBefore is each file the file name rule flagged before the split, as "name\tType".
	misnamedBefore []string
	// continuations is each file of sources that goes on under another name, renamed or merged into another.
	continuations []fileMove
	// moves is every file whose content went to another name, in the order renames.tsv lists them: the split's
	// first, then the name pass's, so the last line for a name is where most of it went.
	moves []fileMove
	// renamed, merged and nameConflicts are the name pass's report: files that took the name the rule asks
	// for or a themed one, files whose code joined the file of that name, and files left as they are.
	renamed       []string
	merged        []string
	nameConflicts []string
	// extractions counts files whose one type left them because it was a minority of the file, and
	// extractionsTried names the files a round tried that for, so its name pass renames them only
	// when the type could not leave.
	extractions      int
	extractionsTried map[string]bool
}

// chunk is a declaration's text on its way to another file, with the file it came from, whose imports
// it was written against.
type chunk struct {
	text   []byte
	source *sourceFile
}

type byteRange struct {
	start int
	end   int
}

// maximumRounds is how many split and name rounds a directory gets to settle. A round's renames can free a
// name an earlier split found taken, so a second round is ordinary; one that still changes something after
// this many is reported rather than written.
const maximumRounds = 5

// splitDirectory splits every package group in a directory so each file serves at most one type
// (planGroup) and names each file for the one type it serves (planRenames), round after round until a
// round changes nothing. otherNames are names in the directory that are not among sources, so no file
// takes one. Nothing is changed in a group whose result fails the equivalence check; that group is
// reported instead. The result's changes and moves are from sources to the last round's files.
func splitDirectory(directory string, sources map[string][]byte, otherNames []string, pinned map[string]string,
	around surroundings) (*directoryResult, error) {
	result := &directoryResult{changes: map[string][]byte{}}
	current := maps.Clone(sources)
	// destinations is, per name of sources, every name its content went to, the one that took most of it last.
	destinations := map[string][]string{}
	// continuesAs is, per name of sources, the name it goes on under, or absent once the split emptied it.
	continuesAs := map[string]string{}
	for name := range sources {
		continuesAs[name] = name
	}
	settled := false
	for round := 0; round < maximumRounds; round++ {
		roundResult, roundError := splitRound(directory, current, otherNames, pinned, around)
		if roundError != nil {
			return nil, roundError
		}
		if round == 0 {
			result.before = roundResult.before
			result.servingSeveral = roundResult.servingSeveral
			result.misnamedBefore = roundResult.misnamedBefore
			result.pinnedFiles = roundResult.pinnedFiles
		}
		result.after = roundResult.after
		result.typeMoves += roundResult.typeMoves
		result.extractions += roundResult.extractions
		result.methodMovesToDeclaration += roundResult.methodMovesToDeclaration
		result.methodMovesToThemed += roundResult.methodMovesToThemed
		result.families = append(result.families, roundResult.families...)
		result.renamed = append(result.renamed, roundResult.renamed...)
		result.merged = append(result.merged, roundResult.merged...)
		// What is left in conflict, failing or unnamed is what the last round found.
		result.conflicts = roundResult.conflicts
		result.failures = roundResult.failures
		result.nameConflicts = roundResult.nameConflicts
		if len(roundResult.changes) == 0 {
			settled = true
			break
		}
		// A file a round makes out of another is followed before the moves that take a whole file, so a file both
		// split and renamed in one round leads to every file its content reached.
		roundMoves := slices.Clone(roundResult.moves)
		slices.SortStableFunc(roundMoves, func(left, right fileMove) int {
			return cmp.Compare(boolRank(left.whole), boolRank(right.whole))
		})
		for _, move := range roundMoves {
			followMove(destinations, sources, move)
			if !move.whole {
				continue
			}
			for name, current := range continuesAs {
				if current != move.from {
					continue
				}
				if move.continues {
					continuesAs[name] = move.to
				} else {
					delete(continuesAs, name)
				}
			}
		}
		for name, content := range roundResult.changes {
			if content == nil {
				delete(current, name)
			} else {
				current[name] = content
			}
		}
	}
	if !settled {
		result.failures = append(result.failures, fmt.Sprintf("%s: still changing after %d rounds, nothing written",
			directory, maximumRounds))
		result.after = result.before
		return result, nil
	}
	for name, content := range current {
		if !bytes.Equal(sources[name], content) {
			result.changes[name] = content
		}
	}
	for name := range sources {
		if _, kept := current[name]; !kept {
			result.changes[name] = nil
		}
	}
	for _, name := range sortedNames(result.changes) {
		_, existed := sources[name]
		switch {
		case !existed:
			result.newFiles = append(result.newFiles, name)
		case result.changes[name] == nil:
			result.deleted = append(result.deleted, name)
		}
	}
	for _, name := range sortedNames(sources) {
		if current, continues := continuesAs[name]; continues && current != name {
			result.continuations = append(result.continuations, fileMove{from: name, to: current, continues: true})
		}
	}
	// A file that stays is its own last destination, so the gate, which keeps the last line per path, follows the
	// findings it keeps to itself and only the moved ones elsewhere.
	for _, name := range sortedNames(sources) {
		list := destinations[name]
		if len(list) == 1 && list[0] == name {
			continue
		}
		for index, destination := range list {
			// A destination listed twice, a file that took code and then the rest, is kept at its last place.
			if !slices.Contains(list[index+1:], destination) {
				result.moves = append(result.moves, fileMove{from: name, to: destination})
			}
		}
	}
	return result, nil
}

// boolRank orders false before true.
func boolRank(value bool) int {
	if value {
		return 1
	}
	return 0
}

// followMove records a round's move in destinations, keyed by the names of sources: every name whose content
// had reached move.from now reaches move.to too. When the move takes move.from whole, move.to takes its place as
// where that content went; when move.from stays, as the split's source does, move.to is one more place, ahead of
// it.
// Mutates: destinations each round's move is recorded in the one map the rounds build together
func followMove(destinations map[string][]string, sources map[string][]byte, move fileMove) {
	fromStays := !move.whole
	for name := range sources {
		list := destinations[name]
		if len(list) == 0 {
			list = []string{name}
		}
		reaches := false
		for index, destination := range list {
			if destination == move.from {
				reaches = true
				if !fromStays {
					list[index] = move.to
				}
			}
		}
		if !reaches {
			continue
		}
		if fromStays {
			list = append([]string{move.to}, list...)
		}
		destinations[name] = list
	}
}

// splitRound is one round of splitDirectory: the split of every group, then the name pass over its result.
func splitRound(directory string, sources map[string][]byte, otherNames []string, pinned map[string]string,
	around surroundings) (*directoryResult, error) {
	result := &directoryResult{changes: map[string][]byte{}, extractionsTried: map[string]bool{}}
	groups, parseGroupsError := parseGroups(directory, sources, pinned, around)
	if parseGroupsError != nil {
		return nil, parseGroupsError
	}
	taken := map[string]bool{}
	for name := range sources {
		taken[strings.ToLower(name)] = true
	}
	for _, name := range otherNames {
		taken[strings.ToLower(name)] = true
	}
	for _, group := range groups {
		result.before.add(measure(group))
		result.servingSeveral = append(result.servingSeveral, servingSeveral(group)...)
		for _, misnamed := range misnamedFiles(group) {
			result.misnamedBefore = append(result.misnamedBefore, misnamed.file.name+"\t"+misnamed.typeName)
		}
		for _, file := range group.files {
			if file.pinned != "" {
				result.pinnedFiles = append(
					result.pinnedFiles,
					fmt.Sprintf("%s/%s: %s", directory, file.name, file.pinned),
				)
			}
		}
		plan := planGroup(group, taken, result)
		if len(plan.removals) == 0 && len(plan.appends) == 0 {
			continue
		}
		outputs, groupError := applyGroup(group, plan, around.importName)
		if groupError == nil {
			groupError = checkGroup(group, outputs)
		}
		if groupError != nil {
			result.failures = append(
				result.failures,
				fmt.Sprintf("%s (package %s): %v", directory, group.packageName, groupError),
			)
			continue
		}
		names := make([]string, 0, len(outputs))
		for name := range outputs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			result.changes[name] = outputs[name]
			if plan.created[name] != nil {
				result.newFiles = append(result.newFiles, name)
			} else if outputs[name] == nil {
				result.deleted = append(result.deleted, name)
			}
		}
		// Every file that took code from another is one more place that file's findings may have gone, a new
		// file or an existing one such as the type's declaration file; a file the split emptied went whole to the
		// file that took its first code.
		for _, target := range sortedNames(plan.appendTargets()) {
			seen := map[string]bool{}
			for _, piece := range plan.appends[target] {
				if piece.source.name != target && !seen[piece.source.name] {
					seen[piece.source.name] = true
					result.moves = append(result.moves, fileMove{from: piece.source.name, to: target})
				}
			}
		}
		for _, name := range names {
			if outputs[name] == nil && plan.created[name] == nil {
				result.moves = append(result.moves, fileMove{from: name, to: plan.receiverOf(name), whole: true})
			}
		}
	}
	afterSources := map[string][]byte{}
	for name, source := range sources {
		afterSources[name] = source
	}
	for name, content := range result.changes {
		if content == nil {
			delete(afterSources, name)
		} else {
			afterSources[name] = content
		}
	}
	afterGroups, parseGroupsError := parseGroups(directory, afterSources, pinned, around)
	if parseGroupsError != nil {
		return nil, fmt.Errorf("after the split: %w", parseGroupsError)
	}
	// The name pass reads the split's result, so a file is named for the one type it serves once the split is done.
	afterTaken := map[string]bool{}
	for name := range afterSources {
		afterTaken[strings.ToLower(name)] = true
	}
	for _, name := range otherNames {
		afterTaken[strings.ToLower(name)] = true
	}
	renames := planRenames(directory, afterGroups, afterTaken, around, result)
	for _, name := range sortedNames(renames.changes) {
		content := renames.changes[name]
		switch {
		case content != nil:
			result.changes[name] = content
			afterSources[name] = content
		case sources[name] != nil:
			result.changes[name] = nil
			delete(afterSources, name)
		default:
			// A file the split made and the name pass moved on was never written.
			delete(result.changes, name)
			delete(afterSources, name)
		}
	}
	for _, move := range renames.moves {
		followed := false
		for index := range result.moves {
			if result.moves[index].to == move.from && sources[move.from] == nil {
				result.moves[index].to = move.to
				followed = true
			}
		}
		if !followed {
			result.moves = append(result.moves, move)
		}
	}
	finalGroups, parseGroupsError := parseGroups(directory, afterSources, pinned, around)
	if parseGroupsError != nil {
		return nil, fmt.Errorf("after the name pass: %w", parseGroupsError)
	}
	for _, group := range finalGroups {
		result.after.add(measure(group))
	}
	return result, nil
}

func docStart(doc *ast.CommentGroup, position token.Pos) token.Pos {
	if doc != nil {
		return doc.Pos()
	}
	return position
}

// applyGroup produces the new content of every file the plan touches, formatted, with each file's
// imports matching what it uses. A file left with nothing but its package clause maps to nil.
func applyGroup(group *packageGroup, plan *groupPlan, importName func(string) string) (map[string][]byte, error) {
	outputs := map[string][]byte{}
	pool := func(chunks []chunk) []*sourceFile {
		var files []*sourceFile
		seen := map[*sourceFile]bool{}
		for _, piece := range chunks {
			if !seen[piece.source] {
				seen[piece.source] = true
				files = append(files, piece.source)
			}
		}
		return files
	}
	for _, file := range group.files {
		removals := plan.removals[file]
		appends := plan.appends[file.name]
		if len(removals) == 0 && len(appends) == 0 {
			continue
		}
		text, spliceError := splice(file.source, removals)
		if spliceError != nil {
			return nil, fmt.Errorf("%s: %w", file.name, spliceError)
		}
		for _, piece := range appends {
			text = append(text, '\n')
			text = append(text, piece.text...)
		}
		text, fixImportsError := fixImports(text, pool(appends), importName)
		if fixImportsError != nil {
			return nil, fmt.Errorf("%s: %w", file.name, fixImportsError)
		}
		formatted, formatError := format.Source(text)
		if formatError != nil {
			return nil, fmt.Errorf("%s: %w", file.name, formatError)
		}
		if len(removals) > 0 && onlyPackageClause(formatted) {
			formatted = nil
		}
		outputs[file.name] = formatted
	}
	for name, origin := range plan.created {
		var text bytes.Buffer
		for _, line := range origin.buildLines {
			text.WriteString(line)
			text.WriteString("\n")
		}
		if len(origin.buildLines) > 0 {
			text.WriteString("\n")
		}
		text.WriteString("package " + group.packageName + "\n")
		for _, piece := range plan.appends[name] {
			text.WriteString("\n")
			text.Write(piece.text)
		}
		fixed, fixImportsError := fixImports(text.Bytes(), pool(plan.appends[name]), importName)
		if fixImportsError != nil {
			return nil, fmt.Errorf("%s: %w", name, fixImportsError)
		}
		formatted, formatError := format.Source(fixed)
		if formatError != nil {
			return nil, fmt.Errorf("%s: %w", name, formatError)
		}
		outputs[name] = formatted
	}
	return outputs, nil
}

// splice removes the ranges from source.
func splice(source []byte, removals []byteRange) ([]byte, error) {
	sorted := append([]byteRange(nil), removals...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left].start < sorted[right].start })
	var result []byte
	cursor := 0
	for _, span := range sorted {
		if span.start < cursor {
			return nil, fmt.Errorf("overlapping removals at byte %d", span.start)
		}
		result = append(result, source[cursor:span.start]...)
		cursor = span.end
	}
	return append(result, source[cursor:]...), nil
}

// onlyPackageClause reports a file with no declarations and no comments but build lines.
func onlyPackageClause(source []byte) bool {
	syntax, parseFileError := parser.ParseFile(token.NewFileSet(), "", source, parser.ParseComments)
	if parseFileError != nil || len(syntax.Decls) > 0 {
		return false
	}
	for _, group := range syntax.Comments {
		for _, comment := range group.List {
			if !isBuildLine(comment.Text) {
				return false
			}
		}
	}
	return true
}

func isBuildLine(text string) bool {
	return strings.HasPrefix(text, "//go:build") || strings.HasPrefix(text, "// +build")
}

// familyCandidates lists the free functions and method-less types of a file whose every reference in
// the package is inside the moved ranges of that file: what a type took with it when it left.
func familyCandidates(
	group *packageGroup,
	file *sourceFile,
	typeName string,
	target string,
	moved []byteRange,
) []string {
	candidates := map[string]ast.Node{}
	withMethods := map[string]bool{}
	for _, other := range typesOf(group) {
		if other.counted() {
			withMethods[other.name] = true
		}
	}
	for _, declaration := range file.syntax.Decls {
		switch node := declaration.(type) {
		case *ast.FuncDecl:
			if node.Recv == nil && node.Name.Name != "init" && node.Name.Name != "main" && node.Name.Name != "_" {
				candidates[node.Name.Name] = node
			}
		case *ast.GenDecl:
			if node.Tok != token.TYPE {
				continue
			}
			for _, spec := range node.Specs {
				typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
				if !isTypeSpec {
					continue
				}
				if !withMethods[typeSpec.Name.Name] {
					candidates[typeSpec.Name.Name] = typeSpec
				}
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	inside := map[string]bool{}
	outside := map[string]bool{}
	for _, other := range group.files {
		for _, reference := range references(other.syntax) {
			if candidates[reference.Name] == nil {
				continue
			}
			offset := other.offset(reference.Pos())
			within := false
			if other != file {
				outside[reference.Name] = true
				continue
			}
			for _, span := range moved {
				if offset >= span.start && offset < span.end {
					within = true
				}
			}
			if within {
				inside[reference.Name] = true
			} else {
				outside[reference.Name] = true
			}
		}
	}
	var names []string
	for name := range inside {
		if !outside[name] {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return []string{fmt.Sprintf(
		"%s/%s: %s moved to %s; used only by it: %s",
		group.directory,
		file.name,
		typeName,
		target,
		strings.Join(names, ", "),
	)}
}

// references is every identifier in a file that may name a package-level declaration: not a
// declaration's own name, a field, a selected member, a composite literal key or a label, and not
// resolved to a local.
func references(syntax *ast.File) []*ast.Ident {
	skip := map[*ast.Ident]bool{}
	topLevel := map[any]bool{}
	for _, declaration := range syntax.Decls {
		switch node := declaration.(type) {
		case *ast.FuncDecl:
			topLevel[node] = true
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				topLevel[spec] = true
			}
		}
	}
	ast.Inspect(syntax, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			skip[node.Sel] = true
		case *ast.Field:
			for _, name := range node.Names {
				skip[name] = true
			}
		case *ast.KeyValueExpr:
			if key, ok := node.Key.(*ast.Ident); ok {
				skip[key] = true
			}
		case *ast.FuncDecl:
			skip[node.Name] = true
		case *ast.TypeSpec:
			skip[node.Name] = true
		case *ast.ValueSpec:
			for _, name := range node.Names {
				skip[name] = true
			}
		case *ast.LabeledStmt:
			skip[node.Label] = true
		case *ast.BranchStmt:
			if node.Label != nil {
				skip[node.Label] = true
			}
		case *ast.ImportSpec:
			if node.Name != nil {
				skip[node.Name] = true
			}
		}
		return true
	})
	var found []*ast.Ident
	ast.Inspect(syntax, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok || skip[identifier] {
			return true
		}
		if identifier.Obj == nil || topLevel[identifier.Obj.Decl] {
			found = append(found, identifier)
		}
		return true
	})
	return found
}
