package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/tools/go/types/typeutil"
)

// The rules in this file name a letter no loop or comparator names, from what its declaration says it holds:
// a pair of parameters, the collection a function literal is handed with, the call that produced it, its type,
// and last the conventional meaning of a few letters for a basic type. Each rule gives names in order of
// preference, and the engine takes the first it accepts.

// typeTable names the standard and common library types a letter holds, by their full type string, decided
// once: a type whose own name is a keyword, or whose last word says less than the name the tree already uses
// for it (`subjectType` is how cohere's rules name the TypeScript checker's type). An error maps to no name: the
// errors rule names one for the call that produced it, which callResultNames does.
var typeTable = map[string]string{
	"*testing.T":        "test",
	"testing.TB":        "test",
	"*testing.B":        "benchmark",
	"*testing.F":        "fuzz",
	"*testing.M":        "suite",
	"*os.File":          "file",
	"io.Reader":         "reader",
	"io.ReadCloser":     "reader",
	"io.Writer":         "writer",
	"*strings.Builder":  "builder",
	"strings.Builder":   "builder",
	"*bytes.Buffer":     "buffer",
	"bytes.Buffer":      "buffer",
	"go/token.Pos":      "position",
	"*go/token.FileSet": "fileSet",
	"go/types.Type":     "resolvedType",
	"*go/types.Package": "typesPackage",
	"*go/types.Struct":  "structType",
	"error":             "",
	"*github.com/microsoft/TypeScript/tsc/shim/checker.Type":     "subjectType",
	"*github.com/microsoft/TypeScript/tsc/internal/checker.Type": "subjectType",
	"*github.com/microsoft/TypeScript/tsc/shim/ast.SourceFile":   "sourceFile",
}

// keywordSubstitutes name a type whose every word-based name is a keyword: a `Range` alone reads `span`.
var keywordSubstitutes = map[string]string{
	"range":  "span",
	"func":   "function",
	"map":    "mapping",
	"struct": "structType",
}

// typeParameterNames name a value of a type parameter by the parameter's conventional letter.
var typeParameterNames = map[string]string{"E": "element", "K": "key", "V": "value", "T": "value"}

// letterConvention is the name a letter's conventional meaning gives it for a basic type: a rune or a byte is a
// character whatever its letter, an integer i or j is an index, a string s is text, a float n a number, and a v
// is a value. An integer n is a count only where its declaration says so: a counter started at zero, a value a
// call named for a count produces, or a parameter of a function named for one; elsewhere code ported from a
// specification or another language spells offsets, ends and exponents with it too. value is what the letter is
// declared to hold, or nil, and function the name of the function it is declared in. Any other letter of a
// basic type has no name to take and is left for the waves.
func letterConvention(letter string, basic *types.Basic, value ast.Expr, function string) string {
	switch {
	case basic.Name() == "rune" || basic.Name() == "byte":
		return "character"
	case letter == "v":
		return "value"
	case basic.Info()&types.IsString != 0:
		if letter == "s" {
			return "text"
		}
	case basic.Info()&types.IsInteger != 0:
		switch letter {
		case "i", "j":
			return "index"
		case "n":
			if isCountDeclaration(value, function) {
				return "count"
			}
		}
	case basic.Info()&types.IsFloat != 0:
		if letter == "n" {
			return "number"
		}
	}
	return ""
}

// isCountDeclaration reports whether an integer n's declaration says it counts: it starts at the literal zero,
// a call named for a count produces it (`lexer.pushCount(1) + lexer.pushSpaces(true)`), or the function it is a
// parameter of is named for one (`pushCount(n int)`).
func isCountDeclaration(value ast.Expr, function string) bool {
	if value == nil {
		return strings.Contains(function, "Count")
	}
	if literal, isLiteral := value.(*ast.BasicLit); isLiteral && literal.Kind == token.INT && literal.Value == "0" {
		return true
	}
	counted := false
	ast.Inspect(value, func(node ast.Node) bool {
		if call, isCall := node.(*ast.CallExpr); isCall && strings.Contains(collectionName(call.Fun), "Count") {
			counted = true
		}
		return !counted
	})
	return counted
}

// parameterPairs are the letter pairs that name two parameters of one list together: `a, b` of one type are the
// left and right operands of a comparison or a merge, and integer `s, e` are a start and an end.
var parameterPairs = []struct {
	first, second string
	integerOnly   bool
	newFirst      string
	newSecond     string
}{
	{first: "a", second: "b", newFirst: "left", newSecond: "right"},
	{first: "s", second: "e", integerOnly: true, newFirst: "start", newSecond: "end"},
}

// pairDecisions names every letter pair in a parameter list that a comparator has not named already: the two
// must be adjacent, `a` before `b`, and of identical types.
// Mutates: decisions the pairs join the one table of the file's decisions, read for what a comparator named
func pairDecisions(info *types.Info, fields *ast.FieldList, decisions map[*types.Var]decision) {
	if fields == nil {
		return
	}
	var names []*ast.Ident
	for _, field := range fields.List {
		names = append(names, field.Names...)
	}
	for index := 0; index+1 < len(names); index++ {
		first, second := variableOf(info, names[index]), variableOf(info, names[index+1])
		if first == nil || second == nil || !types.Identical(first.Type(), second.Type()) {
			continue
		}
		_, firstDecided := decisions[first]
		_, secondDecided := decisions[second]
		if firstDecided || secondDecided {
			continue
		}
		for _, pair := range parameterPairs {
			if first.Name() != pair.first || second.Name() != pair.second {
				continue
			}
			if basic, isBasic := first.Type().Underlying().(*types.Basic); pair.integerOnly &&
				(!isBasic || basic.Info()&types.IsInteger == 0) {
				continue
			}
			decisions[first] = decision{names: []string{pair.newFirst}, rule: "parameter pair"}
			decisions[second] = decision{names: []string{pair.newSecond}, rule: "parameter pair"}
		}
	}
}

// resultName is the name a letter takes from one result of a standard library call, by package path, function
// name and result position.
type resultName struct {
	packagePath string
	name        string
	names       []string
}

// resultNames are the standard library calls whose results have conventional names: a decoded rune and its
// size, the bytes read and the error reading them, a parsed number and the error parsing it.
var resultNames = []resultName{
	{packagePath: "unicode/utf8", name: "DecodeRune", names: []string{"character", "size"}},
	{packagePath: "unicode/utf8", name: "DecodeRuneInString", names: []string{"character", "size"}},
	{packagePath: "unicode/utf8", name: "DecodeLastRune", names: []string{"character", "size"}},
	{packagePath: "unicode/utf8", name: "DecodeLastRuneInString", names: []string{"character", "size"}},
	{packagePath: "io", name: "ReadAll", names: []string{"content", "readError"}},
	{packagePath: "os", name: "ReadFile", names: []string{"content", "readError"}},
	{packagePath: "strconv", name: "ParseInt", names: []string{"number", "parseError"}},
	{packagePath: "strconv", name: "ParseUint", names: []string{"number", "parseError"}},
	{packagePath: "strconv", name: "ParseFloat", names: []string{"number", "parseError"}},
	{packagePath: "strconv", name: "Atoi", names: []string{"number", "parseError"}},
	{packagePath: "archive/zip", name: "Open", names: []string{"reader", "openError"}},
}

// producingCall is the call a variable is declared to hold a result of, and that result's position: `v := f()`,
// `_, v := f()` or `var v = f()`. It is nil for any other shape.
func producingCall(declaration ast.Node, identifier *ast.Ident) (*ast.CallExpr, int) {
	var declared []ast.Expr
	var values []ast.Expr
	switch typed := declaration.(type) {
	case *ast.AssignStmt:
		if typed.Tok != token.DEFINE {
			return nil, 0
		}
		declared, values = typed.Lhs, typed.Rhs
	case *ast.ValueSpec:
		for _, name := range typed.Names {
			declared = append(declared, name)
		}
		values = typed.Values
	default:
		return nil, 0
	}
	for position, expression := range declared {
		if expression != identifier {
			continue
		}
		// One value on the right is a call whose results the names on the left hold in order; one value per name
		// is a call whose only result the name holds.
		var value ast.Expr
		resultPosition := 0
		switch {
		case len(values) == 1:
			value, resultPosition = values[0], position
		case position < len(values):
			value = values[position]
		default:
			return nil, 0
		}
		call, isCall := ast.Unparen(value).(*ast.CallExpr)
		if !isCall {
			return nil, 0
		}
		return call, resultPosition
	}
	return nil, 0
}

// callResultNames names a letter from the call that produced it: a conventional result of a standard library
// call, or an error named for its call as the errors rule names one (`openError`).
func callResultNames(
	info *types.Info,
	call *ast.CallExpr,
	position int,
	variable *types.Var,
	words vocabulary,
) []string {
	function, isFunction := typeutil.Callee(info, call).(*types.Func)
	if !isFunction || function.Pkg() == nil {
		return nil
	}
	for _, entry := range resultNames {
		if function.Pkg().Path() == entry.packagePath && function.Name() == entry.name && position < len(entry.names) {
			return []string{entry.names[position]}
		}
	}
	if isErrorType(variable.Type()) {
		if name, _ := fullWordsLowerCamel(function.Name()+"Error", words); name != "" {
			return []string{name}
		}
	}
	return nil
}

// isErrorType reports whether a type is the predeclared error.
func isErrorType(candidate types.Type) bool {
	return types.Identical(candidate, types.Universe.Lookup("error").Type())
}

// namesFromType are the names a type gives the letter holding it, best first: the table's name, the last word
// of a named type's name, its last two words, its whole name, and a substitute when each of those is a keyword;
// a slice's element names in the plural; a type parameter's conventional name; a function that answers yes or no
// is a predicate and one that maps a value to another a transform.
func namesFromType(declared types.Type, words vocabulary) []string {
	if name, inTable := typeTable[types.TypeString(declared, nil)]; inTable {
		if name == "" {
			return nil
		}
		return []string{name}
	}
	unwrapped := types.Unalias(declared)
	if pointer, isPointer := unwrapped.(*types.Pointer); isPointer {
		unwrapped = types.Unalias(pointer.Elem())
	}
	switch typed := unwrapped.(type) {
	case *types.TypeParam:
		if name, conventional := typeParameterNames[typed.Obj().Name()]; conventional {
			return []string{name}
		}
	case *types.Named:
		return namesFromTypeName(typed.Obj().Name(), words)
	case *types.Slice:
		var plurals []string
		for _, singular := range namesFromType(typed.Elem(), words) {
			plurals = append(plurals, pluralOf(singular))
		}
		return plurals
	case *types.Signature:
		results := typed.Results()
		if results.Len() == 1 {
			if basic, isBasic := results.At(0).Type().(*types.Basic); isBasic && basic.Kind() == types.Bool {
				return []string{"predicate"}
			}
			if typed.Params().Len() == 1 {
				return []string{"transform"}
			}
		}
	}
	return nil
}

// namesFromTypeName are the names a named type's name gives, best first: its last word, its last two words, its
// whole name, each in full house words, and a substitute when the type's name is one keyword.
func namesFromTypeName(typeName string, words vocabulary) []string {
	parts := camelWords(typeName)
	var names []string
	add := func(name string) {
		for _, existing := range names {
			if existing == name {
				return
			}
		}
		names = append(names, name)
	}
	for _, count := range []int{1, 2, len(parts)} {
		if count > len(parts) {
			continue
		}
		name := houseLowerCamel(parts[len(parts)-count:], words)
		if name == "" {
			continue
		}
		if token.IsKeyword(name) {
			if substitute, found := keywordSubstitutes[name]; found && count == len(parts) {
				add(substitute)
			}
			continue
		}
		add(name)
	}
	return names
}

// houseLowerCamel joins camel words as the house spells a local: every vocabulary word expanded, an initialism
// word-cased (`SSA` to `Ssa`), and the first word lowered whole. It returns "" when a word has no mechanical
// expansion.
func houseLowerCamel(parts []string, words vocabulary) string {
	var builder strings.Builder
	for index, part := range parts {
		lowered := strings.ToLower(part)
		if words.adviceWords[lowered] || words.matcherWords[lowered] {
			return ""
		}
		word := part
		if expansion, abbreviated := words.expansions[lowered]; abbreviated {
			word = expansion
		} else if isInitialism(part) || index == 0 {
			word = lowered
		}
		if index > 0 {
			word = strings.ToUpper(word[:1]) + word[1:]
		} else {
			word = strings.ToLower(word[:1]) + word[1:]
		}
		builder.WriteString(word)
	}
	return builder.String()
}

// isInitialism reports whether a camel word is two or more capitals.
func isInitialism(word string) bool {
	if len(word) < 2 {
		return false
	}
	for _, letter := range word {
		if !unicode.IsUpper(letter) {
			return false
		}
	}
	return true
}

// pluralOf is the plural of a lowerCamel name, by its last word's ending: entry to entries, match to matches,
// violation to violations.
func pluralOf(name string) string {
	switch {
	case strings.HasSuffix(name, "y") && len(name) > 1 && !strings.ContainsRune("aeiou", rune(name[len(name)-2])):
		return strings.TrimSuffix(name, "y") + "ies"
	case strings.HasSuffix(name, "s"), strings.HasSuffix(name, "x"), strings.HasSuffix(name, "z"),
		strings.HasSuffix(name, "ch"), strings.HasSuffix(name, "sh"):
		return name + "es"
	}
	return name + "s"
}

// elementType is the type a range over collection yields as its value, or nil when it is not a slice, array,
// map, string or one-value iterator.
func elementType(collection types.Type) types.Type {
	underlying := collection.Underlying()
	if pointer, isPointer := underlying.(*types.Pointer); isPointer {
		underlying = pointer.Elem().Underlying()
	}
	switch typed := underlying.(type) {
	case *types.Slice:
		return typed.Elem()
	case *types.Array:
		return typed.Elem()
	case *types.Map:
		return typed.Elem()
	case *types.Signature:
		// An iterator, `func(yield func(V) bool)`, yields V.
		if typed.Params().Len() == 1 {
			if yield, isSignature := typed.Params().At(0).Type().Underlying().(*types.Signature); isSignature &&
				yield.Params().Len() == 1 {
				return yield.Params().At(0).Type()
			}
		}
	}
	return nil
}

// literalCollectionNames names the first parameter of a function literal handed to a call together with a
// collection whose elements it receives, `Some(declarations, func(d *ast.Node) bool {...})`: the singular of
// the collection's name, or of the last word of the call that produced it (`UnionTypeParts(t)` gives part).
func literalCollectionNames(info *types.Info, call *ast.CallExpr, literal *ast.FuncLit, variable *types.Var,
	words vocabulary) []string {
	for _, argument := range call.Args {
		if ast.Unparen(argument) == literal {
			break
		}
		argumentType := info.TypeOf(argument)
		if argumentType == nil {
			continue
		}
		element := elementType(argumentType)
		if element == nil || !types.Identical(element, variable.Type()) {
			continue
		}
		if singular := collectionSingular(argument, words); singular != "" {
			return []string{singular}
		}
	}
	return nil
}

// genericPlurals are the selected names that say a collection holds nodes or items without saying of what.
var genericPlurals = map[string]bool{"Nodes": true, "NodeList": true, "Items": true, "Elements": true}

// collectionKeywordSubstitutes name an element whose collection's singular is a keyword: the types a union or
// intersection is made of, `subjectType.Types()`, are its member types.
var collectionKeywordSubstitutes = map[string]string{"type": "memberType"}

// collectionSingular is the singular of the name a collection expression is spelled with: an identifier's or a
// selector's whole name (`declarationFiles` gives declarationFile), or the last word of a call's callee
// (`UnionTypeParts(t)` gives part). It is "" when that name is not a plural or its singular is not a name.
func collectionSingular(expression ast.Expr, words vocabulary) string {
	collection := ast.Unparen(expression)
	if call, isCall := collection.(*ast.CallExpr); isCall {
		name := collectionName(call.Fun)
		if name == "" {
			return ""
		}
		parts := camelWords(name)
		// An iterator's `Seq` suffix says how the collection is handed over, not what it holds.
		if len(parts) > 1 && parts[len(parts)-1] == "Seq" {
			parts = parts[:len(parts)-1]
		}
		singular, plural := singularWord(strings.ToLower(parts[len(parts)-1]))
		if !plural || len(singular) < 3 {
			return ""
		}
		if substitute, found := collectionKeywordSubstitutes[singular]; found {
			return substitute
		}
		newName, _ := fullWordsLowerCamel(singular, words)
		if newName == "" || token.IsKeyword(newName) || types.Universe.Lookup(newName) != nil {
			return ""
		}
		return newName
	}
	// A generic plural says only that a collection holds nodes; the name it is selected from says of what:
	// `heritageClauses.Nodes` holds heritage clauses, `modifiers.NodeList.Nodes` modifiers.
	for selector, isSelector := collection.(*ast.SelectorExpr); isSelector && genericPlurals[selector.Sel.Name]; {
		if singular, _ := singularOf(selector.X, words); singular != "" {
			return singular
		}
		selector, isSelector = ast.Unparen(selector.X).(*ast.SelectorExpr)
	}
	newName, _ := singularOf(collection, words)
	return newName
}

// confusingPackageNames are standard library packages a reader would take a variable of that name for, imported
// in the file or not.
var confusingPackageNames = map[string]bool{
	"bytes": true, "context": true, "errors": true, "filepath": true, "fmt": true, "io": true, "json": true, "os": true,
	"path": true, "regexp": true, "sort": true, "strings": true, "sync": true, "testing": true, "time": true,
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

// usableNames keeps the names a letter may take in file, each once: an identifier of three letters or more that is not
// a keyword, a predeclared identifier, an import's name in the file, or a standard library package a reader would
// take it for. A conflict with a name in scope is the engine's to find.
func usableNames(names []string, importNames map[string]bool) []string {
	var usable []string
	for _, name := range names {
		if !token.IsIdentifier(name) || len(name) < 3 || token.IsKeyword(name) || types.Universe.Lookup(name) != nil ||
			importNames[name] || confusingPackageNames[name] || slices.Contains(usable, name) {
			continue
		}
		usable = append(usable, name)
	}
	return usable
}

// declaredValue is the expression a variable is declared to hold when its declaration gives each name its own
// value, `before, after := lines[0], lines[1]` or `var v = f()`, or nil for any other shape.
func declaredValue(declaration ast.Node, identifier *ast.Ident) ast.Expr {
	var declared []ast.Expr
	var values []ast.Expr
	switch typed := declaration.(type) {
	case *ast.AssignStmt:
		if typed.Tok != token.DEFINE {
			return nil
		}
		declared, values = typed.Lhs, typed.Rhs
	case *ast.ValueSpec:
		for _, name := range typed.Names {
			declared = append(declared, name)
		}
		values = typed.Values
	default:
		return nil
	}
	if len(declared) != len(values) {
		return nil
	}
	for position, expression := range declared {
		if expression == identifier {
			return ast.Unparen(values[position])
		}
	}
	return nil
}

// fixtureLetterNames names a letter that holds what a call made from that same letter spelled as a string,
// `a := fixture.worktree("a")`: the letter is the fixture's own name, so the variable is the callee's word
// with the letter after it, worktreeA.
func fixtureLetterNames(value ast.Expr, letter string, words vocabulary) []string {
	var names []string
	ast.Inspect(value, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall || len(names) > 0 || len(call.Args) != 1 {
			return len(names) == 0
		}
		literal, isLiteral := call.Args[0].(*ast.BasicLit)
		if !isLiteral || literal.Kind != token.STRING {
			return true
		}
		if text, unquoteError := strconv.Unquote(literal.Value); unquoteError != nil || text != letter {
			return true
		}
		if callee, _ := fullWordsLowerCamel(collectionName(call.Fun), words); callee != "" {
			names = append(names, callee+strings.ToUpper(letter))
		}
		return false
	})
	return names
}

// indexedElementNames names a letter that holds one element of a plurally named collection,
// `before := beforeLines[index]`: the collection's singular.
func indexedElementNames(value ast.Expr, words vocabulary) []string {
	index, isIndex := value.(*ast.IndexExpr)
	if !isIndex {
		return nil
	}
	if singular := collectionSingular(index.X, words); singular != "" {
		return []string{singular}
	}
	return nil
}

// getterNames names a letter of a basic type that holds what a getter returns, a call with no arguments whose
// name is one word, `n := named.Obj().Name()`: that word.
func getterNames(value ast.Expr, words vocabulary) []string {
	call, isCall := value.(*ast.CallExpr)
	if !isCall || len(call.Args) != 0 {
		return nil
	}
	name := collectionName(call.Fun)
	if name == "" || len(camelWords(name)) != 1 {
		return nil
	}
	if word, _ := fullWordsLowerCamel(name, words); word != "" {
		return []string{word}
	}
	return nil
}
