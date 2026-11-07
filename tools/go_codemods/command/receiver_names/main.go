// Command receivernames spells out the method receivers of cohere's Go code. A receiver named with one or two
// letters, or with a word in the abbreviation vocabulary, becomes the last camel word of its type's name,
// lowercased (`r *swiftRun` becomes `run`). When that word is a Go keyword, a predeclared identifier, an
// import of a file the type's methods live in, a standard library package a reader would take it for
// (`context`, `json`, ...), or itself an abbreviation, the last two words are taken instead (`n *nodeType`
// becomes `nodeType`). A negation word just before the chosen words joins them (`NotAllowedError` falls back
// to `notAllowedError`), and an override table names the types whose computed name reads badly. All receivers of one
// type move together: if the engine refuses any of them, the whole type tries the two-word name, and if that is refused
// too, the type is left for the waves and reported. Every rename goes through the rename engine; after writing, the
// tree is reloaded and every identifier's resolution compared.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/policy"
	"github.com/system-inc/cohere/tools/go_codemods/rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"
)

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code receivernames exits with: runCommandLine returns its exit code here, and
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

// runCommandLine reads the flags and runs receivernames, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print counts and every rename and dropped type, write nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	abbreviations := flag.String(
		"abbreviations",
		"",
		"the abbreviation vocabulary (default: the one built into the pass, from the tree it is built in)",
	)
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	flag.Parse()
	return run(*root, skips, *abbreviations, *dryRun)
}

// loadVocabulary reads every abbreviated word of the vocabulary file, lowercased: the one at filename, or the one built
// into the pass when filename is empty, so a pass run over another repository judges words as it does over cohere.
func loadVocabulary(filename string) (map[string]bool, error) {
	contents := policy.AbbreviationsFile()
	if filename != "" {
		read, readError := os.ReadFile(filename)
		if readError != nil {
			return nil, readError
		}
		contents = read
	}
	var decoded struct {
		Abbreviations []struct {
			Abbreviation string `json:"abbreviation"`
		} `json:"abbreviations"`
	}
	if decodeError := json.Unmarshal(contents, &decoded); decodeError != nil {
		return nil, fmt.Errorf("%s: %w", filename, decodeError)
	}
	vocabulary := map[string]bool{}
	for _, entry := range decoded.Abbreviations {
		if entry.Abbreviation != "" {
			vocabulary[strings.ToLower(entry.Abbreviation)] = true
		}
	}
	if len(vocabulary) == 0 {
		return nil, fmt.Errorf("%s: no abbreviations", filename)
	}
	return vocabulary, nil
}

func run(root string, skips []string, abbreviationsFile string, dryRun bool) int {
	vocabulary, vocabularyError := loadVocabulary(abbreviationsFile)
	if vocabularyError != nil {
		fmt.Fprintf(streams.Errors, "receivernames: vocabulary: %v\n", vocabularyError)
		return 1
	}
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(streams.Errors, "receivernames: loading: %v\n", loadError)
		return 1
	}
	fmt.Fprintf(
		streams.Output,
		"loaded %d root packages, analyzing %d variants\n",
		len(loaded.Roots),
		len(loaded.Analyzed),
	)
	// The analysis takes a while on a whole worktree, so the load's count is seen before it.
	streams.Show(streams.Output)
	fmt.Fprintf(streams.Output, "vocabulary: %d abbreviations\n", len(vocabulary))
	editableFiles, skippedFiles := 0, 0
	for _, analyzedPackage := range loaded.Analyzed {
		editable := len(loaded.RenamePackage(analyzedPackage).Files)
		editableFiles += editable
		skippedFiles += len(analyzedPackage.Syntax) - editable
	}
	fmt.Fprintf(streams.Output, "files in analyzed variants: %d editable, %d skipped\n", editableFiles, skippedFiles)
	if editableFiles == 0 {
		fmt.Fprintln(streams.Errors, "receivernames: no editable files; the root or skip rules are wrong")
		return 1
	}

	var allEdits []rename.Edit
	var allRenames []receiverRename
	var allDropped []droppedType
	var allMixed []mixedType
	var allUnresolved []string
	var totals receiverCounts
	threeLetterNames := map[string]int{}
	for _, analyzedPackage := range loaded.Analyzed {
		renamePackage := loaded.RenamePackage(analyzedPackage)
		plan := planReceivers(renamePackage, analyzedPackage.Syntax, vocabulary)
		totals.add(plan.counts)
		for name, count := range plan.threeLetterNames {
			threeLetterNames[name] += count
		}
		allRenames = append(allRenames, plan.renames...)
		allDropped = append(allDropped, plan.dropped...)
		allMixed = append(allMixed, plan.mixed...)
		for _, unresolved := range plan.unresolved {
			allUnresolved = append(allUnresolved, analyzedPackage.ID+": "+unresolved)
		}
		allEdits = append(allEdits, rename.Edits(renamePackage, plan.accepted)...)
	}

	edits, disagreements := rename.Deduplicate(allEdits)
	editsByFile := rename.GroupByFile(edits)

	renamesByKind := map[string]int{}
	for _, receiver := range allRenames {
		renamesByKind[receiver.kind]++
	}
	droppedReceivers := 0
	droppedByClass := map[string]int{}
	for _, dropped := range allDropped {
		droppedReceivers += dropped.receivers
		droppedByClass[dropped.class]++
	}

	fmt.Fprintf(streams.Output, "receivers seen in editable files: %d\n", totals.seen)
	fmt.Fprintf(streams.Output, "  unnamed or blank, left alone: %d\n", totals.unnamedOrBlank)
	fmt.Fprintf(
		streams.Output,
		"  not eligible (three letters, a full word, or several words, none in the vocabulary): %d\n",
		totals.notEligible,
	)
	fmt.Fprintf(streams.Output, "  eligible (one or two letters, or a vocabulary word): %d\n", totals.eligible)
	fmt.Fprintf(streams.Output, "renamed: %d\n", len(allRenames))
	for _, kind := range sortedKeys(renamesByKind) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", kind, renamesByKind[kind])
	}
	fmt.Fprintf(
		streams.Output,
		"eligible receivers left in dropped types: %d, in %d types\n",
		droppedReceivers,
		len(allDropped),
	)
	for _, class := range sortedKeys(droppedByClass) {
		fmt.Fprintf(streams.Output, "  %s: %d types\n", class, droppedByClass[class])
	}
	fmt.Fprintf(
		streams.Output,
		"types whose methods use more than one receiver name after the pass: %d\n",
		len(allMixed),
	)
	fmt.Fprintf(streams.Output, "edits: %d in %d files\n", len(edits), len(editsByFile))
	fmt.Fprintf(streams.Output, "variant disagreements: %d\n", len(disagreements))
	fmt.Fprintf(streams.Output, "unresolved occurrences (scope model gaps): %d\n", len(allUnresolved))

	fmt.Fprintln(streams.Output, "\nrenames:")
	sort.Slice(allRenames, func(left, right int) bool {
		return positionLess(allRenames[left].position, allRenames[right].position)
	})
	for _, receiver := range allRenames {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s -> %s (%s, %s)\n",
			relativePosition(loaded, receiver.position),
			receiver.oldName,
			receiver.newName,
			receiver.typeName,
			receiver.kind,
		)
		fmt.Fprintf(streams.Output, "      before: %s\n", receiver.lineBefore)
		fmt.Fprintf(streams.Output, "      after:  %s\n", receiver.lineAfter)
	}
	fmt.Fprintln(streams.Output, "\ndropped types (left for waves):")
	sort.Slice(allDropped, func(left, right int) bool {
		return positionLess(allDropped[left].position, allDropped[right].position)
	})
	for _, dropped := range allDropped {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s (%d receivers %s): %s: %s\n",
			relativePosition(loaded, dropped.position),
			dropped.typeName,
			dropped.receivers,
			strings.Join(dropped.names, ","),
			dropped.class,
			strings.ReplaceAll(dropped.reason, loaded.Root+"/", ""),
		)
	}
	fmt.Fprintln(streams.Output, "\ntypes with mixed receiver names after the pass:")
	sort.Slice(allMixed, func(left, right int) bool {
		return positionLess(allMixed[left].position, allMixed[right].position)
	})
	for _, mixed := range allMixed {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s: %s\n",
			relativePosition(loaded, mixed.position),
			mixed.typeName,
			mixed.names,
		)
	}
	fmt.Fprintln(streams.Output, "\nthree-letter receivers outside the vocabulary, left alone:")
	for _, name := range sortedKeys(threeLetterNames) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", name, threeLetterNames[name])
	}
	fmt.Fprintln(streams.Output, "\nvariant disagreements:")
	for _, disagreement := range disagreements {
		fmt.Fprintln(streams.Output, "  "+disagreement)
	}
	fmt.Fprintln(streams.Output, "\nunresolved occurrences:")
	for _, unresolved := range allUnresolved {
		fmt.Fprintln(streams.Output, "  "+strings.ReplaceAll(unresolved, loaded.Root+"/", ""))
	}

	if len(disagreements) > 0 {
		fmt.Fprintln(streams.Errors, "receivernames: variants disagree on some edits; those spans were dropped")
	}
	if dryRun {
		return 0
	}
	return apply(loaded, root, skips, edits, editsByFile)
}

// receiverOverrides names the receivers of types whose computed name reads badly, by type name. An override
// is applied before any computed name and is the type's only candidate.
var receiverOverrides = map[string]string{
	"importerFromMap": "importer",
	"composeContext":  "composition",
}

// confusingPackageNames are standard library packages a reader would confuse a receiver with, imported in the
// method's file or not: `ctx *composeContext` must not become `context`.
var confusingPackageNames = map[string]bool{
	"bytes": true, "context": true, "errors": true, "filepath": true, "fmt": true, "io": true, "json": true, "os": true,
	"path": true, "regexp": true, "sort": true, "strings": true, "sync": true, "testing": true, "time": true,
}

// negationWords keep the word after them in a receiver name.
var negationWords = map[string]bool{"not": true, "no": true, "non": true}

// receiverRename is one accepted receiver rename, with its declaring line before and after.
type receiverRename struct {
	position token.Position
	typeName string
	oldName  string
	newName  string
	// kind is "override", "last word", or "fallback" (the two-word name), each with a negation kept.
	kind       string
	lineBefore string
	lineAfter  string
}

// droppedType is a type whose eligible receivers were all left alone, and why.
type droppedType struct {
	position  token.Position
	typeName  string
	receivers int
	names     []string
	class     string
	reason    string
}

// mixedType is a type whose methods still use more than one receiver name.
type mixedType struct {
	position token.Position
	typeName string
	names    string
}

// receiverPlan is what planReceivers decided for one package.
type receiverPlan struct {
	accepted         map[types.Object]string
	renames          []receiverRename
	dropped          []droppedType
	mixed            []mixedType
	unresolved       []string
	counts           receiverCounts
	threeLetterNames map[string]int
}

// receiverMember is one method's receiver.
type receiverMember struct {
	variable *types.Var
	file     *ast.File
	editable bool
	eligible bool
}

// candidateState is how far a type's receivers have fallen back.
type candidateState int

const (
	oneWordState candidateState = iota
	twoWordsState
	droppedState
)

// receiverGroup is every receiver of one type in one package variant.
type receiverGroup struct {
	typeName *types.TypeName
	members  []receiverMember
	oneWord  string
	twoWords string
	// override says oneWord came from receiverOverrides.
	override bool
	state    candidateState
	// notes records why each candidate was passed over, for the report.
	notes      []string
	dropClass  string
	dropReason string
}

// candidate is the name the group proposes in its current state.
func (group *receiverGroup) candidate() string {
	if group.state == twoWordsState {
		return group.twoWords
	}
	return group.oneWord
}

// drop leaves the group for the waves.
func (group *receiverGroup) drop(class string, reason string) {
	group.state = droppedState
	group.dropClass = class
	group.dropReason = reason
}

// planReceivers proposes receiver names for every type of one package variant and runs the engine until every
// type's receivers either all moved or all stayed. renamePackage.Files are the editable files; allFiles is
// every file of the variant, so a type with a receiver to rename in a skipped file can be left whole.
func planReceivers(renamePackage rename.Package, allFiles []*ast.File, vocabulary map[string]bool) receiverPlan {
	plan := receiverPlan{accepted: map[types.Object]string{}, threeLetterNames: map[string]int{}}
	editable := map[*ast.File]bool{}
	for _, file := range renamePackage.Files {
		editable[file] = true
	}

	groups := map[*types.TypeName]*receiverGroup{}
	var order []*receiverGroup
	for _, file := range allFiles {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Recv == nil || len(function.Recv.List) != 1 {
				continue
			}
			field := function.Recv.List[0]
			if len(field.Names) == 0 || field.Names[0].Name == "_" {
				if editable[file] {
					plan.counts.seen++
					plan.counts.unnamedOrBlank++
				}
				continue
			}
			variable, isVariable := renamePackage.Info.Defs[field.Names[0]].(*types.Var)
			if !isVariable {
				continue
			}
			typeName := receiverTypeName(variable)
			if typeName == nil {
				continue
			}
			member := receiverMember{
				variable: variable,
				file:     file,
				editable: editable[file],
				eligible: isEligible(variable.Name(), vocabulary),
			}
			if member.editable {
				plan.counts.seen++
				if member.eligible {
					plan.counts.eligible++
				} else {
					plan.counts.notEligible++
					if len(variable.Name()) == 3 {
						plan.threeLetterNames[variable.Name()]++
					}
				}
			}
			group, found := groups[typeName]
			if !found {
				group = &receiverGroup{typeName: typeName}
				groups[typeName] = group
				order = append(order, group)
			}
			group.members = append(group.members, member)
		}
	}

	var active []*receiverGroup
	for _, group := range order {
		if prepareGroup(renamePackage, group, vocabulary) {
			active = append(active, group)
		}
	}

	// Fixpoint: a type with any rejected receiver falls back one step for all its receivers (one word, then
	// two words, then left alone), and the engine judges the whole set again, until a pass rejects nothing.
	// Each type only moves forward, so this ends.
	var engineResult rename.Plan
	for {
		var proposals []rename.Proposal
		groupOf := map[types.Object]*receiverGroup{}
		for _, group := range active {
			if group.state == droppedState {
				continue
			}
			for _, member := range group.members {
				if member.eligible && member.variable.Name() != group.candidate() {
					proposals = append(proposals, rename.Proposal{Object: member.variable, NewName: group.candidate()})
					groupOf[member.variable] = group
				}
			}
		}
		engineResult = rename.Check(renamePackage, proposals)
		firstRejection := map[*receiverGroup]string{}
		for _, rejection := range engineResult.Rejections {
			group := groupOf[rejection.Object]
			if _, recorded := firstRejection[group]; !recorded {
				firstRejection[group] = fmt.Sprintf(
					"%s -> %s at line %d: %s",
					rejection.Object.Name(),
					rejection.NewName,
					rejection.Position.Line,
					rejection.Reason,
				)
			}
		}
		if len(firstRejection) == 0 {
			break
		}
		for _, group := range active {
			reason, rejected := firstRejection[group]
			if !rejected {
				continue
			}
			note := fmt.Sprintf("%q rejected (%s)", group.candidate(), reason)
			group.notes = append(group.notes, note)
			if group.state == oneWordState && group.twoWords != "" {
				group.state = twoWordsState
				continue
			}
			class := "engine rejected every candidate"
			switch {
			case group.override:
				class = "engine rejected the override"
			case group.state == oneWordState:
				class = "engine rejected the one-word name, no two-word name"
			}
			group.drop(class, strings.Join(group.notes, "; "))
		}
	}

	plan.accepted = engineResult.Accepted
	plan.unresolved = engineResult.Unresolved
	editsByFile := rename.GroupByFile(rename.Edits(renamePackage, plan.accepted))
	for _, group := range order {
		groupPosition := renamePackage.FileSet.Position(group.typeName.Pos())
		if group.state == droppedState && group.dropClass != "" {
			dropped := droppedType{
				position: groupPosition,
				typeName: group.typeName.Name(),
				class:    group.dropClass,
				reason:   group.dropReason,
			}
			names := map[string]bool{}
			for _, member := range group.members {
				if member.eligible {
					dropped.receivers++
					names[member.variable.Name()] = true
				}
			}
			dropped.names = sortedKeys(names)
			plan.dropped = append(plan.dropped, dropped)
		}
		finalNames := map[string]int{}
		for _, member := range group.members {
			newName, renamed := plan.accepted[member.variable]
			if !renamed {
				finalNames[member.variable.Name()]++
				continue
			}
			finalNames[newName]++
			plan.renames = append(
				plan.renames,
				describeRename(renamePackage.FileSet, editsByFile, group, member, newName),
			)
		}
		if len(finalNames) > 1 {
			var parts []string
			for _, name := range sortedKeys(finalNames) {
				parts = append(parts, fmt.Sprintf("%s x%d", name, finalNames[name]))
			}
			plan.mixed = append(
				plan.mixed,
				mixedType{position: groupPosition, typeName: group.typeName.Name(), names: strings.Join(parts, ", ")},
			)
		}
	}
	return plan
}

// prepareGroup chooses a group's one-word and two-word names and its starting state. It reports whether the
// group has anything to propose; a group with eligible receivers but no usable name is dropped and kept for
// the report.
// Mutates: group preparing a group is setting its names, its notes and its starting state
func prepareGroup(renamePackage rename.Package, group *receiverGroup, vocabulary map[string]bool) bool {
	eligibleFiles := map[*ast.File]bool{}
	anyEligible := false
	for _, member := range group.members {
		if !member.eligible {
			continue
		}
		anyEligible = true
		eligibleFiles[member.file] = true
		if !member.editable {
			position := renamePackage.FileSet.Position(member.variable.Pos())
			group.drop("a receiver to rename is in a skipped or generated file",
				fmt.Sprintf("%s at %s:%d", member.variable.Name(), position.Filename, position.Line))
			return false
		}
	}
	if !anyEligible {
		group.state = droppedState
		return false
	}
	importNames := map[string]bool{}
	for file := range eligibleFiles {
		for name := range fileImportNames(renamePackage.Info, file) {
			importNames[name] = true
		}
	}

	if override, overridden := receiverOverrides[group.typeName.Name()]; overridden {
		// An override is the type's only candidate: the computed names were ruled to read badly.
		group.override = true
		if reason := disqualification(override, camelWords(override), importNames, vocabulary); reason != "" {
			group.drop("override unusable", fmt.Sprintf("%q %s", override, reason))
			return false
		}
		group.oneWord = override
		group.state = oneWordState
		return true
	}

	words := camelWords(group.typeName.Name())
	if words == nil {
		group.drop("type name does not split into camel words", group.typeName.Name())
		return false
	}
	lastOne := lastWords(words, 1)
	oneWord := lowerCamel(lastOne)
	if reason := disqualification(oneWord, lastOne, importNames, vocabulary); reason != "" {
		group.notes = append(group.notes, fmt.Sprintf("%q %s", oneWord, reason))
	} else {
		group.oneWord = oneWord
	}
	if len(words) >= 2 {
		lastTwo := lastWords(words, 2)
		twoWords := lowerCamel(lastTwo)
		reason := disqualification(twoWords, lastTwo, importNames, vocabulary)
		switch {
		case twoWords == oneWord:
			// A negation already brought the one-word name to these words (`NotFound`); there is no fallback.
		case reason != "":
			group.notes = append(group.notes, fmt.Sprintf("%q %s", twoWords, reason))
		default:
			group.twoWords = twoWords
		}
	}
	switch {
	case group.oneWord != "":
		group.state = oneWordState
	case group.twoWords != "":
		group.state = twoWordsState
	default:
		group.drop("no usable name", strings.Join(group.notes, "; "))
		return false
	}
	return true
}

// disqualification says why name cannot be a receiver name, or "" when it can. words are the camel words it
// was built from, each checked against the vocabulary.
func disqualification(name string, words []string, importNames map[string]bool, vocabulary map[string]bool) string {
	switch {
	case !token.IsIdentifier(name):
		return "is not an identifier"
	case token.IsKeyword(name):
		return "is a keyword"
	case types.Universe.Lookup(name) != nil:
		return "is predeclared"
	case importNames[name]:
		return "is the name of an import in a method's file"
	case confusingPackageNames[name]:
		return "is a standard library package name"
	case len([]rune(name)) <= 2:
		// A one- or two-letter name is what this pass replaces, so it never proposes one.
		return "has two letters or fewer"
	}
	for _, word := range words {
		if vocabulary[strings.ToLower(word)] {
			return fmt.Sprintf("holds the abbreviation %q", strings.ToLower(word))
		}
	}
	return ""
}

// isEligible reports whether a receiver name is one this pass replaces: one or two letters, or holding a
// camel word in the vocabulary.
func isEligible(name string, vocabulary map[string]bool) bool {
	if len([]rune(name)) <= 2 {
		return true
	}
	for _, word := range camelWords(name) {
		// A word in capitals is never the house's form (`memoryFS`), so a receiver holding one is renamed too.
		if vocabulary[strings.ToLower(word)] || isInitialism(word) {
			return true
		}
	}
	return false
}

// receiverTypeName is the declared type a receiver's method belongs to: the origin of a generic type, through
// a pointer and any alias.
func receiverTypeName(variable *types.Var) *types.TypeName {
	receiverType := types.Unalias(variable.Type())
	if pointer, isPointer := receiverType.(*types.Pointer); isPointer {
		receiverType = types.Unalias(pointer.Elem())
	}
	named, isNamed := receiverType.(*types.Named)
	if !isNamed {
		return nil
	}
	return named.Origin().Obj()
}

// fileImportNames is every name an import of file can go by: its alias, its package name, and its path base.
func fileImportNames(info *types.Info, file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, spec := range file.Imports {
		if spec.Name != nil && spec.Name.Name != "_" && spec.Name.Name != "." {
			names[spec.Name.Name] = true
		}
		var packageName *types.PkgName
		if spec.Name != nil {
			packageName, _ = info.Defs[spec.Name].(*types.PkgName)
		} else {
			packageName, _ = info.Implicits[spec].(*types.PkgName)
		}
		if packageName != nil {
			names[packageName.Imported().Name()] = true
		}
		if importPath, unquoteError := strconv.Unquote(spec.Path.Value); unquoteError == nil {
			names[path.Base(importPath)] = true
		}
	}
	return names
}

// camelWords splits an identifier into its camel words, keeping initialisms whole: `HTTPServer` is HTTP and
// Server, `parseJSON` is parse and JSON, `IDs` stays one word, and digits stay with the word before them. It
// returns nil for a name with an underscore, which this pass leaves alone.
func camelWords(name string) []string {
	if name == "" || strings.Contains(name, "_") {
		return nil
	}
	runes := []rune(name)
	var words []string
	start := 0
	for index := 1; index < len(runes); index++ {
		previous, current := runes[index-1], runes[index]
		if !unicode.IsUpper(current) {
			continue
		}
		boundary := false
		switch {
		case unicode.IsLower(previous) || unicode.IsDigit(previous):
			boundary = true
		case unicode.IsUpper(previous) && index+1 < len(runes) && unicode.IsLower(runes[index+1]):
			// The last capital of an initialism starts the next word (`HTTPServer`), unless what follows is
			// the initialism's plural `s` (`IDs`, `URLsFor`).
			pluralInitialism := runes[index+1] == 's' && (index+2 == len(runes) || unicode.IsUpper(runes[index+2]))
			boundary = !pluralInitialism
		}
		if boundary {
			words = append(words, string(runes[start:index]))
			start = index
		}
	}
	return append(words, string(runes[start:]))
}

// lastWords is the last count words, with a negation word directly before them joining them, so the name
// keeps its sense: `NotAllowedError` gives Not, Allowed, Error for two words, `cacheNotFound` Not, Found for one.
func lastWords(words []string, count int) []string {
	start := max(len(words)-count, 0)
	if start > 0 && negationWords[strings.ToLower(words[start-1])] {
		start--
	}
	return words[start:]
}

// initialismExpansions are initialisms the house spells out in full rather than word-casing (ruled on
// #paym4r4: `FS` is an abbreviation, so a file system receiver reads `fileSystem`).
var initialismExpansions = map[string]string{"FS": "FileSystem"}

// houseWord is one camel word as the house writes it in a name past its first position: an initialism with
// an expansion becomes the expansion, any other initialism is word-cased (`JSON` to `Json`, `IDs` to `Ids`,
// ruling 3), and an ordinary word is unchanged.
func houseWord(word string) string {
	if expansion, expands := initialismExpansions[strings.TrimSuffix(word, "s")]; expands {
		if strings.HasSuffix(word, "s") && word != strings.TrimSuffix(word, "s") {
			return expansion + "s"
		}
		return expansion
	}
	if isInitialism(word) {
		runes := []rune(strings.ToLower(word))
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	}
	return word
}

// isInitialism reports whether word is two or more capitals, with an optional plural `s` (`FS`, `JSON`, `IDs`).
func isInitialism(word string) bool {
	letters := strings.TrimSuffix(word, "s")
	if len([]rune(letters)) < 2 {
		return false
	}
	for _, letter := range letters {
		if !unicode.IsUpper(letter) {
			return false
		}
	}
	return true
}

// lowerCamel joins words as the house spells a local: every word in house form, and the first lowercased whole
// (`memoryFS` gives memoryFileSystem, `parseJSON` gives parseJson).
func lowerCamel(words []string) string {
	housed := make([]string, len(words))
	for index, word := range words {
		housed[index] = houseWord(word)
	}
	return strings.ToLower(housed[0][:1]) + housed[0][1:] + strings.Join(housed[1:], "")
}

// describeRename records one accepted rename with its declaring line before and after; the after line applies
// every edit that falls on it, so a use on the same line shows renamed too.
func describeRename(
	fileSet *token.FileSet,
	editsByFile map[string][]rename.Edit,
	group *receiverGroup,
	member receiverMember,
	newName string,
) receiverRename {
	position := fileSet.PositionFor(member.variable.Pos(), false)
	receiver := receiverRename{
		position: position,
		typeName: group.typeName.Name(),
		oldName:  member.variable.Name(),
		newName:  newName,
		kind:     "last word",
	}
	switch {
	case group.override:
		receiver.kind = "override"
	case group.state == twoWordsState:
		receiver.kind = "fallback"
	}
	if words := camelWords(newName); !group.override && len(words) > 1 && negationWords[strings.ToLower(words[0])] {
		receiver.kind += ", negation kept"
	}
	source, readError := os.ReadFile(position.Filename)
	if readError != nil || position.Offset > len(source) {
		return receiver
	}
	lineStart := strings.LastIndexByte(string(source[:position.Offset]), '\n') + 1
	lineEnd := strings.IndexByte(string(source[position.Offset:]), '\n')
	if lineEnd < 0 {
		lineEnd = len(source) - position.Offset
	}
	line := string(source[lineStart : position.Offset+lineEnd])
	receiver.lineBefore = strings.TrimSpace(line)
	edits := editsByFile[position.Filename]
	// Edits are sorted by offset; applying them from the end keeps the earlier offsets valid.
	for index := len(edits) - 1; index >= 0; index-- {
		column := edits[index].Offset - lineStart
		if column < 0 || column+len(edits[index].OldText) > len(line) {
			continue
		}
		line = line[:column] + edits[index].NewText + line[column+len(edits[index].OldText):]
	}
	receiver.lineAfter = strings.TrimSpace(line)
	return receiver
}

// apply hands the edits to workspace.ApplyVerified, which proves the rewrite in memory and writes only a
// proven result.
func apply(
	loaded *workspace.Workspace,
	root string,
	skips []string,
	edits []rename.Edit,
	editsByFile map[string][]rename.Edit,
) int {
	return workspace.ApplyVerified(
		loaded,
		root,
		skips,
		edits,
		editsByFile,
		"receivernames",
		streams.Output,
		streams.Errors,
	)
}

// positionLess orders positions by file, then offset.
func positionLess(left token.Position, right token.Position) bool {
	if left.Filename != right.Filename {
		return left.Filename < right.Filename
	}
	return left.Offset < right.Offset
}

// relativePosition prints a position relative to the root.
func relativePosition(loaded *workspace.Workspace, position token.Position) string {
	if relative, inside := loaded.Relative(position.Filename); inside {
		return fmt.Sprintf("%s:%d", relative, position.Line)
	}
	return position.String()
}

// sortedKeys returns a map's keys in order.
func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
