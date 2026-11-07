// Command loopnames renames the single-letter names consistency-no-ambiguous-identifier flags for what they
// hold (ruling 2 on #pp04nv8). A loop or a comparator says it first: the two parameters of a function literal
// passed to a comparator function (slices.SortFunc, sort.Slice and the like, matched through types) become left
// and right; a range key over a slice, array, string or integer named i, j, k or n, and a three-clause loop
// counter so named, become index; a single-letter range value takes the singular of the ranged collection's
// plural name (`for _, entry := range entries`); a map's single-letter key and value become key and value. A
// loop nested with another that wants the same name takes the singular-based name instead (`lineIndex`), and is
// left for the waves when no plural collection names it.
//
// Every other letter is named from its declaration, by the rules in typed.go: a pair of parameters `a, b` of one
// type are left and right, a function literal's parameter takes the singular of the collection it is handed
// with, a call's result takes the call's conventional name, and then the letter's type names it (`program
// *Program` is program, `t *checker.Type` subjectType), and last a few letters' conventional meaning for a basic
// type (a rune is a character, an integer i an index). Every letter receiver of a type takes one name, and a
// type switch's letter is `typed` in every clause. A struct field is renamed through globalrename. Each letter
// carries its names best first, and a letter the engine refuses one name for tries the next; the coordinates
// x, y and z, and the blank identifier, are never touched, and every letter no rule names is listed for the
// waves with its function and line. Every rename goes through the rename engine, which refuses any that would
// change what an identifier resolves to; the rewrite is proven in memory by workspace.ApplyVerified before
// anything is written.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/tools/go_codemods/rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"

	"golang.org/x/tools/go/types/typeutil"
)

// commandName prefixes every message this command prints to standard error.
const commandName = "loopnames"

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code loopnames exits with: runCommandLine returns its exit code here, and
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

// runCommandLine reads the flags and runs loopnames, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print counts and every rejection, write nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	flag.Parse()
	return run(*root, skips, *dryRun)
}

// candidate is one single-letter local and what the generator decided for it.
type candidate struct {
	variable *types.Var
	// names are the proposed names, best first: the engine's fixpoint takes the first it accepts. Empty when the
	// variable is not eligible.
	names []string
	// category says why the variable is not eligible, or "" when it is.
	category string
	// call is the detail the report prints: the comparator function, or the ranged collection.
	call string
	// rule names the rule that chose the names, for the report.
	rule string
	// group ties variables that take one name together, every letter receiver of one type and every clause
	// variable of one type switch, or is "" for a variable alone.
	group string
	// function is the function the variable is declared in, for the waves' report.
	function string
}

// vocabulary is the abbreviation vocabulary, by lowercased word.
type vocabulary struct {
	expansions map[string]string
	// adviceWords have no mechanical expansion: a name holding one is judged by hand.
	adviceWords map[string]bool
	// matcherWords are judged by a matcher, not as a plain word (`ms`): a name holding one is judged by hand.
	matcherWords    map[string]bool
	allowedNames    []string
	allowedSegments []string
}

// parseVocabulary reads policy/Abbreviations.json.
func parseVocabulary(contents []byte) (vocabulary, error) {
	type style struct {
		Style   string `json:"style"`
		Matcher string `json:"matcher"`
	}
	var file struct {
		Abbreviations []struct {
			Abbreviation string   `json:"abbreviation"`
			Expansions   []string `json:"expansions"`
			Advice       string   `json:"advice"`
			Whole        *style   `json:"whole"`
			Prefix       *style   `json:"prefix"`
			Suffix       *style   `json:"suffix"`
		} `json:"abbreviations"`
		AllowedNames []struct {
			Name string `json:"name"`
		} `json:"allowedNames"`
		AllowedSegments []struct {
			Segment string `json:"segment"`
		} `json:"allowedSegments"`
	}
	if decodeError := json.Unmarshal(contents, &file); decodeError != nil {
		return vocabulary{}, decodeError
	}
	words := vocabulary{
		expansions:   map[string]string{},
		adviceWords:  map[string]bool{},
		matcherWords: map[string]bool{},
	}
	isAdvice := func(form *style) bool { return form != nil && form.Style == "advice" }
	for _, entry := range file.Abbreviations {
		word := strings.ToLower(entry.Abbreviation)
		switch {
		case entry.Suffix != nil && entry.Suffix.Matcher != "":
			words.matcherWords[word] = true
		case len(entry.Expansions) != 1 || entry.Advice != "" || isAdvice(entry.Whole) || isAdvice(entry.Prefix):
			words.adviceWords[word] = true
		default:
			words.expansions[word] = entry.Expansions[0]
		}
	}
	if len(words.expansions) == 0 {
		return vocabulary{}, fmt.Errorf("the vocabulary holds no expansions")
	}
	for _, allowed := range file.AllowedNames {
		words.allowedNames = append(words.allowedNames, allowed.Name)
	}
	for _, allowed := range file.AllowedSegments {
		words.allowedSegments = append(words.allowedSegments, allowed.Segment)
	}
	return words, nil
}

// camelWords splits a Go name into its camelCase words: a boundary before an uppercase letter that follows a
// lowercase letter or digit, and before the last uppercase letter of a run that a lowercase letter follows
// (`HTTPServer` is `HTTP` and `Server`).
func camelWords(name string) []string {
	runes := []rune(name)
	var words []string
	start := 0
	for index := 1; index < len(runes); index++ {
		previous, current := runes[index-1], runes[index]
		lowerFollows := index+1 < len(runes) && unicode.IsLower(runes[index+1])
		if unicode.IsUpper(current) &&
			(unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && lowerFollows)) {
			words = append(words, string(runes[start:index]))
			start = index
		}
	}
	return append(words, string(runes[start:]))
}

// fullWordsLowerCamel is name in lowerCamel with every vocabulary word expanded. It returns "" and a category
// when a word has no mechanical expansion.
func fullWordsLowerCamel(name string, words vocabulary) (string, string) {
	var builder strings.Builder
	for index, word := range camelWords(name) {
		lowered := strings.ToLower(word)
		if words.adviceWords[lowered] {
			return "", "advice-only word in the name"
		}
		if words.matcherWords[lowered] {
			return "", "matcher-judged word (ms) in the name"
		}
		expansion, abbreviated := words.expansions[lowered]
		switch {
		case abbreviated && index == 0:
			// An expansion is already lowerCamel (`workingDirectory`).
			word = expansion
		case abbreviated:
			word = strings.ToUpper(expansion[:1]) + expansion[1:]
		case index == 0:
			// A plain first word lowers whole, initialism or not: `HTTPGet` is `httpGet`.
			word = lowered
		}
		builder.WriteString(word)
	}
	return builder.String(), ""
}

// keptLetters are the coordinates consistency-no-ambiguous-identifier keeps, so they are never candidates.
var keptLetters = map[string]bool{"x": true, "y": true, "z": true}

// isJudgedLetter reports whether name is what the analyzer flags: one lowercase ASCII letter other than a
// coordinate. The blank identifier is not a letter.
func isJudgedLetter(name string) bool {
	return len(name) == 1 && name[0] >= 'a' && name[0] <= 'z' && !keptLetters[name]
}

// indexLetters are the letters a range key over a slice, array or string, or a three-clause loop counter, is
// renamed from: the conventional spellings of an index.
var indexLetters = map[string]bool{"i": true, "j": true, "k": true, "n": true}

// comparatorFunction is a function whose function-literal argument compares two elements, by package path and
// name.
type comparatorFunction struct {
	packagePath string
	name        string
}

// comparatorFunctions take a comparator whose two parameters are the two things compared: elements for the
// slices and maps functions, indices for sort's. Their parameters are named left and right.
var comparatorFunctions = map[comparatorFunction]bool{
	{packagePath: "slices", name: "SortFunc"}:         true,
	{packagePath: "slices", name: "SortStableFunc"}:   true,
	{packagePath: "slices", name: "IsSortedFunc"}:     true,
	{packagePath: "slices", name: "BinarySearchFunc"}: true,
	{packagePath: "slices", name: "MinFunc"}:          true,
	{packagePath: "slices", name: "MaxFunc"}:          true,
	{packagePath: "slices", name: "CompareFunc"}:      true,
	{packagePath: "slices", name: "EqualFunc"}:        true,
	{packagePath: "slices", name: "CompactFunc"}:      true,
	{packagePath: "maps", name: "EqualFunc"}:          true,
	{packagePath: "sort", name: "Slice"}:              true,
	{packagePath: "sort", name: "SliceStable"}:        true,
	{packagePath: "sort", name: "SliceIsSorted"}:      true,
}

// comparatorNames are the names a comparator's parameters take, by position.
var comparatorNames = []string{"left", "right"}

// binarySearchNames are the names slices.BinarySearchFunc's comparator parameters take: the first is an
// element, which the rules for any parameter name, and the second the target searched for.
var binarySearchNames = []string{"", "target"}

// decision is what the generator decided for one variable: new names, best first, or a category that leaves it
// for the waves, a detail for the report (the comparator function, or the ranged collection), and the rule.
type decision struct {
	names    []string
	category string
	detail   string
	rule     string
	group    string
}

// irregularSingulars are plural words the suffix rules get wrong, and words that look plural but are not
// (mapped to "").
var irregularSingulars = map[string]string{
	"children": "child",
	"people":   "person",
	"indices":  "index",
	"indexes":  "index",
	"vertices": "vertex",
	"matrices": "matrix",
	"aliases":  "alias",
	"caches":   "cache",
	"statuses": "status",
	"analyses": "analysis",
	"leaves":   "leaf",
	"halves":   "half",
	"cookies":  "cookie",
	"movies":   "movie",
	"series":   "",
	"species":  "",
	"news":     "",
	"does":     "",
	"goes":     "",
}

// singularWord is the singular of one lowercase word when it reads as a regular English plural, by its suffix:
// entries to entry, matches to match, boxes to box, lines to line. A word of fewer than four letters, or one
// ending in ss, us or is (class, status, analysis), is not judged plural.
func singularWord(word string) (string, bool) {
	if singular, irregular := irregularSingulars[word]; irregular {
		return singular, singular != ""
	}
	if len(word) < 4 {
		return "", false
	}
	switch {
	case strings.HasSuffix(word, "ies"):
		return strings.TrimSuffix(word, "ies") + "y", true
	case strings.HasSuffix(word, "sses"), strings.HasSuffix(word, "shes"), strings.HasSuffix(word, "ches"),
		strings.HasSuffix(word, "xes"), strings.HasSuffix(word, "zzes"):
		return strings.TrimSuffix(word, "es"), true
	case strings.HasSuffix(word, "ss"), strings.HasSuffix(word, "us"), strings.HasSuffix(word, "is"):
		return "", false
	case strings.HasSuffix(word, "s"):
		return strings.TrimSuffix(word, "s"), true
	}
	return "", false
}

// collectionName is the name a ranged or measured expression is spelled with: an identifier, or the selected
// name of a selector (`file.lines` is lines). Anything else has none.
func collectionName(expression ast.Expr) string {
	switch typed := ast.Unparen(expression).(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	}
	return ""
}

// singularOf is the singular, in full lowerCamel words, of the collection expression names when its last camel
// word is a plural (`lines` gives line, `fileNames` fileName, `entries` entry), or "" with a category.
func singularOf(expression ast.Expr, words vocabulary) (string, string) {
	name := collectionName(expression)
	if name == "" {
		return "", "the collection is not a named identifier"
	}
	parts := camelWords(name)
	last := parts[len(parts)-1]
	singular, plural := singularWord(strings.ToLower(last))
	if !plural || len(singular) < 3 {
		return "", "the collection's name is not a plural word"
	}
	if last[0] >= 'A' && last[0] <= 'Z' {
		singular = strings.ToUpper(singular[:1]) + singular[1:]
	}
	newName, category := fullWordsLowerCamel(strings.Join(parts[:len(parts)-1], "")+singular, words)
	if newName == "" {
		return "", category
	}
	if token.IsKeyword(newName) || types.Universe.Lookup(newName) != nil {
		return "", "the singular would be a keyword or predeclared identifier"
	}
	return newName, ""
}

// loopVariable is one single-letter variable a loop declares, and the plain name it wants: index, key or
// value, which a nested loop wanting the same name turns into a singular-based one.
type loopVariable struct {
	variable  *types.Var
	plainName string
}

// loop is one range or three-clause loop and what its letters want.
type loop struct {
	node      ast.Node
	ancestors []*loop
	variables []loopVariable
	// singular names the loop's collection, or is "" with singularCategory saying why not.
	singular         string
	singularCategory string
	detail           string
}

// wants reports whether the loop wants plainName for one of its letters.
func (current *loop) wants(plainName string) bool {
	for _, wanted := range current.variables {
		if wanted.plainName == plainName {
			return true
		}
	}
	return false
}

// variableOf is the variable an identifier expression declares, when it is a judged letter.
func variableOf(info *types.Info, expression ast.Expr) *types.Var {
	identifier, isIdentifier := expression.(*ast.Ident)
	if !isIdentifier || !isJudgedLetter(identifier.Name) {
		return nil
	}
	variable, isVariable := info.Defs[identifier].(*types.Var)
	if !isVariable {
		return nil
	}
	return variable
}

// rangeLoop reads a range statement: an index key over a slice, array or string wants index, and its value
// takes the singular of the collection; a map's key and value want key and value. Every other letter it
// declares is decided here with a category.
// Mutates: decisions every loop of the file decides its letters into the one table the pass reads
func rangeLoop(info *types.Info, statement *ast.RangeStmt, words vocabulary, decisions map[*types.Var]decision) *loop {
	current := &loop{node: statement, detail: collectionName(statement.X)}
	current.singular, current.singularCategory = singularOf(statement.X, words)
	if current.singular == "" {
		// A collection a call returns, or a generic plural selected from a named one, has a singular too:
		// `subjectType.Types()` holds member types, `UnionTypePartsSeq(subjectType)` parts.
		if singular := collectionSingular(statement.X, words); singular != "" {
			current.singular, current.singularCategory = singular, ""
		}
	}
	if statement.Tok != token.DEFINE {
		return current
	}
	key, value := variableOf(info, statement.Key), variableOf(info, statement.Value)
	if key == nil && value == nil {
		return current
	}
	leave := func(variable *types.Var, category string) {
		if variable != nil {
			decisions[variable] = decision{category: category, detail: current.detail}
		}
	}
	rangedType := info.TypeOf(statement.X)
	if rangedType == nil {
		leave(key, "range over an untyped expression")
		leave(value, "range over an untyped expression")
		return current
	}
	underlying := rangedType.Underlying()
	if pointer, isPointer := underlying.(*types.Pointer); isPointer {
		underlying = pointer.Elem().Underlying()
	}
	sequence := false
	switch typed := underlying.(type) {
	case *types.Slice, *types.Array:
		sequence = true
	case *types.Basic:
		switch {
		case typed.Info()&types.IsString != 0:
			sequence = true
		case typed.Info()&types.IsInteger != 0:
			// A range over an integer counts, so its key is an index the way a counter is, nested or not.
			if key != nil && indexLetters[key.Name()] {
				current.variables = append(current.variables, loopVariable{variable: key, plainName: "index"})
			} else {
				leave(key, "range over an integer")
			}
			return current
		}
	case *types.Map:
		if key != nil {
			current.variables = append(current.variables, loopVariable{variable: key, plainName: "key"})
		}
		if value != nil {
			current.variables = append(current.variables, loopVariable{variable: value, plainName: "value"})
		}
		return current
	case *types.Chan:
		leave(key, "range over a channel")
		return current
	case *types.Signature:
		// An iterator's single variable is its element, named like a slice's value.
		if key != nil && value == nil && current.singular != "" {
			decisions[key] = decision{names: []string{current.singular}, detail: current.detail, rule: "range value"}
			return current
		}
		leave(key, "range over a function")
		leave(value, "range over a function")
		return current
	}
	if !sequence {
		leave(key, "range over another type")
		leave(value, "range over another type")
		return current
	}
	if key != nil {
		if indexLetters[key.Name()] {
			current.variables = append(current.variables, loopVariable{variable: key, plainName: "index"})
		} else {
			leave(key, "index key letter other than i, j, k or n")
		}
	}
	if value != nil {
		if current.singular == "" {
			leave(value, "range value: "+current.singularCategory)
		} else {
			decisions[value] = decision{names: []string{current.singular}, detail: current.detail, rule: "range value"}
		}
	}
	return current
}

// counterLoop reads a three-clause loop whose counter is an index letter, `for i := 0; ...`: it wants index,
// and its collection is the one its condition measures, `i < len(lines)`.
// Mutates: decisions every loop of the file decides its letters into the one table the pass reads
func counterLoop(info *types.Info, statement *ast.ForStmt, words vocabulary, decisions map[*types.Var]decision) *loop {
	current := &loop{node: statement, singularCategory: "the condition measures no collection with len"}
	initial, isAssignment := statement.Init.(*ast.AssignStmt)
	if !isAssignment || initial.Tok != token.DEFINE || len(initial.Lhs) != 1 {
		return current
	}
	counter := variableOf(info, initial.Lhs[0])
	if counter == nil {
		return current
	}
	if condition, isBinary := statement.Cond.(*ast.BinaryExpr); isBinary {
		if call, isCall := ast.Unparen(condition.Y).(*ast.CallExpr); isCall && len(call.Args) == 1 {
			if builtin, isBuiltin := typeutil.Callee(
				info,
				call,
			).(*types.Builtin); isBuiltin && builtin.Name() == "len" {
				current.detail = collectionName(call.Args[0])
				current.singular, current.singularCategory = singularOf(call.Args[0], words)
			}
		}
	}
	if !indexLetters[counter.Name()] {
		decisions[counter] = decision{category: "loop counter letter other than i, j, k or n", detail: current.detail}
		return current
	}
	current.variables = append(current.variables, loopVariable{variable: counter, plainName: "index"})
	return current
}

// comparatorDecisions names the parameters of every function literal passed to a comparator function.
// Mutates: decisions the comparators' parameters join the one table of the file's decisions
func comparatorDecisions(info *types.Info, file *ast.File, decisions map[*types.Var]decision) {
	ast.Inspect(file, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		function, isFunction := typeutil.Callee(info, call).(*types.Func)
		if !isFunction || function.Pkg() == nil {
			return true
		}
		function = function.Origin()
		if !comparatorFunctions[comparatorFunction{packagePath: function.Pkg().Path(), name: function.Name()}] {
			return true
		}
		qualified := function.Pkg().Path() + "." + function.Name()
		for _, argument := range call.Args {
			literal, isLiteral := ast.Unparen(argument).(*ast.FuncLit)
			if !isLiteral {
				continue
			}
			var parameters []*ast.Ident
			for _, field := range literal.Type.Params.List {
				parameters = append(parameters, field.Names...)
			}
			if len(parameters) != len(comparatorNames) {
				continue
			}
			for position, parameter := range parameters {
				newName := comparatorNames[position]
				if qualified == "slices.BinarySearchFunc" {
					newName = binarySearchNames[position]
				}
				if variable := variableOf(info, parameter); variable != nil && newName != "" {
					decisions[variable] = decision{names: []string{newName}, detail: qualified, rule: "comparator"}
				}
			}
		}
		return true
	})
}

// signatureKinds marks each variable a signature declares by kind: a receiver, a function's parameter, a
// function literal's parameter, a parameter in a function type, or a named result.
func signatureKinds(info *types.Info, files []*ast.File) map[*types.Var]string {
	kinds := map[*types.Var]string{}
	handled := map[*ast.FuncType]bool{}
	mark := func(fields *ast.FieldList, kind string) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				if variable, isVariable := info.Defs[name].(*types.Var); isVariable {
					kinds[variable] = kind
				}
			}
		}
	}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				mark(typed.Recv, "receiver")
				mark(typed.Type.Params, "function parameter")
				mark(typed.Type.Results, "named result")
				handled[typed.Type] = true
			case *ast.FuncLit:
				mark(typed.Type.Params, "function literal parameter")
				mark(typed.Type.Results, "named result")
				handled[typed.Type] = true
			case *ast.FuncType:
				if !handled[typed] {
					mark(typed.Params, "parameter in a function type")
					mark(typed.Results, "named result")
				}
			}
			return true
		})
	}
	return kinds
}

// findCandidates decides, for every single-letter local declared in files that consistency-no-ambiguous-
// identifier flags, its new name or why it has none.
func findCandidates(packageTypes *types.Package, info *types.Info, fileSet *token.FileSet, files []*ast.File,
	words vocabulary) []candidate {
	decisions := map[*types.Var]decision{}
	var loops []*loop
	for _, file := range files {
		comparatorDecisions(info, file, decisions)
		var enclosing []*loop
		var visit func(node ast.Node)
		visit = func(node ast.Node) {
			ast.Inspect(node, func(child ast.Node) bool {
				var current *loop
				var body *ast.BlockStmt
				switch typed := child.(type) {
				case *ast.RangeStmt:
					current, body = rangeLoop(info, typed, words, decisions), typed.Body
				case *ast.ForStmt:
					current, body = counterLoop(info, typed, words, decisions), typed.Body
				default:
					return true
				}
				current.ancestors = append([]*loop(nil), enclosing...)
				loops = append(loops, current)
				enclosing = append(enclosing, current)
				visit(body)
				enclosing = enclosing[:len(enclosing)-1]
				return false
			})
		}
		visit(file)
	}
	descendants := map[*loop][]*loop{}
	for _, current := range loops {
		for _, ancestor := range current.ancestors {
			descendants[ancestor] = append(descendants[ancestor], current)
		}
	}
	for _, current := range loops {
		for _, wanted := range current.variables {
			nested := false
			for _, other := range append(append([]*loop(nil), current.ancestors...), descendants[current]...) {
				if other.wants(wanted.plainName) {
					nested = true
				}
			}
			switch {
			case !nested:
				names := []string{wanted.plainName}
				if current.singular != "" {
					// A plain name taken in the loop's scope falls back to the singular-based one.
					names = append(names, current.singular+strings.ToUpper(wanted.plainName[:1])+wanted.plainName[1:])
				}
				decisions[wanted.variable] = decision{names: names, detail: current.detail, rule: "loop"}
			case current.singular == "":
				decisions[wanted.variable] = decision{
					category: "nested loop: " + current.singularCategory,
					detail:   current.detail,
				}
			default:
				decisions[wanted.variable] = decision{
					names:  []string{current.singular + strings.ToUpper(wanted.plainName[:1]) + wanted.plainName[1:]},
					detail: current.detail,
					rule:   "nested loop",
				}
			}
		}
	}

	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			if functionType, isFunctionType := node.(*ast.FuncType); isFunctionType {
				pairDecisions(info, functionType.Params, decisions)
			}
			return true
		})
	}
	for variable, decided := range receiverDecisions(info, fileSet, files, words) {
		decisions[variable] = decided
	}

	kinds := signatureKinds(info, files)
	var candidates []candidate
	for _, file := range files {
		importNames := fileImportNames(info, file)
		testFile := strings.HasSuffix(fileSet.Position(file.Pos()).Filename, "_test.go")
		ast.PreorderStack(file, nil, func(node ast.Node, stack []ast.Node) bool {
			if statement, isTypeSwitch := node.(*ast.TypeSwitchStmt); isTypeSwitch {
				candidates = append(candidates, typeSwitchCandidates(info, fileSet, statement, importNames, stack)...)
				return true
			}
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier || !isJudgedLetter(identifier.Name) {
				return true
			}
			variable, isVariable := info.Defs[identifier].(*types.Var)
			if !isVariable || !rename.IsLocal(variable, packageTypes) {
				return true
			}
			decided, found := decisions[variable]
			switch {
			case !found:
				decided = typedDecision(info, identifier, variable, stack, testFile, words)
			case len(decided.names) == 0 && (strings.HasPrefix(decided.category, "range") ||
				strings.HasPrefix(decided.category, "loop counter letter")):
				// A loop's index and counter letters are the loop rules' alone, so a nested pair they leave is not
				// named index twice; a range value or a range over a function the loop rules cannot name takes
				// what its type gives.
				if typed := typedDecision(info, identifier, variable, stack, testFile, words); len(typed.names) > 0 {
					typed.detail = decided.detail
					decided = typed
				}
			case decided.rule == "range value":
				// A collection's singular that conflicts falls back to the names the element's type gives.
				decided.names = append(decided.names, namesFromType(variable.Type(), words)...)
			}
			if decided.category == "" && len(decided.names) == 0 {
				decided.category = "other local variable"
				if kind, inSignature := kinds[variable]; inSignature {
					decided.category = kind
				}
			}
			names := decided.names
			if decided.group == "" {
				names = usableNames(names, importNames)
				if len(names) == 0 && len(decided.names) > 0 {
					decided.category = "every name the rules give is a keyword, predeclared, an import or a package: " +
						strings.Join(decided.names, ", ")
				}
			}
			candidates = append(candidates, candidate{
				variable: variable, names: names, category: decided.category, call: decided.detail,
				rule: decided.rule, group: decided.group, function: enclosingFunction(stack),
			})
			return true
		})
	}
	return candidates
}

// enclosingFunction names the function declaration a node lies in, for the waves' report.
func enclosingFunction(stack []ast.Node) string {
	for index := len(stack) - 1; index >= 0; index-- {
		if function, isFunction := stack[index].(*ast.FuncDecl); isFunction {
			return function.Name.Name
		}
	}
	return "package level"
}

// typedDecision names a letter no loop, comparator or pair named, from its declaration: a function literal's
// first parameter from the collection it is handed with, a test case ranged over in a test, a call's result
// from the call, then its type, then the letter's conventional meaning for a basic type.
func typedDecision(info *types.Info, identifier *ast.Ident, variable *types.Var, stack []ast.Node, testFile bool,
	words vocabulary) decision {
	var decided decision
	add := func(rule string, names []string) {
		for _, name := range names {
			if name == "" || slices.Contains(decided.names, name) {
				continue
			}
			if decided.rule == "" {
				decided.rule = rule
			}
			decided.names = append(decided.names, name)
		}
	}
	parent := stack[len(stack)-1]
	if literal, call := literalParameterOf(identifier, stack); literal != nil && call != nil {
		add("collection handed with the literal", literalCollectionNames(info, call, literal, variable, words))
	}
	if statement, isRange := parent.(*ast.RangeStmt); isRange && statement.Value == identifier && testFile {
		if _, isStruct := variable.Type().Underlying().(*types.Struct); isStruct {
			if _, isNamed := types.Unalias(variable.Type()).(*types.Named); !isNamed {
				add("test case", []string{"testCase"})
			}
		}
	}
	if call, position := producingCall(parent, identifier); call != nil {
		add("call result", callResultNames(info, call, position, variable, words))
	}
	if isErrorType(variable.Type()) && len(decided.names) == 0 {
		decided.category = "an error no call names"
		return decided
	}
	value := declaredValue(parent, identifier)
	if value != nil {
		add("fixture letter", fixtureLetterNames(value, variable.Name(), words))
		add("indexed element", indexedElementNames(value, words))
	}
	add("type", namesFromType(variable.Type(), words))
	if basic, isBasic := variable.Type().Underlying().(*types.Basic); isBasic {
		if value != nil {
			add("getter", getterNames(value, words))
		}
		add("letter convention", []string{letterConvention(variable.Name(), basic, value, enclosingFunction(stack))})
	}
	if len(decided.names) == 0 {
		decided.rule = ""
		decided.category = "no rule names a " + typeKind(variable.Type()) + " held by " + variable.Name()
	}
	return decided
}

// typeKind is a word for what kind of type a letter holds, for the waves' report.
func typeKind(declared types.Type) string {
	switch declared.Underlying().(type) {
	case *types.Basic:
		return "basic type (" + declared.String() + ")"
	case *types.Struct:
		return "struct"
	case *types.Slice:
		return "slice"
	case *types.Map:
		return "map"
	case *types.Signature:
		return "function"
	}
	return declared.String()
}

// literalParameterOf is the function literal identifier is the first parameter of and the call the literal is
// an argument of, or nils.
func literalParameterOf(identifier *ast.Ident, stack []ast.Node) (*ast.FuncLit, *ast.CallExpr) {
	if len(stack) < 5 {
		return nil, nil
	}
	fields, isFieldList := stack[len(stack)-2].(*ast.FieldList)
	literal, isLiteral := stack[len(stack)-4].(*ast.FuncLit)
	if !isFieldList || !isLiteral || fields != literal.Type.Params || len(fields.List[0].Names) == 0 ||
		fields.List[0].Names[0] != identifier {
		return nil, nil
	}
	for index := len(stack) - 5; index >= 0; index-- {
		switch typed := stack[index].(type) {
		case *ast.ParenExpr:
			continue
		case *ast.CallExpr:
			return literal, typed
		}
		break
	}
	return literal, nil
}

// receiverDecisions names every letter receiver, all of one type together: the name the type's other receivers
// already share, then the names its type gives, then the name the package's own code most often gives a value of
// the type (`state := &reactivity{...}`), then its package's name before its own (`ruleContext`) when the
// package's name is a word.
func receiverDecisions(
	info *types.Info,
	fileSet *token.FileSet,
	files []*ast.File,
	words vocabulary,
) map[*types.Var]decision {
	type receiverGroup struct {
		letters     []*types.Var
		siblings    map[string]bool
		importNames map[string]bool
	}
	groups := map[*types.TypeName]*receiverGroup{}
	var order []*types.TypeName
	for _, file := range files {
		importNames := fileImportNames(info, file)
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction ||
				function.Recv == nil ||
				len(function.Recv.List) != 1 ||
				len(function.Recv.List[0].Names) != 1 {
				continue
			}
			variable, isVariable := info.Defs[function.Recv.List[0].Names[0]].(*types.Var)
			if !isVariable {
				continue
			}
			typeName := receiverTypeName(variable)
			if typeName == nil {
				continue
			}
			group, found := groups[typeName]
			if !found {
				group = &receiverGroup{siblings: map[string]bool{}, importNames: map[string]bool{}}
				groups[typeName] = group
				order = append(order, typeName)
			}
			switch {
			case isJudgedLetter(variable.Name()):
				group.letters = append(group.letters, variable)
				for name := range importNames {
					group.importNames[name] = true
				}
			case variable.Name() != "_":
				group.siblings[variable.Name()] = true
			}
		}
	}
	decisions := map[*types.Var]decision{}
	for _, typeName := range order {
		group := groups[typeName]
		if len(group.letters) == 0 {
			continue
		}
		var names []string
		if len(group.siblings) == 1 {
			for sibling := range group.siblings {
				names = append(names, sibling)
			}
		}
		names = append(names, namesFromType(group.letters[0].Type(), words)...)
		names = append(names, packageNamesFor(info, files, typeName)...)
		if typeName.Pkg() != nil && !strings.Contains(typeName.Pkg().Name(), "_") {
			qualified := houseLowerCamel(append([]string{typeName.Pkg().Name()}, camelWords(typeName.Name())...), words)
			names = append(names, qualified)
		}
		names = usableNames(names, group.importNames)
		decided := decision{
			names: names, rule: "receiver", detail: typeName.Name(),
			group: fmt.Sprintf("receiver %s at %s", typeName.Name(), fileSet.Position(typeName.Pos())),
		}
		if len(names) == 0 {
			decided = decision{category: "receiver whose type gives no usable name", detail: typeName.Name()}
		}
		for _, variable := range group.letters {
			decisions[variable] = decided
		}
	}
	return decisions
}

// packageNamesFor is the name of three letters or more the package's own code most often gives a variable
// holding a value of typeName, or a pointer to one; a name it gives one value among others says what that value
// is, not what the type is.
func packageNamesFor(info *types.Info, files []*ast.File, typeName *types.TypeName) []string {
	counts := map[string]int{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier || len(identifier.Name) < 3 {
				return true
			}
			if variable, isVariable := info.Defs[identifier].(*types.Var); isVariable && !variable.IsField() &&
				receiverTypeName(variable) == typeName {
				counts[variable.Name()]++
			}
			return true
		})
	}
	names := sortedKeys(counts)
	sort.SliceStable(names, func(left, right int) bool { return counts[names[left]] > counts[names[right]] })
	return names[:min(len(names), 1)]
}

// receiverTypeName is the declared type a receiver's method belongs to: the origin of a generic type, through a
// pointer and any alias.
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

// typeSwitchCandidates names the letter a type switch binds, `switch t := terminal.(type)`, as the house writes
// it, `typed`, and `typedTerminal` when that is taken: one candidate for each clause's variable, all in one group.
func typeSwitchCandidates(info *types.Info, fileSet *token.FileSet, statement *ast.TypeSwitchStmt,
	importNames map[string]bool, stack []ast.Node) []candidate {
	assignment, isAssignment := statement.Assign.(*ast.AssignStmt)
	if !isAssignment || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return nil
	}
	guard, isIdentifier := assignment.Lhs[0].(*ast.Ident)
	if !isIdentifier || !isJudgedLetter(guard.Name) {
		return nil
	}
	names := []string{"typed"}
	if assertion, isAssertion := ast.Unparen(assignment.Rhs[0]).(*ast.TypeAssertExpr); isAssertion {
		if subject := collectionName(assertion.X); len(subject) > 1 {
			names = append(names, "typed"+strings.ToUpper(subject[:1])+subject[1:])
		}
	}
	names = usableNames(names, importNames)
	var candidates []candidate
	for _, clause := range statement.Body.List {
		variable, isVariable := info.Implicits[clause].(*types.Var)
		if !isVariable {
			continue
		}
		candidates = append(candidates, candidate{
			variable: variable, names: names, rule: "type switch", call: guard.Name,
			group:    "type switch at " + fileSet.Position(guard.Pos()).String(),
			function: enclosingFunction(stack),
		})
	}
	return candidates
}

// declarationRecord is one candidate's outcome, keyed by where it is declared, so a file seen by two
// package variants counts once, and a type switch's clause variables, declared at its guard, count once.
type declarationRecord struct {
	position string
	filename string
	offset   int
	oldName  string
	// names are the names the rules gave, best first; newName is the one the engine accepted.
	names    []string
	newName  string
	rule     string
	category string
	call     string
	function string
	// refusal is the engine's reason for the last name, when it refused every name the rules gave.
	refusal string
}

// settle runs the engine until every candidate holds a name it accepts or has run out of names: a candidate,
// or every candidate of its group together, whose name the engine refuses tries its next name, and the whole set
// is judged again. Each round moves at least one group forward and none back, so this ends. It returns the
// final plan and, for every group that ran out, the engine's reason for its last name.
func settle(renamePackage rename.Package, candidates []candidate) (rename.Plan, map[string]string) {
	groupOf := map[types.Object]string{}
	for _, decided := range candidates {
		groupOf[decided.variable] = groupKey(decided)
	}
	step := map[string]int{}
	refusals := map[string]string{}
	for {
		var proposals []rename.Proposal
		for _, decided := range candidates {
			if current := step[groupKey(decided)]; current < len(decided.names) {
				proposals = append(
					proposals,
					rename.Proposal{Object: decided.variable, NewName: decided.names[current]},
				)
			}
		}
		plan := rename.Check(renamePackage, proposals)
		refused := map[string]string{}
		for _, rejection := range plan.Rejections {
			if _, recorded := refused[groupOf[rejection.Object]]; !recorded {
				refused[groupOf[rejection.Object]] = rejection.NewName + ": " + rejection.Reason
			}
		}
		if len(refused) == 0 {
			return plan, refusals
		}
		for group, reason := range refused {
			step[group]++
			refusals[group] = reason
		}
	}
}

// groupKey is the key a candidate moves through its names under: its group, or its own declaration.
func groupKey(decided candidate) string {
	if decided.group != "" {
		return decided.group
	}
	return fmt.Sprintf("variable %d", decided.variable.Pos())
}

func run(root string, skips []string, dryRun bool) int {
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(streams.Errors, "%s: loading: %v\n", commandName, loadError)
		return 1
	}
	vocabularyFile, readError := os.ReadFile(filepath.Join(loaded.Root, "policy", "Abbreviations.json"))
	if readError != nil {
		fmt.Fprintf(streams.Errors, "%s: %v\n", commandName, readError)
		return 1
	}
	words, vocabularyError := parseVocabulary(vocabularyFile)
	if vocabularyError != nil {
		fmt.Fprintf(streams.Errors, "%s: policy/Abbreviations.json: %v\n", commandName, vocabularyError)
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
	// allCommentEdits are the doc comment respellings among allEdits, printed for review.
	var allCommentEdits []rename.Edit
	var allUnresolved []string
	records := map[string]*declarationRecord{}
	var recordOrder []string
	for _, analyzedPackage := range loaded.Analyzed {
		renamePackage := loaded.RenamePackage(analyzedPackage)
		candidates := findCandidates(
			analyzedPackage.Types,
			analyzedPackage.TypesInfo,
			analyzedPackage.Fset,
			renamePackage.Files,
			words,
		)
		plan, refusals := settle(renamePackage, candidates)
		for _, decided := range candidates {
			position := analyzedPackage.Fset.PositionFor(decided.variable.Pos(), false)
			key := fmt.Sprintf("%s:%d", position.Filename, position.Offset)
			if _, seen := records[key]; seen {
				continue
			}
			record := &declarationRecord{
				position: relativePosition(loaded, position),
				filename: position.Filename,
				offset:   position.Offset,
				oldName:  decided.variable.Name(),
				names:    decided.names,
				newName:  plan.Accepted[decided.variable],
				rule:     decided.rule,
				category: decided.category,
				call:     decided.call,
				function: decided.function,
			}
			if record.newName == "" && len(decided.names) > 0 {
				record.refusal = strings.ReplaceAll(refusals[groupKey(decided)], loaded.Root+"/", "")
			}
			records[key] = record
			recordOrder = append(recordOrder, key)
		}
		for _, unresolved := range plan.Unresolved {
			allUnresolved = append(allUnresolved, analyzedPackage.ID+": "+unresolved)
		}
		allEdits = append(allEdits, rename.Edits(renamePackage, plan.Accepted)...)
		commentEdits := docCommentEdits(renamePackage, plan.Accepted)
		allEdits = append(allEdits, commentEdits...)
		allCommentEdits = append(allCommentEdits, commentEdits...)
	}

	fieldResult, fields := planFields(loaded, fieldCandidates(loaded, words))
	allEdits = append(allEdits, fieldResult.Edits...)
	others := otherLetterDeclarations(loaded)

	edits, disagreements := rename.Deduplicate(allEdits)
	editsByFile := rename.GroupByFile(edits)

	candidateCount, eligibleCount, acceptedCount := 0, 0, 0
	refusedByReason, notEligibleByCategory, acceptedNames, acceptedRules, nameSteps :=
		map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	for _, key := range recordOrder {
		record := records[key]
		candidateCount++
		switch {
		case len(record.names) == 0:
			notEligibleByCategory[record.category]++
		case record.newName == "":
			eligibleCount++
			refusedByReason[refusalClass(record.refusal)]++
		default:
			eligibleCount++
			acceptedCount++
			acceptedNames[record.newName]++
			acceptedRules[record.rule]++
			nameSteps[fmt.Sprintf("name %d of %d", slices.Index(record.names, record.newName)+1, len(record.names))]++
		}
	}
	fmt.Fprintf(streams.Output, "candidates (single-letter locals the analyzer flags): %d\n", candidateCount)
	fmt.Fprintf(streams.Output, "eligible (a rule gave names): %d\n", eligibleCount)
	fmt.Fprintf(streams.Output, "accepted by the engine: %d\n", acceptedCount)
	printCounts(acceptedRules, 0)
	fmt.Fprintln(streams.Output, "which of its names each accepted letter took:")
	printCounts(nameSteps, 0)
	fmt.Fprintf(streams.Output, "refused by the engine, every name: %d\n", eligibleCount-acceptedCount)
	printCounts(refusedByReason, 0)
	fmt.Fprintf(streams.Output, "not eligible, left for the waves: %d\n", candidateCount-eligibleCount)
	printCounts(notEligibleByCategory, 0)
	fmt.Fprintln(streams.Output, "most frequent new names (accepted):")
	printCounts(acceptedNames, 25)
	fieldsAccepted, fieldsRefused, fieldsLeft := 0, 0, 0
	for _, field := range fields {
		newName, refusal := fieldOutcome(fieldResult, field)
		switch {
		case newName != "":
			fieldsAccepted++
		case refusal != "":
			fieldsRefused++
		default:
			fieldsLeft++
		}
	}
	fmt.Fprintf(
		streams.Output,
		"single-letter struct fields: %d, renamed through globalrename: %d, refused: %d, left for the waves: %d\n",
		len(fields),
		fieldsAccepted,
		fieldsRefused,
		fieldsLeft,
	)
	fmt.Fprintf(streams.Output, "other single-letter declarations, left for the waves: %d\n", len(others))
	fmt.Fprintf(streams.Output, "edits: %d in %d files\n", len(edits), len(editsByFile))
	fmt.Fprintf(streams.Output, "variant disagreements: %d\n", len(disagreements))
	fmt.Fprintf(streams.Output, "unresolved occurrences (scope model gaps): %d\n", len(allUnresolved))

	fmt.Fprintln(streams.Output, "\nrefused (every name), left for the waves:")
	for _, key := range recordOrder {
		if record := records[key]; len(record.names) > 0 && record.newName == "" {
			fmt.Fprintf(
				streams.Output,
				"  %s: %s in %s, tried %s: %s\n",
				record.position,
				record.oldName,
				record.function,
				strings.Join(record.names, ", "),
				record.refusal,
			)
		}
	}
	fmt.Fprintln(streams.Output, "\nnot eligible, left for the waves:")
	for _, key := range recordOrder {
		if record := records[key]; len(record.names) == 0 {
			fmt.Fprintf(
				streams.Output,
				"  %s: %s in %s: %s %s\n",
				record.position,
				record.oldName,
				record.function,
				record.category,
				record.call,
			)
		}
	}
	fmt.Fprintln(streams.Output, "\naccepted (declaring line before | after):")
	for _, key := range recordOrder {
		if record := records[key]; record.newName != "" {
			before, after := lineBeforeAndAfter(record.filename, record.offset, editsByFile[record.filename])
			fmt.Fprintf(
				streams.Output,
				"  %s: %s -> %s [%s %s]\n      %s\n    | %s\n",
				record.position,
				record.oldName,
				record.newName,
				record.rule,
				record.call,
				before,
				after,
			)
		}
	}
	fmt.Fprintln(streams.Output, "\ndoc comment respellings:")
	commentEdits, _ := rename.Deduplicate(allCommentEdits)
	for _, edit := range commentEdits {
		before, after := lineBeforeAndAfter(edit.Filename, edit.Offset, editsByFile[edit.Filename])
		fmt.Fprintf(
			streams.Output,
			"  %s: %s -> %s\n      %s\n    | %s\n",
			relativeOffset(loaded, edit),
			edit.OldText,
			edit.NewText,
			before,
			after,
		)
	}
	fmt.Fprintln(streams.Output, "\nstruct fields:")
	for _, field := range fields {
		position := relativePosition(loaded, field.goPackage.Fset.PositionFor(field.variable.Pos(), false))
		newName, refusal := fieldOutcome(fieldResult, field)
		switch {
		case newName != "":
			fmt.Fprintf(
				streams.Output,
				"  %s: %s -> %s [%s, a field of %s]\n",
				position,
				field.variable.Name(),
				newName,
				field.rule,
				field.structName,
			)
		case refusal != "":
			fmt.Fprintf(
				streams.Output,
				"  %s: %s of %s, refused %s: %s\n",
				position,
				field.variable.Name(),
				field.structName,
				field.newName,
				refusal,
			)
		default:
			fmt.Fprintf(
				streams.Output,
				"  %s: %s of %s: %s\n",
				position,
				field.variable.Name(),
				field.structName,
				field.category,
			)
		}
	}
	fmt.Fprintln(
		streams.Output,
		"\nfield comment respellings globalrename proposed, not applied (a letter in prose is a word):",
	)
	for _, edit := range fieldResult.CommentEdits {
		relative, _ := loaded.Relative(edit.Filename)
		fmt.Fprintf(streams.Output, "  %s offset %d: %s -> %s\n", relative, edit.Offset, edit.OldText, edit.NewText)
	}
	fmt.Fprintln(streams.Output, "\nglobalrename model notes:")
	for _, note := range append(append(append(fieldResult.Unresolved, fieldResult.SelectionModelMismatches...),
		fieldResult.SatisfactionModelMismatches...), fieldResult.Notes...) {
		fmt.Fprintln(streams.Output, "  "+strings.ReplaceAll(note, loaded.Root+"/", ""))
	}
	fmt.Fprintln(streams.Output, "\nother single-letter declarations:")
	for _, other := range others {
		fmt.Fprintln(streams.Output, "  "+other)
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

// refusalClass groups the engine's reasons for counting: a conflict with a name in scope or another rename,
// or the reason itself.
func refusalClass(reason string) string {
	if _, detail, found := strings.Cut(reason, ": "); found {
		if strings.HasPrefix(detail, "conflict:") {
			return "conflict with a name in scope or another rename"
		}
		return detail
	}
	return reason
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
	return workspace.ApplyVerified(loaded, root, skips, edits, editsByFile, "loopnames", streams.Output, streams.Errors)
}

// relativeOffset prints where an edit falls, relative to the root, by line.
func relativeOffset(loaded *workspace.Workspace, edit rename.Edit) string {
	source, readError := os.ReadFile(edit.Filename)
	line := 0
	if readError == nil && edit.Offset <= len(source) {
		line = strings.Count(string(source[:edit.Offset]), "\n") + 1
	}
	relative, _ := loaded.Relative(edit.Filename)
	return fmt.Sprintf("%s:%d", relative, line)
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
