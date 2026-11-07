package main

// The -global pass: every name the rule flags whose uses reach past one function, functions, methods, fields, types,
// constants and package variables, and the locals and parameters holding an advice-only word (args, arg, fn), which
// the local pass leaves as waves. Every rename goes through the global rename engine, which follows a name across
// packages and refuses what would change an interface's satisfaction, an encoder's key, or a resolution, and the
// write is proven by an overlay reload before anything is written. A name that spells an upstream rule or an outside
// name is listed, unless -rename-owned-spellings asks for it too: the Go identifier is ours, and only the string a
// rule registers under, or the outside name a doc comment cites, is someone else's.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/tools/go_codemods/global_rename"
	"github.com/system-inc/cohere/tools/go_codemods/rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"

	"golang.org/x/tools/go/packages"
)

// adviceWords are the vocabulary's words with no single expansion, which this pass names by type and role.
var adviceWords = map[string]bool{"args": true, "arg": true, "fn": true}

// listedName is a flagged name the pass does not propose, and why.
type listedName struct {
	position token.Position
	kind     string
	name     string
	reason   string
}

// Reasons a flagged name is listed rather than proposed.
const (
	reasonOtherPass      = "holds err or ctx, which the error and context passes name"
	reasonLocalPass      = "a local without an advice word, which the local pass names"
	reasonTypeSwitch     = "a type switch's bound name, which no engine renames"
	reasonUpstreamRule   = "spells the upstream rule "
	reasonExternalPrefix = "spells an external name: "
	reasonNoNote         = "spells an external name no doc comment of its own names, and external.go writes it no note"
	reasonNoteOpening    = "its note would open its new doc comment, and does not open with the new name "
)

func runGlobal(root string, vocabularyPath string, skips []string, dryRun bool, renameOwnedSpellings bool,
	libraries []string, options global_rename.Options) int {
	vocabularyData, readError := readVocabulary(vocabularyPath)
	if readError != nil {
		fmt.Fprintf(streams.Errors, "wordnames: reading the vocabulary: %v\n", readError)
		return 1
	}
	loadedVocabulary, vocabularyError := loadVocabulary(vocabularyData)
	if vocabularyError != nil {
		fmt.Fprintf(streams.Errors, "wordnames: %s: %v\n", vocabularyPath, vocabularyError)
		return 1
	}
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(streams.Errors, "wordnames: loading: %v\n", loadError)
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
	pinned, pinnedError := libraryPackagePaths(loaded, libraries)
	if pinnedError != nil {
		fmt.Fprintf(streams.Errors, "wordnames: -rename-library-exports: %v\n", pinnedError)
		return 2
	}
	options.PinnedLibraries = pinned
	for _, library := range libraries {
		fmt.Fprintf(streams.Output, "library exports renamable: %s\n", library)
	}

	candidates := findGlobalCandidates(loaded, loadedVocabulary, externalNames, renameOwnedSpellings)
	fmt.Fprintf(
		streams.Output,
		"flagged names: %d proposed, %d listed (%d names in generated files left, as the analyzer leaves them)\n",
		len(candidates.proposals),
		len(candidates.listed),
		candidates.generatedSkipped,
	)
	options.ReviewedLiterals = reviewedLiterals
	result := global_rename.Plan(loaded, candidates.proposals, options)
	respellings := keepExternalSpellings(result, candidates.externals)
	citations := citationEdits(loaded, result, candidates.externals)
	reflowed, leftLong := reflowLongComments(loaded, result)
	moves := planFileMoves(loaded, result, candidates.proposals)
	reportGlobal(loaded, candidates.listed, result, respellings)
	fmt.Fprintf(streams.Output, "\ncomment citations of an old name, respelled: %d\n", citations)
	fmt.Fprintf(
		streams.Output,
		"comment paragraphs a respelling made too long, reflowed: %d, lines left long: %d\n",
		reflowed,
		leftLong,
	)
	reportMoves(loaded, moves)
	if dryRun {
		return 0
	}
	// The write reloads the whole workspace to prove it, so the plan is seen before that wait.
	streams.Show(streams.Output)
	mismatches, applyError := global_rename.Apply(loaded, root, skips, result, streams.Output)
	if applyError != nil {
		fmt.Fprintf(streams.Errors, "wordnames: %v\n", applyError)
		return 1
	}
	if mismatches > 0 {
		return 1
	}
	if moveError := moveFiles(moves); moveError != nil {
		fmt.Fprintf(streams.Errors, "wordnames: the renames are written, and %v\n", moveError)
		return 1
	}
	return 0
}

// libraryPackagePaths finds each named library package, a directory relative to the root, among the loaded
// packages, and pins it with the flag's reason. A name matching no package, or one under internal/, which needs no
// pin, is refused, so a mistyped name cannot leave a library's exports silently refused.
func libraryPackagePaths(loaded *workspace.Workspace, libraries []string) (map[string]string, error) {
	pinned := map[string]string{}
	for _, library := range libraries {
		library = strings.Trim(strings.TrimSpace(library), "/")
		found := false
		for _, rootsPackage := range loaded.Roots {
			if len(rootsPackage.GoFiles) == 0 || strings.HasSuffix(rootsPackage.Name, "_test") {
				continue
			}
			directory, inside := loaded.Relative(filepath.Dir(rootsPackage.GoFiles[0]))
			if !inside || directory != library {
				continue
			}
			if strings.Contains("/"+directory+"/", "/internal/") {
				return nil, fmt.Errorf(
					"%s is under internal/, whose exports nothing outside the repository can import",
					library,
				)
			}
			pinned[rootsPackage.PkgPath] =
				"named by -rename-library-exports: every consumer outside the repository reads it at a pin"
			found = true
		}
		if !found {
			return nil, fmt.Errorf("no loaded package lives in %s", library)
		}
	}
	return pinned, nil
}

// reportMoves prints each file the pass moves with its type, old and new paths apart by a tab as the gate's
// renames.tsv reads them, and each move it refuses.
func reportMoves(loaded *workspace.Workspace, moves []fileMove) {
	relative := func(path string) string {
		if inside, isInside := loaded.Relative(path); isInside {
			return inside
		}
		return path
	}
	refused := 0
	for _, move := range moves {
		if move.refusal != "" {
			refused++
		}
	}
	fmt.Fprintf(streams.Output, "\nfile moves: %d, %d refused\n", len(moves)-refused, refused)
	for _, move := range moves {
		if move.refusal != "" {
			fmt.Fprintf(streams.Output, "  refused %s (%s): %s\n", relative(move.oldPath), move.typeName, move.refusal)
			continue
		}
		fmt.Fprintf(
			streams.Output,
			"  move\t%s\t%s\t(%s)\n",
			relative(move.oldPath),
			relative(move.newPath),
			move.typeName,
		)
	}
}

// globalCandidates are the names the pass proposes, those it lists with the reason, and for each proposed external
// name, by its declaration's file and offset, what its doc comment keeps.
type globalCandidates struct {
	proposals        []global_rename.Proposal
	listed           []listedName
	generatedSkipped int
	externals        map[string]plannedExternal
}

// findGlobalCandidates proposes a full-word name for every name the rule flags that this pass owns, once per
// declaration across variants, and lists the rest with the reason. It reads the analyzed variants, which hold
// every file. A name spelling an upstream rule or one of externals is proposed only when renameOwnedSpellings
// asks, and an external name only when its doc comment will name the outside spelling after the rename.
func findGlobalCandidates(loaded *workspace.Workspace, vocabulary *vocabulary, externals map[string]externalName,
	renameOwnedSpellings bool) globalCandidates {
	ruleSpans := upstreamRuleSpans(loaded)
	candidates := globalCandidates{externals: map[string]plannedExternal{}}
	seen := map[string]bool{}
	for _, analyzedPackage := range loaded.Analyzed {
		reachable := reachOf(analyzedPackage.Types)
		for _, file := range analyzedPackage.Syntax {
			filename := analyzedPackage.Fset.PositionFor(file.Package, false).Filename
			relative, inside := loaded.Relative(filename)
			if !inside {
				continue
			}
			generated := ast.IsGenerated(file)
			importNames := map[string]bool{}
			for _, specification := range file.Imports {
				if packageName := analyzedPackage.TypesInfo.PkgNameOf(specification); packageName != nil {
					importNames[packageName.Name()] = true
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				var identifier *ast.Ident
				var object types.Object
				switch declaring := node.(type) {
				case *ast.TypeSwitchStmt:
					assignment, isAssignment := declaring.Assign.(*ast.AssignStmt)
					if !isAssignment || len(assignment.Lhs) != 1 {
						return true
					}
					identifier, _ = assignment.Lhs[0].(*ast.Ident)
				case *ast.Ident:
					identifier = declaring
					object = analyzedPackage.TypesInfo.Defs[declaring]
					if object == nil || !isOurs(reachable, object) {
						return true
					}
				default:
					return true
				}
				if identifier == nil || identifier.Name == "_" {
					return true
				}
				external, isExternal := externals[relative+":"+identifier.Name]
				_, flagged := vocabulary.find(identifier.Name)
				// A table name the vocabulary does not flag, an initialism's, is the table's to propose, and so is a
				// name an override renames to keep it paired with a renamed name beside it.
				_, overridden := nameOverrides[relative+":"+identifier.Name]
				byTable := renameOwnedSpellings && ((isExternal && external.newName != "") || (overridden && !flagged))
				if !flagged && !byTable {
					return true
				}
				if generated {
					candidates.generatedSkipped++
					return true
				}
				position := analyzedPackage.Fset.PositionFor(identifier.Pos(), false)
				identity := fmt.Sprintf("%s:%d", position.Filename, position.Offset)
				if seen[identity] {
					return true
				}
				seen[identity] = true
				list := func(kind string, reason string) {
					candidates.listed = append(
						candidates.listed,
						listedName{position: position, kind: kind, name: identifier.Name, reason: reason},
					)
				}
				if object == nil {
					list("type switch guard", reasonTypeSwitch)
					return true
				}
				local := rename.IsLocal(object, analyzedPackage.Types)
				kind := nonLocalKind(object, analyzedPackage.Types)
				if local {
					kind = "local"
				}
				newName, reason := "", ""
				if flagged {
					newName, reason = globalName(vocabulary, identifier.Name, object, local)
				}
				if chosen, overridden := nameOverrides[relative+":"+identifier.Name]; overridden {
					newName, reason = chosen, ""
				}
				if byTable && external.newName != "" {
					newName, reason = external.newName, ""
				}
				owned := ""
				switch {
				case isExternal:
					owned = reasonExternalPrefix + external.reason
				case ruleSpans.spelledIn(vocabulary, identifier.Name) != "":
					owned = reasonUpstreamRule + ruleSpans.spelledIn(vocabulary, identifier.Name)
				}
				var planned plannedExternal
				if isExternal && reason == "" && renameOwnedSpellings {
					planned = planExternal(analyzedPackage.Fset, file, identifier, external, newName)
				}
				switch {
				case reason != "":
					list(kind, reason)
				case owned != "" && !renameOwnedSpellings:
					list(kind, owned)
				case planned.refusal != "":
					list(kind, planned.refusal)
				case local && importNames[newName]:
					list(kind, "would become the name of an import in the file")
				default:
					candidates.proposals = append(
						candidates.proposals,
						global_rename.Proposal{Package: analyzedPackage, Object: object, NewName: newName},
					)
					if isExternal {
						candidates.externals[identity] = planned
					}
				}
				return true
			})
		}
	}
	return candidates
}

// globalName is the full-word name for a flagged name, or the reason it has none here. It expands the name the
// way the rule reads it, word by word, until the rule finds nothing; an advice-only word takes the name its type
// and role give (see adviceName). A local takes part only for an advice word: its other words are the local
// pass's.
func globalName(vocabulary *vocabulary, name string, object types.Object, local bool) (string, string) {
	current := name
	usedAdvice := false
	for range maximumExpansions {
		found, matched := vocabulary.find(current)
		if !matched {
			break
		}
		word := found.entry.Abbreviation
		if word == "err" || word == "ctx" {
			return "", reasonOtherPass
		}
		next := found.suggestion
		if adviceWords[word] {
			usedAdvice = true
			next = adviceName(found, current, object)
		}
		if next == "" || next == current {
			return "", fmt.Sprintf("the %s form of %q gives advice this pass has no rule for", found.form, word)
		}
		current = next
	}
	if _, stillFlagged := vocabulary.find(current); stillFlagged {
		return "", fmt.Sprintf("still flagged after %d expansions", maximumExpansions)
	}
	if local && !usedAdvice {
		return "", reasonLocalPass
	}
	switch {
	case token.IsKeyword(current):
		return "", "would become a Go keyword"
	case types.Universe.Lookup(current) != nil:
		return "", "would become a predeclared identifier"
	}
	return current, ""
}

// adviceName is the ruling for the advice-only words, by form, and for fn by what the value is.
//
// args is a list of arguments, `arguments`, unless it is a []string, which in this code is always a command line,
// `commandArguments`, the vocabulary's own second choice. arg is one of them, `argument`. As a prefix or suffix
// each reads as its expansion, argType as argumentType and couldExpandArg as couldExpandArgument.
//
// fn names a function. Where the value is not a Go function but a function object, a JavaScript function's syntax
// node, a *types.Func, a lowered function, the kind is the job, so it reads `function`, and fnName `functionName`.
// Where it is a Go function value, the vocabulary asks for its role: a filepath.WalkFunc or fs.WalkDirFunc is a
// visit, a function answering one bool a predicate, one returning what it takes a transform, and anything else a
// callback. A suffix Fn is dropped, as the vocabulary says, parseFn reading parse, unless the value is a walk
// function, which reads visit at the end of its name too.
func adviceName(found finding, name string, object types.Object) string {
	word := found.entry.Abbreviation
	switch word {
	case "args", "arg":
		expansion := found.entry.singleExpansion()
		switch found.form {
		case "whole":
			if word == "args" && isStringSlice(object.Type()) {
				return "commandArguments"
			}
			return expansion
		case "prefix":
			return expansion + strings.TrimPrefix(name, word)
		case "suffix":
			return strings.TrimSuffix(name, capitalize(word)) + capitalize(expansion)
		}
	case "fn":
		signature, isFunction := object.Type().Underlying().(*types.Signature)
		if _, isDeclaredFunction := object.(*types.Func); isDeclaredFunction {
			isFunction = false
		}
		switch found.form {
		case "whole":
			if !isFunction {
				return "function"
			}
			return functionRole(object.Type(), signature)
		case "prefix":
			return "function" + strings.TrimPrefix(name, word)
		case "suffix":
			stem := strings.TrimSuffix(name, "Fn")
			if isFunction && isWalkFunction(object.Type()) {
				return stem + "Visit"
			}
			return stem
		}
	}
	return ""
}

// functionRole is a Go function value's role, read from its type.
func functionRole(goType types.Type, signature *types.Signature) string {
	if isWalkFunction(goType) {
		return "visit"
	}
	results := signature.Results()
	if results.Len() == 1 && types.Identical(results.At(0).Type(), types.Typ[types.Bool]) {
		return "predicate"
	}
	parameters := signature.Params()
	if parameters.Len() == 1 && results.Len() == 1 && types.Identical(parameters.At(0).Type(), results.At(0).Type()) {
		return "transform"
	}
	return "callback"
}

// isWalkFunction is whether a type is the standard library's walk callback, filepath.WalkFunc or fs.WalkDirFunc.
func isWalkFunction(goType types.Type) bool {
	named, isNamed := types.Unalias(goType).(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil {
		return false
	}
	path, name := named.Obj().Pkg().Path(), named.Obj().Name()
	return (path == "path/filepath" && name == "WalkFunc") || (path == "io/fs" && name == "WalkDirFunc")
}

func isStringSlice(goType types.Type) bool {
	slice, isSlice := goType.Underlying().(*types.Slice)
	return isSlice && types.Identical(slice.Elem(), types.Typ[types.String])
}

// upstreamRuleSpans reads every rule.Rule composite literal's Name in the root packages.
func upstreamRuleSpans(loaded *workspace.Workspace) upstreamRules {
	rules := upstreamRules{spans: map[string]string{}}
	for _, rootsPackage := range loaded.Roots {
		for _, file := range rootsPackage.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				literal, isLiteral := node.(*ast.CompositeLit)
				if !isLiteral || !isRuleType(rootsPackage, literal) {
					return true
				}
				for _, element := range literal.Elts {
					keyValue, isKeyValue := element.(*ast.KeyValueExpr)
					if !isKeyValue {
						continue
					}
					key, isIdentifier := keyValue.Key.(*ast.Ident)
					nameLiteral, isBasic := keyValue.Value.(*ast.BasicLit)
					if !isIdentifier || key.Name != "Name" || !isBasic || nameLiteral.Kind != token.STRING {
						continue
					}
					ruleName, unquoteError := strconv.Unquote(nameLiteral.Value)
					if unquoteError != nil {
						continue
					}
					rules.add(ruleName)
				}
				return true
			})
		}
	}
	return rules
}

// isRuleType is whether a composite literal builds the lint rule.Rule.
func isRuleType(goPackage *packages.Package, literal *ast.CompositeLit) bool {
	named, isNamed := types.Unalias(goPackage.TypesInfo.TypeOf(literal)).(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == "Rule" && strings.HasSuffix(named.Obj().Pkg().Path(), "/internal/lint/rule")
}

// reportGlobal prints the counts, then every list a reviewer needs.
func reportGlobal(
	loaded *workspace.Workspace,
	listed []listedName,
	result *global_rename.Result,
	respellings []externalRespelling,
) {
	relative := func(position token.Position) string {
		if path, inside := loaded.Relative(position.Filename); inside {
			return fmt.Sprintf("%s:%d", path, position.Line)
		}
		return position.String()
	}
	trim := func(text string) string { return strings.ReplaceAll(text, loaded.Root+"/", "") }
	byReason := map[string]int{}
	for _, name := range listed {
		reason := name.reason
		if strings.HasPrefix(reason, reasonUpstreamRule) {
			reason = reasonUpstreamRule + "..."
		}
		if strings.HasPrefix(reason, reasonExternalPrefix) {
			reason = reasonExternalPrefix + "..."
		}
		byReason[reason]++
	}
	fmt.Fprintf(streams.Output, "listed, not proposed: %d\n", len(listed))
	for _, reason := range sortedKeys(byReason) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", reason, byReason[reason])
	}
	acceptedByKind := map[string]int{}
	for _, accepted := range result.Accepted {
		acceptedByKind[accepted.Kind]++
	}
	fmt.Fprintf(streams.Output, "accepted: %d\n", len(result.Accepted))
	for _, kind := range sortedKeys(acceptedByKind) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", kind, acceptedByKind[kind])
	}
	byClass := map[string]int{}
	for _, rejection := range result.Rejections {
		byClass[rejection.Class]++
	}
	fmt.Fprintf(streams.Output, "refused: %d\n", len(result.Rejections))
	for _, class := range sortedKeys(byClass) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", class, byClass[class])
	}
	editFiles := map[string]bool{}
	for _, edit := range result.Edits {
		editFiles[edit.Filename] = true
	}
	fmt.Fprintf(
		streams.Output,
		"edits: %d identifiers in %d files, %d tags added, %d comment respellings, %d in platform twins outside the load\n",
		len(result.Edits),
		len(editFiles),
		len(result.TagEdits),
		len(result.CommentEdits),
		len(result.TwinEdits),
	)
	fmt.Fprintf(
		streams.Output,
		"model checks: %d unresolved scope walks, %d selection mismatches, %d satisfaction mismatches\n",
		len(result.Unresolved),
		len(result.SelectionModelMismatches),
		len(result.SatisfactionModelMismatches),
	)
	byOutcome := map[string]int{}
	for _, respelling := range respellings {
		byOutcome[respelling.outcome]++
	}
	fmt.Fprintf(streams.Output, "external names' doc comments: %d\n", len(respellings))
	for _, outcome := range sortedKeys(byOutcome) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", outcome, byOutcome[outcome])
	}

	fmt.Fprintln(streams.Output, "\nexternal names' doc comments:")
	for _, respelling := range respellings {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s -> %s: %s\n",
			relative(respelling.position),
			respelling.oldName,
			respelling.newName,
			respelling.outcome,
		)
	}
	fmt.Fprintln(streams.Output, "\nencoding guard fields:")
	for _, field := range result.EncodedFields {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s %s -> %s [%s] (%s): %s\n",
			relative(field.Position),
			field.Kind,
			field.OldName,
			field.NewName,
			strings.Join(field.Encoders, ", "),
			field.Tag,
			field.Outcome,
		)
	}
	fmt.Fprintln(streams.Output, "\nrefused:")
	for _, rejection := range result.Rejections {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s %s -> %s: [%s] %s\n",
			relative(rejection.Position),
			rejection.Kind,
			rejection.OldName,
			rejection.NewName,
			rejection.Class,
			trim(rejection.Reason),
		)
	}
	fmt.Fprintln(streams.Output, "\nlisted:")
	sort.Slice(
		listed,
		func(left, right int) bool { return relative(listed[left].position) < relative(listed[right].position) },
	)
	for _, name := range listed {
		fmt.Fprintf(streams.Output, "  %s: %s %s: %s\n", relative(name.position), name.kind, name.name, name.reason)
	}
	fmt.Fprintln(streams.Output, "\naccepted:")
	for _, accepted := range result.Accepted {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s %s -> %s\n",
			relative(accepted.Position),
			accepted.Kind,
			accepted.OldName,
			accepted.NewName,
		)
	}
	fmt.Fprintln(streams.Output, "\ntags added:")
	for _, edit := range result.TagEdits {
		path, _ := loaded.Relative(edit.Filename)
		fmt.Fprintf(streams.Output, "  %s offset %d:%s\n", path, edit.Offset, edit.NewText)
	}
	fmt.Fprintln(streams.Output, "\nnotes:")
	for _, note := range result.Notes {
		fmt.Fprintln(streams.Output, "  "+trim(note))
	}
}
