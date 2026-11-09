// Command contextnames renames each local `ctx` to a full name. A ctx of type context.Context that is written
// exactly once, at its own definition, from one call in a short table (matched through types, never by
// spelling) takes the name that call gives it: `context.WithTimeout` gives timeoutContext,
// `(*testing.T).Context` testContext, and so on; one from any other call is left for the waves. A
// context.Context parameter came from the caller, and is named for that: callerContext. The parameter's name
// and the call table are internal/naming/go_origin_names, which the Go engine's naming rule asks too, so its
// suggestion and this rename agree. A ctx parameter or variable whose type, after one pointer, is a named type
// of our own module ending in Context takes the type's name: `composeContext` stays composeContext, and an
// exact `Context` takes its package's name in front (`rule.Context` gives ruleContext). Every rename goes
// through the rename engine, which refuses any that would change what an identifier resolves to; after
// writing, the tree is reloaded and every identifier's resolution compared. Everything left alone is reported.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
	"github.com/system-inc/cohere/internal/naming/abbreviation"
	"github.com/system-inc/cohere/internal/naming/go_origin_names"
	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/policy"

	"golang.org/x/mod/modfile"
)

// commandName prefixes every message this command prints to standard error.
const commandName = "contextnames"

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code contextnames exits with: runCommandLine returns its exit code here, and
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

// runCommandLine reads the flags and runs contextnames, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print counts and every rejection, write nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	flag.Parse()
	return run(*root, skips, *dryRun)
}

// candidate is one local named ctx and what the generator decided for it.
type candidate struct {
	variable *types.Var
	// newName is the proposed name, or "" when the variable is not eligible.
	newName string
	// category says why the variable is not eligible, or "" when it is.
	category string
	// call is the producing call's function, qualified, when the definition has one; for a variable of the
	// wrong type, its type.
	call string
}

// writeSites records how the function bodies of some files write each variable.
type writeSites struct {
	count        map[*types.Var]int
	addressTaken map[*types.Var]bool
	definitions  map[*types.Var]definition
	// signature marks parameters, named results and receivers, by kind.
	signature map[*types.Var]string
}

// definition is the statement that declares a variable.
type definition struct {
	// values is the defining statement's right side.
	values []ast.Expr
	// fromRange marks a variable declared by a range clause.
	fromRange bool
}

// findWrites walks files and counts every write of every variable: assignment left sides (`=`, and a `:=`
// that reuses it, which go/types records in Uses), var declarations, increments, range keys and values. A
// write inside a closure is a write like any other. Taking the address marks the variable, since a write
// through the pointer is invisible here.
func findWrites(info *types.Info, files []*ast.File) writeSites {
	sites := writeSites{
		count:        map[*types.Var]int{},
		addressTaken: map[*types.Var]bool{},
		definitions:  map[*types.Var]definition{},
		signature:    map[*types.Var]string{},
	}
	variableOf := func(expression ast.Expr) (*types.Var, *ast.Ident) {
		identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
		if !isIdentifier {
			return nil, nil
		}
		variable, isVariable := info.ObjectOf(identifier).(*types.Var)
		if !isVariable {
			return nil, nil
		}
		return variable, identifier
	}
	markSignature := func(fields *ast.FieldList, kind string) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				if variable, isVariable := info.Defs[name].(*types.Var); isVariable {
					sites.signature[variable] = kind
				}
			}
		}
	}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				markSignature(typed.Recv, "receiver")
			case *ast.FuncType:
				markSignature(typed.Params, "parameter")
				markSignature(typed.Results, "named result")
			case *ast.AssignStmt:
				for _, left := range typed.Lhs {
					variable, identifier := variableOf(left)
					if variable == nil {
						continue
					}
					sites.count[variable]++
					if info.Defs[identifier] == variable {
						sites.definitions[variable] = definition{values: typed.Rhs}
					}
				}
			case *ast.ValueSpec:
				for _, name := range typed.Names {
					if variable, isVariable := info.Defs[name].(*types.Var); isVariable {
						sites.count[variable]++
						sites.definitions[variable] = definition{values: typed.Values}
					}
				}
			case *ast.IncDecStmt:
				if variable, _ := variableOf(typed.X); variable != nil {
					sites.count[variable]++
				}
			case *ast.RangeStmt:
				for _, expression := range []ast.Expr{typed.Key, typed.Value} {
					if expression == nil {
						continue
					}
					variable, identifier := variableOf(expression)
					if variable == nil {
						continue
					}
					sites.count[variable]++
					if info.Defs[identifier] == variable {
						sites.definitions[variable] = definition{fromRange: true}
					}
				}
			case *ast.UnaryExpr:
				if typed.Op == token.AND {
					if variable, _ := variableOf(typed.X); variable != nil {
						sites.addressTaken[variable] = true
					}
				}
			}
			return true
		})
	}
	return sites
}

// definingCall returns the single call a variable is defined from, or a category saying why there is none.
func definingCall(sites writeSites, variable *types.Var) (*ast.CallExpr, string) {
	if kind, inSignature := sites.signature[variable]; inSignature {
		return nil, kind
	}
	if sites.addressTaken[variable] {
		return nil, "address taken"
	}
	if sites.count[variable] > 1 {
		return nil, "written more than once"
	}
	declaration, defined := sites.definitions[variable]
	switch {
	case !defined:
		return nil, "not defined by an assignment or var declaration"
	case declaration.fromRange:
		return nil, "defined by a range clause"
	case len(declaration.values) == 0:
		return nil, "declared without a value"
	case len(declaration.values) > 1:
		return nil, "right side holds more than one value"
	}
	call, isCall := ast.Unparen(declaration.values[0]).(*ast.CallExpr)
	if !isCall {
		return nil, "right side is not a call"
	}
	return call, ""
}

// parseVocabulary reads policy/Abbreviations.json through the shared vocabulary, into the words a context's
// name is built from.
func parseVocabulary(contents []byte) (go_origin_names.Vocabulary, error) {
	loaded, loadError := abbreviation.Load(contents, nil)
	if loadError != nil {
		return go_origin_names.Vocabulary{}, loadError
	}
	words := go_origin_names.VocabularyOf(loaded)
	if len(words.Expansions) == 0 {
		return go_origin_names.Vocabulary{}, fmt.Errorf("the vocabulary holds no expansions")
	}
	return words, nil
}

// fullWordsLowerCamel is name in lowerCamel with every vocabulary word expanded. It returns "" and a category
// when a word has no mechanical expansion.
func fullWordsLowerCamel(name string, words go_origin_names.Vocabulary) (string, string) {
	var builder strings.Builder
	for index, word := range go_origin_names.CamelWords(name) {
		lowered := strings.ToLower(word)
		if words.AdviceWords[lowered] {
			return "", "advice-only word in the type name"
		}
		if words.MatcherWords[lowered] {
			return "", "matcher-judged word (ms) in the type name"
		}
		expansion, abbreviated := words.Expansions[lowered]
		switch {
		case abbreviated && index == 0:
			// An expansion is already lowerCamel (`workingDirectory`).
			word = expansion
		case abbreviated:
			word = strings.ToUpper(expansion[:1]) + expansion[1:]
		case index == 0:
			// A plain first word lowers whole, initialism or not: `YAMLContext` is `yamlContext`.
			word = lowered
		}
		builder.WriteString(word)
	}
	return builder.String(), ""
}

// ownContextType is the named type a variable's type is, after stripping one pointer, when that type belongs
// to ownModule and its name ends in Context.
func ownContextType(variable *types.Var, ownModule string) (*types.TypeName, bool) {
	variableType := types.Unalias(variable.Type())
	if pointer, isPointer := variableType.(*types.Pointer); isPointer {
		variableType = types.Unalias(pointer.Elem())
	}
	named, isNamed := variableType.(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil || !strings.HasSuffix(named.Obj().Name(), "Context") {
		return nil, false
	}
	packagePath := named.Obj().Pkg().Path()
	if packagePath != ownModule && !strings.HasPrefix(packagePath, ownModule+"/") {
		return nil, false
	}
	return named.Obj(), true
}

// contextOverrides names the ctx of an own Context type whose derived name would be the type's own name, which
// a parameter cannot take without shadowing the type. Keyed by the package path relative to the module, and
// applied before the type-derived name.
var contextOverrides = map[go_origin_names.TypeName]string{
	{PackagePath: "internal/format/yaml/compose", Name: "composeContext"}:   "composition",
	{PackagePath: "internal/lint/ecmascript/regexp", Name: "escapeContext"}: "escaping",
}

// ownContextName names a ctx of one of our own Context types: the override for its type when there is one,
// else lowerCamel of the type name in full words, with the package name (in its QualifierSpelling) in
// front when the type is named exactly Context.
func ownContextName(typeObject *types.TypeName, ownModule string, words go_origin_names.Vocabulary) (string, string) {
	relativePath := strings.TrimPrefix(strings.TrimPrefix(typeObject.Pkg().Path(), ownModule), "/")
	overrideKey := go_origin_names.TypeName{PackagePath: relativePath, Name: typeObject.Name()}
	if override, found := contextOverrides[overrideKey]; found {
		return override, ""
	}
	base := typeObject.Name()
	if base == "Context" {
		qualifier := typeObject.Pkg().Name()
		if camel, known := go_origin_names.QualifierSpelling(qualifier); known {
			qualifier = camel
		}
		base = qualifier + "Context"
	}
	name, category := fullWordsLowerCamel(base, words)
	if name == "" {
		return "", category
	}
	if token.IsKeyword(name) || types.Universe.Lookup(name) != nil {
		return "", "name would be a keyword or predeclared identifier"
	}
	return name, ""
}

// findCandidates decides, for every local named ctx declared in files, its new name or why it has none.
func findCandidates(
	packageTypes *types.Package,
	info *types.Info,
	files []*ast.File,
	ownModule string,
	words go_origin_names.Vocabulary,
) []candidate {
	sites := findWrites(info, files)
	var candidates []candidate
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier || identifier.Name != "ctx" {
				return true
			}
			variable, isVariable := info.Defs[identifier].(*types.Var)
			if !isVariable || !rename.IsLocal(variable, packageTypes) {
				return true
			}
			decided := candidate{variable: variable}
			decided.newName, decided.category, decided.call = contextName(info, sites, variable, ownModule, words)
			candidates = append(candidates, decided)
			return true
		})
	}
	return candidates
}

// contextName is one ctx's new name, or the category that keeps it, and the producing call (or, for one of
// our own Context types, the type) when there is one.
func contextName(
	info *types.Info,
	sites writeSites,
	variable *types.Var,
	ownModule string,
	words go_origin_names.Vocabulary,
) (string, string, string) {
	if typeObject, isOwn := ownContextType(variable, ownModule); isOwn {
		typeString := variable.Type().String()
		switch sites.signature[variable] {
		case "receiver":
			return "", "own Context type: receiver, left for the receiver pass", typeString
		case "named result":
			return "", "own Context type: named result", typeString
		}
		newName, category := ownContextName(typeObject, ownModule, words)
		return newName, category, typeString
	}
	if !go_origin_names.IsContext(variable.Type()) {
		return "", "type is neither context.Context nor an own Context type", variable.Type().String()
	}
	if sites.signature[variable] == "parameter" {
		return go_origin_names.CallerContextName, "", "a parameter"
	}
	call, category := definingCall(sites, variable)
	if call == nil {
		return "", category, ""
	}
	return go_origin_names.ContextCallName(info, call)
}

// declarationRecord is one candidate's outcome, keyed by where it is declared, so a file seen by two
// package variants counts once.
type declarationRecord struct {
	position string
	filename string
	offset   int
	oldName  string
	newName  string
	category string
	call     string
	// rejection is the engine's reason class, when it refused the proposal.
	rejection string
	reason    string
}

// declaredAt finds the position an engine conflict reason names as the other declaration.
var declaredAt = regexp.MustCompile(`declared at (\S+) instead of`)

// insteadOf finds the old name of the object an engine conflict reason says the occurrence belongs to.
var insteadOf = regexp.MustCompile(`instead of "([^"]*)"$`)

func run(root string, skips []string, dryRun bool) int {
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(streams.Errors, "%s: loading: %v\n", commandName, loadError)
		return 1
	}
	// The vocabulary is the one built into the pass, from the cohere tree it was built in, so a pass run over another
	// repository judges words exactly as it does over cohere.
	words, vocabularyError := parseVocabulary(policy.AbbreviationsFile())
	if vocabularyError != nil {
		fmt.Fprintf(streams.Errors, "%s: policy/Abbreviations.json: %v\n", commandName, vocabularyError)
		return 1
	}
	moduleFile, moduleReadError := os.ReadFile(filepath.Join(loaded.Root, "go.mod"))
	if moduleReadError != nil {
		fmt.Fprintf(streams.Errors, "%s: %v\n", commandName, moduleReadError)
		return 1
	}
	ownModule := modfile.ModulePath(moduleFile)
	if ownModule == "" {
		fmt.Fprintf(streams.Errors, "%s: go.mod at the root names no module\n", commandName)
		return 1
	}
	fmt.Fprintf(streams.Output, "own module: %s\n", ownModule)
	fmt.Fprintf(
		streams.Output,
		"loaded %d root packages, analyzing %d variants\n",
		len(loaded.Roots),
		len(loaded.Analyzed),
	)
	// The analysis takes a while on a whole worktree, so the load's count is seen before it.
	streams.Show(streams.Output)
	editableFiles, skippedFiles := 0, 0
	for _, analyzedPackage := range loaded.Analyzed {
		editable := len(loaded.RenamePackage(analyzedPackage).Files)
		editableFiles += editable
		skippedFiles += len(analyzedPackage.Syntax) - editable
	}
	fmt.Fprintf(streams.Output, "files in analyzed variants: %d editable, %d skipped\n", editableFiles, skippedFiles)
	if editableFiles == 0 {
		fmt.Fprintf(streams.Errors, "%s: no editable files; the root or skip rules are wrong\n", commandName)
		return 1
	}

	var allEdits []rename.Edit
	var allUnresolved []string
	records := map[string]*declarationRecord{}
	var recordOrder []string
	for _, analyzedPackage := range loaded.Analyzed {
		renamePackage := loaded.RenamePackage(analyzedPackage)
		candidates := findCandidates(
			analyzedPackage.Types,
			analyzedPackage.TypesInfo,
			renamePackage.Files,
			ownModule,
			words,
		)
		var proposals []rename.Proposal
		proposedAt := map[string]string{}
		for _, decided := range candidates {
			position := analyzedPackage.Fset.PositionFor(decided.variable.Pos(), false)
			key := fmt.Sprintf("%s:%d", position.Filename, position.Offset)
			if _, seen := records[key]; !seen {
				records[key] = &declarationRecord{
					position: relativePosition(loaded, position),
					filename: position.Filename,
					offset:   position.Offset,
					oldName:  decided.variable.Name(),
					newName:  decided.newName,
					category: decided.category,
					call:     decided.call,
				}
				recordOrder = append(recordOrder, key)
			}
			if decided.newName != "" {
				proposals = append(proposals, rename.Proposal{Object: decided.variable, NewName: decided.newName})
				proposedAt[analyzedPackage.Fset.Position(decided.variable.Pos()).String()] = decided.newName
			}
		}
		plan := rename.Check(renamePackage, proposals)
		for _, unresolved := range plan.Unresolved {
			allUnresolved = append(allUnresolved, analyzedPackage.ID+": "+unresolved)
		}
		for _, rejection := range plan.Rejections {
			position := analyzedPackage.Fset.PositionFor(rejection.Object.Pos(), false)
			record, found := records[fmt.Sprintf("%s:%d", position.Filename, position.Offset)]
			if !found || record.rejection != "" {
				continue
			}
			record.rejection = reasonClass(
				rejection.Reason,
				rejection.NewName,
				proposedAt,
				map[string]bool{"ctx": true},
			)
			record.reason = strings.ReplaceAll(rejection.Reason, loaded.Root+"/", "")
		}
		allEdits = append(allEdits, rename.Edits(renamePackage, plan.Accepted)...)
	}

	edits, disagreements := rename.Deduplicate(allEdits)
	editsByFile := rename.GroupByFile(edits)

	candidateCount, eligibleCount, acceptedCount := 0, 0, 0
	rejectedByReason := map[string]int{}
	notEligibleByCategory := map[string]int{}
	acceptedNames := map[string]int{}
	notEligibleCalls := map[string]int{}
	for _, key := range recordOrder {
		record := records[key]
		candidateCount++
		switch {
		case record.newName == "":
			notEligibleByCategory[record.category]++
			if record.call != "" {
				notEligibleCalls[record.call]++
			}
		case record.rejection != "":
			eligibleCount++
			rejectedByReason[record.rejection]++
		default:
			eligibleCount++
			acceptedCount++
			acceptedNames[record.newName]++
		}
	}
	fmt.Fprintf(streams.Output, "candidates (locals named ctx): %d\n", candidateCount)
	fmt.Fprintf(streams.Output, "eligible (proposed): %d\n", eligibleCount)
	fmt.Fprintf(streams.Output, "accepted by the engine: %d\n", acceptedCount)
	fmt.Fprintf(streams.Output, "rejected by the engine: %d\n", eligibleCount-acceptedCount)
	printCounts(rejectedByReason, 0)
	fmt.Fprintf(streams.Output, "not eligible: %d\n", candidateCount-eligibleCount)
	printCounts(notEligibleByCategory, 0)
	fmt.Fprintln(streams.Output, "most frequent new names (accepted):")
	printCounts(acceptedNames, 15)
	fmt.Fprintln(streams.Output, "most frequent calls (or variable types) among the not eligible:")
	printCounts(notEligibleCalls, 15)
	fmt.Fprintf(streams.Output, "edits: %d in %d files\n", len(edits), len(editsByFile))
	fmt.Fprintf(streams.Output, "variant disagreements: %d\n", len(disagreements))
	fmt.Fprintf(streams.Output, "unresolved occurrences (scope model gaps): %d\n", len(allUnresolved))

	fmt.Fprintln(streams.Output, "\nrejections:")
	for _, key := range recordOrder {
		if record := records[key]; record.newName != "" && record.rejection != "" {
			fmt.Fprintf(
				streams.Output,
				"  %s: %s -> %s: %s\n",
				record.position,
				record.oldName,
				record.newName,
				record.reason,
			)
		}
	}
	fmt.Fprintln(streams.Output, "\nnot eligible:")
	for _, key := range recordOrder {
		if record := records[key]; record.newName == "" {
			fmt.Fprintf(
				streams.Output,
				"  %s: %s: %s %s\n",
				record.position,
				record.oldName,
				record.category,
				record.call,
			)
		}
	}
	fmt.Fprintln(streams.Output, "\naccepted (declaring line before | after):")
	for _, key := range recordOrder {
		if record := records[key]; record.newName != "" && record.rejection == "" {
			before, after := lineBeforeAndAfter(record.filename, record.offset, editsByFile[record.filename])
			fmt.Fprintf(
				streams.Output,
				"  %s: %s -> %s [%s]\n      %s\n    | %s\n",
				record.position,
				record.oldName,
				record.newName,
				record.call,
				before,
				after,
			)
		}
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
		fmt.Fprintf(streams.Errors, "%s: variants disagree on some edits; those spans were dropped\n", commandName)
	}
	if dryRun {
		return 0
	}
	return apply(loaded, root, skips, edits, editsByFile)
}

// reasonClass groups rejection reasons for counting, telling a collision between two proposed renames of the
// same new name apart from a conflict with a name already in the code. A collision is an occurrence of one
// candidate (still under its old name) that would resolve to another proposed rename; an occurrence of
// anything else that would resolve to a proposed rename is that rename shadowing a name already in scope.
func reasonClass(reason string, newName string, proposedAt map[string]string, oldNames map[string]bool) string {
	if !strings.HasPrefix(reason, "conflict:") {
		return reason
	}
	declared := declaredAt.FindStringSubmatch(reason)
	displaced := insteadOf.FindStringSubmatch(reason)
	if declared != nil && proposedAt[declared[1]] == newName && displaced != nil && oldNames[displaced[1]] {
		return "conflict with another proposed rename of the same name"
	}
	return "conflict with a name already in scope"
}

// printCounts prints counts largest first, at most limit of them when limit is positive.
func printCounts(counts map[string]int, limit int) {
	keys := sortedKeys(counts)
	sort.SliceStable(keys, func(left, right int) bool { return counts[keys[left]] > counts[keys[right]] })
	for index, key := range keys {
		if limit > 0 && index == limit {
			break
		}
		fmt.Fprintf(streams.Output, "  %5d  %s\n", counts[key], key)
	}
}

// lineBeforeAndAfter returns the source line holding offset, as it is and with the edits that fall on it.
func lineBeforeAndAfter(filename string, offset int, edits []rename.Edit) (string, string) {
	source, readError := os.ReadFile(filename)
	if readError != nil {
		return readError.Error(), ""
	}
	lineStart := strings.LastIndexByte(string(source[:offset]), '\n') + 1
	lineEnd := len(source)
	if newline := strings.IndexByte(string(source[offset:]), '\n'); newline >= 0 {
		lineEnd = offset + newline
	}
	line := string(source[lineStart:lineEnd])
	after := line
	for index := len(edits) - 1; index >= 0; index-- {
		edit := edits[index]
		if edit.Offset < lineStart || edit.Offset+len(edit.OldText) > lineEnd {
			continue
		}
		relative := edit.Offset - lineStart
		after = after[:relative] + edit.NewText + after[relative+len(edit.OldText):]
	}
	return strings.TrimSpace(line), strings.TrimSpace(after)
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
		"contextnames",
		streams.Output,
		streams.Errors,
	)
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
