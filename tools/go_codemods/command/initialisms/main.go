// Command initialisms spells every initialism in a declared name as a word, the house casing: `groupID`
// becomes `groupId`, `URL` becomes `Url`, `HTTPServer` becomes `HttpServer`, `EnforceForTSTypes` becomes
// `EnforceForTsTypes`. Its candidates are exactly what the rules circle's
// cohere-go/consistency-no-uppercase-initialism analyzer flags, with the same exemptions; every one goes through
// the global rename engine (locals through the local engine first), which refuses any rename that could change
// what the program means. The rewrite is reloaded in memory and every identifier's resolution compared before
// anything is written, and nothing is written unless every one matches.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/tools/go_codemods/global_rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"

	"golang.org/x/tools/go/packages"
)

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code initialisms exits with: runCommandLine returns its exit code here, and
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

// runCommandLine reads the flags and runs initialisms, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print the plan, write nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	tagEncodedFields := flag.Bool(
		"tag-encoded-fields",
		false,
		"keep an encoded field's key by adding a tag that spells its old name",
	)
	limitLineLength := flag.Bool(
		"limit-line-length",
		false,
		"refuse a rename that would make a line newly longer than 120 columns",
	)
	renameImplementations := flag.Bool(
		"rename-implementations",
		false,
		"rename every workspace method implementing a renamed interface method with it",
	)
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	flag.Parse()
	return run(*root, skips, *dryRun, global_rename.Options{
		TagEncodedFields:      *tagEncodedFields,
		LimitLineLength:       *limitLineLength,
		RenameImplementations: *renameImplementations,
	})
}

// guard is a type switch's bound name: go/types gives it no object, only one per clause, so no engine renames it.
type guard struct {
	position token.Position
	name     string
}

func run(root string, skips []string, dryRun bool, options global_rename.Options) int {
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(streams.Errors, "initialisms: loading: %v\n", loadError)
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

	proposals, guards, generatedSkipped := findCandidates(loaded)
	fmt.Fprintf(
		streams.Output,
		"candidates: %d declarations (plus %d type switch guards, which no engine renames; %d names in generated files left, as the analyzer leaves them)\n",
		len(proposals),
		len(guards),
		generatedSkipped,
	)

	options.ReviewedLiterals = reviewedLiterals
	result := global_rename.Plan(loaded, proposals, options)
	report(loaded, guards, result)

	if dryRun {
		return 0
	}
	if len(result.Unresolved)+len(result.SelectionModelMismatches)+len(result.SatisfactionModelMismatches) > 0 {
		fmt.Fprintln(
			streams.Errors,
			"initialisms: the engine's model disagreed with go/types somewhere; the renames it touched were refused, the rest go on to the verified write",
		)
	}
	// The write reloads the whole workspace to prove it, so the plan is seen before that wait.
	streams.Show(streams.Output)
	mismatches, applyError := global_rename.Apply(loaded, root, skips, result, streams.Output)
	if applyError != nil {
		fmt.Fprintf(streams.Errors, "initialisms: %v\n", applyError)
		return 1
	}
	if mismatches > 0 {
		return 1
	}
	return 0
}

// findCandidates proposes a word-cased name for every declared name the analyzer flags, once per declaration
// across variants. It reads the analyzed variants, which hold every file.
func findCandidates(loaded *workspace.Workspace) ([]global_rename.Proposal, []guard, int) {
	var proposals []global_rename.Proposal
	var guards []guard
	seen := map[string]bool{}
	seenGuards := map[string]bool{}
	generatedSkipped := 0
	for _, analyzedPackage := range loaded.Analyzed {
		directed := directedObjects(analyzedPackage)
		interfaces := &foreignInterfaces{goPackage: analyzedPackage}
		for _, file := range analyzedPackage.Syntax {
			filename := analyzedPackage.Fset.PositionFor(file.Package, false).Filename
			if _, inside := loaded.Relative(filename); !inside {
				continue
			}
			generated := ast.IsGenerated(file)
			isTestFile := strings.HasSuffix(filename, "_test.go")
			ast.Inspect(file, func(node ast.Node) bool {
				switch declaring := node.(type) {
				case *ast.Ident:
					object := analyzedPackage.TypesInfo.Defs[declaring]
					if object == nil ||
						directed[object] ||
						!(flagged(declaring.Name) || holdsFileSystemWord(declaring.Name)) {
						return true
					}
					if !isOurs(object, isTestFile, interfaces) {
						return true
					}
					if generated {
						generatedSkipped++
						return true
					}
					position := analyzedPackage.Fset.PositionFor(object.Pos(), false)
					identity := fmt.Sprintf("%s:%d", position.Filename, position.Offset)
					if seen[identity] {
						return true
					}
					seen[identity] = true
					newName := houseName(declaring.Name)
					if relative, inside := loaded.Relative(position.Filename); inside {
						if chosen, overridden := nameOverrides[relative+":"+declaring.Name]; overridden {
							newName = chosen
						}
					}
					proposals = append(
						proposals,
						global_rename.Proposal{Package: analyzedPackage, Object: object, NewName: newName},
					)
				case *ast.TypeSwitchStmt:
					assignment, isAssignment := declaring.Assign.(*ast.AssignStmt)
					if !isAssignment || len(assignment.Lhs) != 1 || generated {
						return true
					}
					if bound, isIdentifier := assignment.Lhs[0].(*ast.Ident); isIdentifier && flagged(bound.Name) {
						position := analyzedPackage.Fset.PositionFor(bound.Pos(), false)
						identity := fmt.Sprintf("%s:%d", position.Filename, position.Offset)
						if !seenGuards[identity] {
							seenGuards[identity] = true
							guards = append(guards, guard{position: position, name: bound.Name})
						}
					}
				}
				return true
			})
		}
	}
	return proposals, guards, generatedSkipped
}

// flagged is the analyzer's judge: not blank, not screaming snake case, and changed by word casing.
func flagged(name string) bool {
	return name != "_" && !isScreamingSnakeCase(name) && wordCased(name) != name
}

// houseName is a name word-cased, with the FS word spelled out: the house writes a file system as FileSystem
// (NodeFileSystem, countingFileSystem), as receivernames names an FS receiver fileSystem, so FS reads FileSystem
// and not Fs, which is only the abbreviation cased as a word.
func houseName(name string) string {
	return spelledFileSystem(wordCased(name))
}

// holdsFileSystemWord is whether a name already spells FS as the word Fs, as a tree word-cased before FileSystem
// was its spelling does, so the same pass settles both trees.
func holdsFileSystemWord(name string) bool {
	return !isScreamingSnakeCase(name) && spelledFileSystem(name) != name
}

// spelledFileSystem is name with each camel word Fs spelled FileSystem, and a leading fs before a capital, the FS
// word opening a lower camel name, spelled fileSystem. A word is bounded by the name's start, a lower-case letter
// or a digit before it, and its end, a capital or a digit after it, so Dfs, Fsi and Lufs hold no Fs word.
func spelledFileSystem(name string) string {
	var builder strings.Builder
	runes := []rune(name)
	for index := 0; index < len(runes); index++ {
		if index+1 < len(runes) &&
			runes[index+1] == 's' &&
			(runes[index] == 'F' || (index == 0 && runes[index] == 'f')) {
			startsWord := index == 0 || unicode.IsLower(runes[index-1]) || unicode.IsDigit(runes[index-1])
			after := index + 2
			endsWord := after == len(runes) || unicode.IsUpper(runes[after]) || unicode.IsDigit(runes[after])
			wholeLowerName := runes[index] == 'f' && after == len(runes)
			if startsWord && endsWord && !wholeLowerName {
				if runes[index] == 'f' {
					builder.WriteString("fileSystem")
				} else {
					builder.WriteString("FileSystem")
				}
				index++
				continue
			}
		}
		builder.WriteRune(runes[index])
	}
	return builder.String()
}

// report prints the counts, then every list a reviewer needs.
func report(loaded *workspace.Workspace, guards []guard, result *global_rename.Result) {
	relative := func(position token.Position) string {
		if path, inside := loaded.Relative(position.Filename); inside {
			return fmt.Sprintf("%s:%d", path, position.Line)
		}
		return position.String()
	}
	trim := func(text string) string { return strings.ReplaceAll(text, loaded.Root+"/", "") }

	candidatesByKind := map[string]int{}
	for _, rejection := range result.Rejections {
		if rejection.Kind != global_rename.KindEmbeddedField {
			candidatesByKind[rejection.Kind]++
		}
	}
	acceptedByKind := map[string]int{}
	impliedAccepted := 0
	for _, accepted := range result.Accepted {
		acceptedByKind[accepted.Kind]++
		if accepted.Kind == global_rename.KindEmbeddedField {
			impliedAccepted++
		} else {
			candidatesByKind[accepted.Kind]++
		}
	}
	fmt.Fprintln(streams.Output, "candidates by kind:")
	for _, kind := range sortedKeys(candidatesByKind) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", kind, candidatesByKind[kind])
	}
	fmt.Fprintf(
		streams.Output,
		"accepted: %d (of them %d embedded fields renamed with their type)\n",
		len(result.Accepted),
		impliedAccepted,
	)
	for _, kind := range sortedKeys(acceptedByKind) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", kind, acceptedByKind[kind])
	}
	fmt.Fprintf(streams.Output, "refused: %d, and %d type switch guards\n", len(result.Rejections), len(guards))
	byClass := map[string]int{}
	for _, rejection := range result.Rejections {
		byClass[rejection.Class]++
	}
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
	fmt.Fprintf(
		streams.Output,
		"encoding guard: %d proposed fields reachable by an encoder or reflection; %d encoder arguments the analysis could not follow\n",
		len(result.EncodedFields),
		len(result.OpaqueEncoderArguments),
	)

	fmt.Fprintln(streams.Output, "\nencoding guard fields:")
	for _, field := range result.EncodedFields {
		tag := field.Tag
		if tag == "" {
			tag = "no tag"
		}
		fmt.Fprintf(
			streams.Output,
			"  %s: %s %s -> %s [%s] (%s): %s\n",
			relative(field.Position),
			field.Kind,
			field.OldName,
			field.NewName,
			strings.Join(field.Encoders, ", "),
			tag,
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
	fmt.Fprintln(streams.Output, "\ntype switch guards (refused, no engine renames them):")
	for _, guard := range guards {
		fmt.Fprintf(streams.Output, "  %s: %s -> %s\n", relative(guard.position), guard.name, houseName(guard.name))
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
	fmt.Fprintln(streams.Output, "\nplatform twins renamed outside the load:")
	for _, edit := range result.TwinEdits {
		path, _ := loaded.Relative(edit.Filename)
		fmt.Fprintf(streams.Output, "  %s offset %d: %s -> %s\n", path, edit.Offset, edit.OldText, edit.NewText)
	}
	fmt.Fprintln(streams.Output, "\ntags added:")
	for _, edit := range result.TagEdits {
		path, _ := loaded.Relative(edit.Filename)
		fmt.Fprintf(streams.Output, "  %s offset %d:%s\n", path, edit.Offset, edit.NewText)
	}
	for _, section := range []struct {
		title   string
		entries []string
	}{
		{"unresolved scope walks", result.Unresolved},
		{"selection model mismatches", result.SelectionModelMismatches},
		{"satisfaction model mismatches", result.SatisfactionModelMismatches},
		{"encoder arguments the analysis could not follow", result.OpaqueEncoderArguments},
		{"notes", result.Notes},
	} {
		fmt.Fprintf(streams.Output, "\n%s:\n", section.title)
		for _, entry := range section.entries {
			fmt.Fprintln(streams.Output, "  "+trim(entry))
		}
	}
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// What follows is the rules circle's analyzer, cohere-go/consistency-no-uppercase-initialism on branch
// cohere-go-rules, read and kept to its letter so this codemod proposes exactly what it flags.

// isOurs is whether object's name is ours to spell. A package name is the imported package's, an embedded field
// is named by its type, and a method may be named by an interface of another package.
func isOurs(object types.Object, isTestFile bool, interfaces *foreignInterfaces) bool {
	switch declared := object.(type) {
	case *types.Var:
		return !declared.Embedded()
	case *types.Const, *types.TypeName, *types.Label:
		return true
	case *types.Func:
		if declared.Signature().Recv() != nil {
			return !interfaces.fix(declared)
		}
		return !isTestFile || !isExampleName(declared.Name())
	}
	return false
}

func isExampleName(name string) bool {
	rest, found := strings.CutPrefix(name, "Example")
	if !found {
		return false
	}
	if rest == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(first)
}

// directedObjects are the package-level declarations a //go:linkname or a cgo //export fixes by name.
func directedObjects(goPackage *packages.Package) map[types.Object]bool {
	directed := map[types.Object]bool{}
	for _, file := range goPackage.Syntax {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				fields := strings.Fields(comment.Text)
				if len(fields) >= 2 && fields[0] == "//go:linkname" {
					if object := goPackage.Types.Scope().Lookup(fields[1]); object != nil {
						directed[object] = true
					}
				}
			}
		}
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Recv != nil || function.Doc == nil {
				continue
			}
			for _, comment := range function.Doc.List {
				if comment.Text == "//export "+function.Name.Name {
					if object := goPackage.TypesInfo.Defs[function.Name]; object != nil {
						directed[object] = true
					}
				}
			}
		}
	}
	return directed
}

func isScreamingSnakeCase(name string) bool {
	if !strings.Contains(name, "_") {
		return false
	}
	for _, character := range name {
		if unicode.IsLower(character) {
			return false
		}
	}
	return true
}

// wordCased is name with each run of capitals spelled as a word, its first capital kept and the rest lowered.
// When the run is followed by a lower-case letter, its last capital opens that next word and stays, so
// JSONEncoder becomes JsonEncoder and IPv4 stays. A run followed by a lone s that ends the word is a plural
// initialism, so IDs becomes Ids.
func wordCased(name string) string {
	runes := []rune(name)
	start := 0
	for start < len(runes) {
		if !unicode.IsUpper(runes[start]) {
			start++
			continue
		}
		end := start
		for end < len(runes) && unicode.IsUpper(runes[end]) {
			end++
		}
		lowered := end
		if end < len(runes) && unicode.IsLower(runes[end]) && !isPluralEnding(runes, end) {
			lowered = end - 1
		}
		for index := start + 1; index < lowered; index++ {
			runes[index] = unicode.ToLower(runes[index])
		}
		start = end
	}
	return string(runes)
}

func isPluralEnding(runes []rune, index int) bool {
	return runes[index] == 's' && (index+1 == len(runes) || !unicode.IsLower(runes[index+1]))
}

// foreignInterfaces are the interfaces of every package in this package's import graph, and error, by method
// name, generic ones apart.
type foreignInterfaces struct {
	goPackage       *packages.Package
	byMethod        map[string][]*types.Interface
	genericByMethod map[string][]*types.Interface
}

func (interfaces *foreignInterfaces) fix(method *types.Func) bool {
	if interfaces.byMethod == nil {
		interfaces.build()
	}
	implementer := implementerOf(method)
	for _, candidate := range interfaces.byMethod[method.Name()] {
		if types.Implements(implementer, candidate) {
			return true
		}
	}
	for _, candidate := range interfaces.genericByMethod[method.Name()] {
		if implementsInShape(implementer, candidate) {
			return true
		}
	}
	return false
}

func implementsInShape(implementer types.Type, candidate *types.Interface) bool {
	for index := range candidate.NumMethods() {
		wanted := candidate.Method(index)
		object, _, _ := types.LookupFieldOrMethod(implementer, true, wanted.Pkg(), wanted.Name())
		found, isFunc := object.(*types.Func)
		if !isFunc {
			return false
		}
		if found.Signature().Params().Len() != wanted.Signature().Params().Len() ||
			found.Signature().Results().Len() != wanted.Signature().Results().Len() ||
			found.Signature().Variadic() != wanted.Signature().Variadic() {
			return false
		}
	}
	return true
}

func implementerOf(method *types.Func) types.Type {
	receiverType := method.Signature().Recv().Type()
	if pointer, isPointer := types.Unalias(receiverType).(*types.Pointer); isPointer {
		receiverType = pointer.Elem()
	}
	if types.IsInterface(receiverType) {
		return receiverType
	}
	return types.NewPointer(receiverType)
}

func (interfaces *foreignInterfaces) build() {
	interfaces.byMethod = map[string][]*types.Interface{}
	interfaces.genericByMethod = map[string][]*types.Interface{}
	interfaces.add(types.Universe.Lookup("error").Type())
	visited := map[*types.Package]bool{interfaces.goPackage.Types: true}
	pending := []*types.Package{interfaces.goPackage.Types}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, imported := range current.Imports() {
			if visited[imported] {
				continue
			}
			visited[imported] = true
			pending = append(pending, imported)
			for _, name := range imported.Scope().Names() {
				if typeName, isTypeName := imported.Scope().Lookup(name).(*types.TypeName); isTypeName {
					interfaces.add(typeName.Type())
				}
			}
		}
	}
}

func (interfaces *foreignInterfaces) add(declared types.Type) {
	interfaceType, isInterface := declared.Underlying().(*types.Interface)
	if !isInterface || !interfaceType.IsMethodSet() {
		return
	}
	index := interfaces.byMethod
	if named, isNamed := types.Unalias(declared).(*types.Named); isNamed && named.TypeParams().Len() > 0 {
		index = interfaces.genericByMethod
	}
	for methodIndex := range interfaceType.NumMethods() {
		methodName := interfaceType.Method(methodIndex).Name()
		index[methodName] = append(index[methodName], interfaceType)
	}
}
