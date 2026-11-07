// Command errornames names every error for what produced it. A local `err` (or an error named `e`) written
// once takes a name from its call: a table of standard library calls first (matched through types, never by
// spelling), else the function's name in full words plus Error, a function value's name, or for a call of a
// call's result the function that made it. A local written more than once is split into one variable per
// web of reaching definitions (splitter.go), each named the same way. A named result takes its function's name
// and a walk callback's error parameter is pathError, each split too when the body reuses it. A name that
// abbreviates error (`readErr`, `errOne`) is spelled out. A second phase renames sentinels, error-making
// functions and fields the same way through the cross-package engine (globals.go).
//
// Whole renames go through the rename engine, which refuses any that would change what an identifier
// resolves to. Every package is then rehearsed in memory (verify.go): rewritten, type-checked, every
// identifier compared, and every changed line held to the house width, dropping a declaration's decisions
// until it passes. After writing, the tree is reloaded and every identifier's resolution compared again.
// Everything it leaves alone is reported with a reason, for the hand-written waves.
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
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/tools/go_codemods/rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// commandName prefixes every message this command prints to standard error.
const commandName = "errornames"

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code errornames exits with: runCommandLine returns its exit code here, and
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

// runCommandLine reads the flags and runs errornames, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print counts and every rejection, write nothing")
	fixup := flag.Bool(
		"fixup",
		false,
		"rename locals already named newError, forError or ofError by the New, For and Of qualifier rule",
	)
	root := flag.String("root", ".", "the worktree root, holding go.work")
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	flag.Parse()
	return run(*root, skips, *dryRun, *fixup)
}

// candidate is one local named err and what the generator decided for it.
type candidate struct {
	variable *types.Var
	// newName is the proposed name, or "" when the variable is not eligible.
	newName string
	// category says why the variable is not eligible, or "" when it is.
	category string
	// call is the producing call's function, qualified, when the definition has one; for a variable of the
	// wrong type, its type.
	call string
	// split is the plan for a variable written more than once that becomes several, when it does.
	split *splitPlan
	// pending is a parameter its callers name, decided once every package's locals are.
	pending *pendingParameter
}

// pendingParameter is an error parameter no walk names: its callers decide it, read through types, or else
// the function's job does.
type pendingParameter struct {
	// callee is the declaration position of what its callers call: the function, or the variable a literal
	// is bound to.
	callee string
	// index is the parameter's place among the arguments.
	index int
	// function is the function's or binding's name, and tests reports whether it returns only a bool.
	function string
	tests    bool
	// typeCheckErrors marks the Error callback of a go/types Config.
	typeCheckErrors bool
	// resolve settles the candidate under a name: a whole rename, or a split when the body writes it.
	resolve func(newName string) (string, string, *splitPlan)
}

// typeName is a named type by package path and name.
type typeName struct {
	packagePath string
	name        string
}

// tableEntry names the error of one standard library call.
type tableEntry struct {
	packagePath string
	// receiver is the receiver's type name for a method, "" for a package function.
	receiver string
	name     string
	// namePrefix matches every function whose name starts with name (strconv.Parse*).
	namePrefix bool
	// newName is the error's name; "" marks a constructed error, which is named by meaning in the waves.
	newName string
}

// errorTable is the spec's override table, matched before the default.
var errorTable = []tableEntry{
	{packagePath: "os", name: "ReadFile", newName: "readError"},
	{packagePath: "io", name: "ReadAll", newName: "readError"},
	{packagePath: "os", name: "WriteFile", newName: "writeError"},
	{packagePath: "os", name: "MkdirAll", newName: "createDirectoryError"},
	{packagePath: "os", name: "Mkdir", newName: "createDirectoryError"},
	{packagePath: "os", name: "MkdirTemp", newName: "temporaryDirectoryError"},
	{packagePath: "os", name: "CreateTemp", newName: "temporaryFileError"},
	{packagePath: "os", name: "ReadDir", newName: "readDirectoryError"},
	{packagePath: "os", name: "Stat", newName: "statError"},
	{packagePath: "os", name: "Lstat", newName: "statError"},
	{packagePath: "os", name: "Open", newName: "openError"},
	{packagePath: "os", name: "OpenFile", newName: "openError"},
	{packagePath: "os", name: "Create", newName: "createError"},
	{packagePath: "os", name: "Remove", newName: "removeError"},
	{packagePath: "os", name: "RemoveAll", newName: "removeError"},
	{packagePath: "os", name: "Rename", newName: "renameError"},
	{packagePath: "os", name: "Symlink", newName: "symlinkError"},
	{packagePath: "os", name: "Readlink", newName: "readLinkError"},
	{packagePath: "os", name: "Chtimes", newName: "changeTimesError"},
	{packagePath: "os", name: "Chmod", newName: "changeModeError"},
	{packagePath: "os", name: "Getwd", newName: "workingDirectoryError"},
	{packagePath: "os", name: "UserHomeDir", newName: "homeDirectoryError"},
	{packagePath: "os", name: "Executable", newName: "executableError"},
	{packagePath: "path/filepath", name: "Abs", newName: "absolutePathError"},
	{packagePath: "path/filepath", name: "Rel", newName: "relativePathError"},
	{packagePath: "path/filepath", name: "EvalSymlinks", newName: "resolveSymlinksError"},
	{packagePath: "path/filepath", name: "Walk", newName: "walkError"},
	{packagePath: "path/filepath", name: "WalkDir", newName: "walkError"},
	{packagePath: "path/filepath", name: "Glob", newName: "globError"},
	{packagePath: "path/filepath", name: "Match", newName: "matchError"},
	{packagePath: "encoding/json", name: "Unmarshal", newName: "decodeError"},
	{packagePath: "encoding/json", receiver: "Decoder", name: "Decode", newName: "decodeError"},
	{packagePath: "encoding/json", name: "Marshal", newName: "encodeError"},
	{packagePath: "encoding/json", name: "MarshalIndent", newName: "encodeError"},
	{packagePath: "strconv", name: "Atoi", newName: "parseError"},
	{packagePath: "strconv", name: "Parse", namePrefix: true, newName: "parseError"},
	{packagePath: "os/exec", name: "LookPath", newName: "lookPathError"},
	{packagePath: "os/exec", receiver: "Cmd", name: "Run", newName: "runError"},
	{packagePath: "os/exec", receiver: "Cmd", name: "Output", newName: "outputError"},
	{packagePath: "os/exec", receiver: "Cmd", name: "CombinedOutput", newName: "outputError"},
	{packagePath: "os/exec", receiver: "Cmd", name: "Start", newName: "startError"},
	{packagePath: "os/exec", receiver: "Cmd", name: "Wait", newName: "waitError"},
	{packagePath: "regexp", name: "Compile", newName: "compileError"},
	{packagePath: "io", name: "Copy", newName: "copyError"},
	{packagePath: "fmt", name: "Sscanf", newName: "scanError"},
	{packagePath: "fmt", name: "Sscan", newName: "scanError"},
	{packagePath: "bufio", receiver: "Scanner", name: "Err", newName: "scanError"},
	{packagePath: "go/format", name: "Source", newName: "formatError"},
	{packagePath: "compress/gzip", name: "NewReader", newName: "gzipError"},
	{packagePath: "net/http", receiver: "Client", name: "Do", newName: "requestError"},
	{packagePath: "io", name: "ReadFull", newName: "readError"},
	{packagePath: "io", name: "WriteString", newName: "writeError"},
	{packagePath: "syscall", name: "Dup", newName: "duplicateError"},
	{packagePath: "syscall", name: "Mmap", newName: "memoryMapError"},
	{packagePath: "fmt", name: "Errorf"},
	{packagePath: "errors", name: "New"},
	{packagePath: "errors", name: "Join"},
}

// matches reports whether function, called on a receiver of one of receivers' types, is this entry's call.
// A method matches on either its declared receiver or the receiver expression's type, so a method promoted
// from an embedded type still matches the type it was called on.
func (entry tableEntry) matches(function *types.Func, receivers []typeName) bool {
	if function.Pkg() == nil {
		return false
	}
	if function.Name() != entry.name && !(entry.namePrefix && strings.HasPrefix(function.Name(), entry.name)) {
		return false
	}
	if entry.receiver == "" {
		return len(receivers) == 0 && function.Pkg().Path() == entry.packagePath
	}
	for _, receiver := range receivers {
		if receiver == (typeName{packagePath: entry.packagePath, name: entry.receiver}) {
			return true
		}
	}
	return false
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

// defaultErrorName is lowerCamel(functionName) with every vocabulary word expanded, plus Error unless the
// name already ends in it. It returns "" and a category when the name is left for the waves.
func defaultErrorName(functionName string, words vocabulary) (string, string) {
	if strings.Contains(functionName, "_") {
		return "", "function name holds an underscore"
	}
	for _, allowed := range words.allowedNames {
		if functionName == allowed {
			return "", "function name is an allowed name in the vocabulary"
		}
	}
	for _, allowed := range words.allowedSegments {
		if strings.Contains(functionName, allowed) {
			return "", "function name holds an allowed segment in the vocabulary"
		}
	}
	var builder strings.Builder
	lastWord := ""
	for index, word := range camelWords(functionName) {
		lowered := strings.ToLower(word)
		if words.adviceWords[lowered] {
			return "", "advice-only word in the function name"
		}
		if words.matcherWords[lowered] {
			return "", "matcher-judged word (ms) in the function name"
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
		default:
			word = houseCased(word, index)
		}
		builder.WriteString(word)
		lastWord = strings.ToLower(camelWords(word)[len(camelWords(word))-1])
	}
	name := builder.String()
	if lastWord != "error" {
		name += "Error"
	}
	if token.IsKeyword(name) || types.Universe.Lookup(name) != nil {
		return "", "name would be a keyword or predeclared identifier"
	}
	return name, ""
}

// qualifiedCallNames are producing call names too generic to name an error alone. The error folds in a
// qualifier: the package name for a package-level function, the last camel word of the receiver's type name
// for a method.
var qualifiedCallNames = map[string]bool{"New": true, "For": true, "Of": true}

// qualifierWords spells an all-lowercase package name in its camel words, so a qualifier reads as the words it
// is made of (`optionschema.For` gives optionSchemaForError). Every qualifier passes through it, so a pass
// reproduces exactly the names the tree holds; a new multi-word package that needs splitting is added here,
// never respelled by hand after a pass.
var qualifierWords = map[string]string{
	"optionschema":  "optionSchema",
	"tooldirectory": "toolDirectory",
}

// qualifiedErrorName names the error of a New, For or Of call: New gives `new` + Qualifier + `Error`
// (`prettier.New` gives newPrettierError), For and Of give qualifier + Name + `Error` (`release.For` gives
// releaseForError, `Of` on a `*Graph` gives graphOfError). A qualifier in qualifierWords takes its camel
// spelling; otherwise a vocabulary word in it is expanded.
func qualifiedErrorName(function *types.Func, words vocabulary) (string, string) {
	qualifier := ""
	if signature, isSignature := function.Type().(*types.Signature); isSignature && signature.Recv() != nil {
		receiverWords := camelWords(namedTypeOf(signature.Recv().Type()).name)
		qualifier = receiverWords[len(receiverWords)-1]
	} else if function.Pkg() != nil {
		qualifier = function.Pkg().Name()
	}
	if qualifier == "" {
		return "", "New, For or Of call without a named package or receiver type"
	}
	lowered := strings.ToLower(qualifier)
	if words.adviceWords[lowered] || words.matcherWords[lowered] {
		return "", "advice-only or matcher-judged word in the qualifier"
	}
	if camel, known := qualifierWords[lowered]; known {
		lowered = camel
	} else if expansion, abbreviated := words.expansions[lowered]; abbreviated {
		lowered = expansion
	}
	name := lowered + function.Name() + "Error"
	if function.Name() == "New" {
		name = "new" + strings.ToUpper(lowered[:1]) + lowered[1:] + "Error"
	}
	if token.IsKeyword(name) || types.Universe.Lookup(name) != nil {
		return "", "name would be a keyword or predeclared identifier"
	}
	return name, ""
}

// fixupNames are the generic names an earlier pass gave before the qualifier rule; -fixup renames them.
var fixupNames = map[string]bool{"newError": true, "forError": true, "ofError": true}

// writeSites records how the function bodies of some files write each variable.
type writeSites struct {
	count        map[*types.Var]int
	addressTaken map[*types.Var]bool
	definitions  map[*types.Var]definition
	// signature marks parameters, named results and receivers, by kind.
	signature map[*types.Var]string
	// owners is the *ast.FuncDecl or *ast.FuncLit declaring each parameter, named result and receiver; one
	// declared in a function type of its own has none.
	owners map[*types.Var]ast.Node
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
		owners:       map[*types.Var]ast.Node{},
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
	markSignature := func(fields *ast.FieldList, kind string, owner ast.Node) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				if variable, isVariable := info.Defs[name].(*types.Var); isVariable {
					sites.signature[variable] = kind
					if owner != nil {
						sites.owners[variable] = owner
					}
				}
			}
		}
	}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				markSignature(typed.Recv, "receiver", typed)
				markSignature(typed.Type.Params, "parameter", typed)
				markSignature(typed.Type.Results, "named result", typed)
			case *ast.FuncLit:
				markSignature(typed.Type.Params, "parameter", typed)
				markSignature(typed.Type.Results, "named result", typed)
			case *ast.FuncType:
				// A function type met on its own (a type declaration, a field, a parameter's type), or again
				// under the declaration or literal already marked, which keeps its owner.
				markSignature(typed.Params, "parameter", nil)
				markSignature(typed.Results, "named result", nil)
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

// namedTypeOf is the named type `resolvedType` is or points to, or the zero typeName.
func namedTypeOf(resolvedType types.Type) typeName {
	dereferenced := resolvedType
	if pointer, isPointer := types.Unalias(resolvedType).(*types.Pointer); isPointer {
		dereferenced = pointer.Elem()
	}
	if named, isNamed := types.Unalias(dereferenced).(*types.Named); isNamed && named.Obj().Pkg() != nil {
		return typeName{packagePath: named.Obj().Pkg().Path(), name: named.Obj().Name()}
	}
	return typeName{}
}

// producingFunction resolves call to the function it calls, with the receiver types a table entry may match
// (the method's declared receiver, and the type of the expression it was called on). It returns a category
// instead when the call has no declared function behind it.
func producingFunction(info *types.Info, call *ast.CallExpr) (*types.Func, []typeName, string) {
	if typeAndValue, found := info.Types[call.Fun]; found && typeAndValue.IsType() {
		return nil, nil, "right side is a conversion"
	}
	switch callee := typeutil.Callee(info, call).(type) {
	case *types.Func:
		function := callee.Origin()
		var receivers []typeName
		if signature, isSignature := function.Type().(*types.Signature); isSignature && signature.Recv() != nil {
			receivers = append(receivers, namedTypeOf(signature.Recv().Type()))
		}
		if selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSelector {
			if selection, found := info.Selections[selector]; found {
				receivers = append(receivers, namedTypeOf(selection.Recv()))
			}
		}
		return function, receivers, ""
	case *types.Builtin:
		return nil, nil, "right side calls a builtin"
	case *types.Var:
		return nil, nil, "right side calls a function value"
	}
	return nil, nil, "right side calls a function expression"
}

// qualifiedName prints function as package.Name or (Receiver).Name, for the report.
func qualifiedName(function *types.Func) string {
	if signature, isSignature := function.Type().(*types.Signature); isSignature && signature.Recv() != nil {
		receiver := namedTypeOf(signature.Recv().Type())
		if receiver.name != "" {
			return "(" + receiver.packagePath + "." + receiver.name + ")." + function.Name()
		}
		return "(" + signature.Recv().Type().String() + ")." + function.Name()
	}
	if function.Pkg() == nil {
		return function.Name()
	}
	return function.Pkg().Path() + "." + function.Name()
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

// walkCallbackErrorName names the error a walk passes its callback: the error related to the path it
// visits, as fs.WalkDirFunc and filepath.WalkFunc describe it.
const walkCallbackErrorName = "pathError"

// isErrorCandidate reports whether a local is named like an error this pass names from its producer: `err`,
// a single-letter `e` of type error, or a walk callback's error parameter named for the walk (`walkErr`,
// `walkError`), which reads as the walk's own result and is the error related to its path instead.
func isErrorCandidate(info *types.Info, sites writeSites, variable *types.Var) bool {
	errorType := types.Universe.Lookup("error").Type()
	switch variable.Name() {
	case "err":
		return true
	case "e":
		return types.Identical(variable.Type(), errorType)
	case "walkErr", "walkError":
		if sites.signature[variable] != "parameter" || !types.Identical(variable.Type(), errorType) {
			return false
		}
		newName, _, _ := parameterErrorName(info, sites.owners[variable], variable)
		return newName == walkCallbackErrorName
	}
	return false
}

// findCandidates decides, for every local named err (or an error named e) declared in files, its new name
// or why it has none, and for every other local whose name abbreviates error, its expansion. A local written
// more than once is split into one variable per web when it can be. With fixup, it decides instead for every
// local named newError, forError or ofError.
//
// finalNames, when not nil, gets the name every occurrence of a renamed or split candidate will read, by its
// position, for the parameter stage to read its callers' arguments by.
func findCandidates(
	packageTypes *types.Package,
	info *types.Info,
	fileSet *token.FileSet,
	files []*ast.File,
	words vocabulary,
	fixup bool,
	finalNames map[string]string,
) []candidate {
	sites := findWrites(info, files)
	bindings := literalBindings(info, files)
	errorType := types.Universe.Lookup("error").Type()
	analysis := &splitter{
		fileSet:      fileSet,
		packageTypes: packageTypes,
		info:         info,
		functions:    newFunctionIndex(files),
		occurrences:  map[*types.Var][]*ast.Ident{},
		words:        words,
		files:        files,
	}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			if identifier, isIdentifier := node.(*ast.Ident); isIdentifier {
				if variable, isVariable := info.ObjectOf(
					identifier,
				).(*types.Var); isVariable && variable.Pkg() == packageTypes {
					analysis.occurrences[variable] = append(analysis.occurrences[variable], identifier)
				}
			}
			return true
		})
	}
	var candidates []candidate
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier {
				return true
			}
			variable, isVariable := info.Defs[identifier].(*types.Var)
			if !isVariable || !rename.IsLocal(variable, packageTypes) {
				return true
			}
			decided := candidate{variable: variable}
			switch {
			case fixup && fixupNames[identifier.Name]:
				decided.newName, decided.category, decided.call = errorName(
					info,
					sites,
					variable,
					errorType,
					words,
					true,
				)
			case fixup:
				return true
			case isErrorCandidate(info, sites, variable):
				decided.newName, decided.category, decided.call = errorName(
					info,
					sites,
					variable,
					errorType,
					words,
					false,
				)
				var parameter *parameterHost
				if decided.category == "parameter written in its body" {
					parameter = &parameterHost{owner: sites.owners[variable]}
					parameter.newName, parameter.category, _ = parameterErrorName(info, parameter.owner, variable)
					if parameter.newName == "" {
						// No walk names it: its callers do, once every package's locals are decided.
						parameter = nil
						decided.pending = pendParameter(info, sites, bindings, analysis, variable)
					}
				}
				if sites.signature[variable] == "named result" &&
					decided.newName != "" &&
					sites.count[variable] > 0 &&
					!sites.addressTaken[variable] {
					// A result the body writes is split, so a write that only an explicit return reads is
					// named for its call; if it cannot be, the result is renamed whole.
					parameter = &parameterHost{owner: sites.owners[variable], newName: decided.newName, result: true}
					if plan, category := analysis.split(variable, parameter); category == "" && len(plan.groups) > 1 {
						decided.split, decided.category = plan, plan.residual
						decided.newName = ""
					}
					parameter = nil
				}
				if decided.category == "written more than once" || parameter != nil {
					decided.split, decided.category = analysis.split(variable, parameter)
					switch {
					case decided.split != nil && len(decided.split.groups) == 1:
						decided.newName, decided.category = decided.split.groups[0].newName, decided.split.residual
						decided.split = nil
					case decided.split != nil:
						decided.category = decided.split.residual
					}
					if decided.newName == "err" {
						decided.newName = ""
					}
				}
			default:
				newName, category, abbreviated := abbreviatedErrorName(identifier.Name, words)
				if !abbreviated {
					return true
				}
				decided.newName, decided.category = newName, category
			}
			if decided.category == "parameter" && decided.pending == nil {
				decided.pending = pendParameter(info, sites, bindings, analysis, variable)
			}
			candidates = append(candidates, decided)
			return true
		})
	}
	if finalNames != nil {
		for _, decided := range candidates {
			recordFinalNames(analysis.fileSet, analysis.occurrences[decided.variable], decided, finalNames)
		}
	}
	return candidates
}

// positionKey names a position by file and offset, the same across every variant that holds the file.
func positionKey(fileSet *token.FileSet, position token.Pos) string {
	resolved := fileSet.PositionFor(position, false)
	return fmt.Sprintf("%s:%d", resolved.Filename, resolved.Offset)
}

// recordFinalNames records the name each occurrence of a decided candidate will read.
// Mutates: finalNames every decided candidate of the pass records its occurrences into the one table
func recordFinalNames(
	fileSet *token.FileSet,
	occurrences []*ast.Ident,
	decided candidate,
	finalNames map[string]string,
) {
	switch {
	case decided.split != nil:
		for _, each := range decided.split.groups {
			for _, member := range each.webs {
				for _, definition := range member.definitions {
					finalNames[positionKey(fileSet, definition.identifier.Pos())] = each.newName
				}
				for _, use := range member.uses {
					finalNames[positionKey(fileSet, use.Pos())] = each.newName
				}
			}
		}
	case decided.newName != "":
		for _, occurrence := range occurrences {
			finalNames[positionKey(fileSet, occurrence.Pos())] = decided.newName
		}
	}
}

// literalBinding is what a function literal is bound to: a variable it is assigned to, or the Error field
// of a go/types Config.
type literalBinding struct {
	variable        *types.Var
	typeCheckErrors bool
}

// literalBindings finds every function literal assigned to one variable, and every one given as a go/types
// Config's Error callback.
func literalBindings(info *types.Info, files []*ast.File) map[*ast.FuncLit]literalBinding {
	bindings := map[*ast.FuncLit]literalBinding{}
	bind := func(names []ast.Expr, values []ast.Expr) {
		if len(names) != len(values) {
			return
		}
		for index, value := range values {
			literal, isLiteral := ast.Unparen(value).(*ast.FuncLit)
			identifier, isIdentifier := names[index].(*ast.Ident)
			if !isLiteral || !isIdentifier {
				continue
			}
			if variable, isVariable := info.ObjectOf(identifier).(*types.Var); isVariable {
				bindings[literal] = literalBinding{variable: variable}
			}
		}
	}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				bind(typed.Lhs, typed.Rhs)
			case *ast.ValueSpec:
				names := make([]ast.Expr, len(typed.Names))
				for index, name := range typed.Names {
					names[index] = name
				}
				bind(names, typed.Values)
			case *ast.CompositeLit:
				if namedTypeOf(info.TypeOf(typed)) != (typeName{packagePath: "go/types", name: "Config"}) {
					return true
				}
				for _, element := range typed.Elts {
					pair, isPair := element.(*ast.KeyValueExpr)
					if !isPair {
						continue
					}
					key, isKey := pair.Key.(*ast.Ident)
					literal, isLiteral := ast.Unparen(pair.Value).(*ast.FuncLit)
					if isKey && isLiteral && key.Name == "Error" {
						bindings[literal] = literalBinding{typeCheckErrors: true}
					}
				}
			}
			return true
		})
	}
	return bindings
}

// pendParameter records what the parameter stage needs to name an error parameter: whom its callers call,
// at which argument, the function's job, and how to settle it under a name.
func pendParameter(
	info *types.Info,
	sites writeSites,
	bindings map[*ast.FuncLit]literalBinding,
	analysis *splitter,
	variable *types.Var,
) *pendingParameter {
	owner := sites.owners[variable]
	pending := &pendingParameter{index: -1}
	var functionType *ast.FuncType
	switch typed := owner.(type) {
	case *ast.FuncDecl:
		functionType = typed.Type
		pending.function = typed.Name.Name
		pending.callee = positionKey(analysis.fileSet, typed.Name.Pos())
	case *ast.FuncLit:
		functionType = typed.Type
		binding := bindings[typed]
		pending.typeCheckErrors = binding.typeCheckErrors
		if binding.variable != nil {
			pending.function = binding.variable.Name()
			pending.callee = positionKey(analysis.fileSet, binding.variable.Pos())
		}
	default:
		return nil
	}
	index := 0
	for _, field := range functionType.Params.List {
		for _, name := range field.Names {
			if info.Defs[name] == variable {
				pending.index = index
			}
			index++
		}
		if len(field.Names) == 0 {
			index++
		}
	}
	// A literal's type is recorded on the literal; a declared function's on its object, never on its FuncType.
	signatureType := info.TypeOf(functionType)
	if declaration, isDeclaration := owner.(*ast.FuncDecl); isDeclaration && info.Defs[declaration.Name] != nil {
		signatureType = info.Defs[declaration.Name].Type()
	}
	if signature, isSignature := signatureType.(*types.Signature); isSignature {
		results := signature.Results()
		pending.tests = results.Len() == 1 && types.Identical(results.At(0).Type(), types.Typ[types.Bool])
	}
	written := sites.count[variable] > 0
	pending.resolve = func(newName string) (string, string, *splitPlan) {
		if !written {
			return newName, "", nil
		}
		plan, category := analysis.split(variable, &parameterHost{owner: owner, newName: newName})
		switch {
		case category != "":
			return "", category, nil
		case len(plan.groups) == 1:
			return plan.groups[0].newName, plan.residual, nil
		}
		return "", plan.residual, plan
	}
	return pending
}

// callSites lists, by the declaration position of what is called and then by argument index, the name each
// call's argument will read: its producer's for a call, its final name for a variable this pass names, its
// own when it already names an error, or "" when nothing names it.
// Mutates: sites every package's calls join one table, keyed by what they call, so a callee reads all its callers
func callSites(
	goPackage *packages.Package,
	finalNames map[string]string,
	words vocabulary,
	sites map[string]map[int][]string,
) {
	for _, file := range goPackage.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			callee := typeutil.Callee(goPackage.TypesInfo, call)
			if function, isFunction := callee.(*types.Func); isFunction {
				callee = function.Origin()
			}
			if callee == nil || callee.Pkg() == nil {
				return true
			}
			key := positionKey(goPackage.Fset, callee.Pos())
			if sites[key] == nil {
				sites[key] = map[int][]string{}
			}
			for index, argument := range call.Args {
				sites[key][index] = append(sites[key][index], argumentName(goPackage, argument, finalNames, words))
			}
			return true
		})
	}
}

// argumentName is the name an argument will read as an error, or "".
func argumentName(
	goPackage *packages.Package,
	argument ast.Expr,
	finalNames map[string]string,
	words vocabulary,
) string {
	switch typed := ast.Unparen(argument).(type) {
	case *ast.CallExpr:
		newName, _, _ := producerName(goPackage.TypesInfo, typed, words)
		return newName
	case *ast.Ident:
		if _, isVariable := goPackage.TypesInfo.ObjectOf(typed).(*types.Var); !isVariable {
			return ""
		}
		if newName, named := finalNames[positionKey(goPackage.Fset, typed.Pos())]; named && newName != "err" {
			return newName
		}
		if typed.Name != "err" && strings.HasSuffix(typed.Name, "Error") {
			return typed.Name
		}
	}
	return ""
}

// nameParameter applies the ruling on error parameters no walk names, and says which clause decided it: the
// one operation every caller hands in (clause 2), else candidateError for a function that tests or
// classifies the error and sourceError for one that turns it into something else or reports it (clause 3);
// a go/types Config's Error callback reports a type-checking error (clause 4).
func nameParameter(pending *pendingParameter, sites map[string]map[int][]string) (string, string) {
	if pending.typeCheckErrors {
		return "typeCheckError", "clause 4: the go/types Config Error callback"
	}
	if pending.callee != "" && pending.index >= 0 {
		names := sites[pending.callee][pending.index]
		agreed := ""
		for index, name := range names {
			if name == "" || (index > 0 && name != agreed) {
				agreed = ""
				break
			}
			agreed = name
		}
		if agreed != "" {
			return agreed, fmt.Sprintf("clause 2: all %d callers hand in %s", len(names), agreed)
		}
	}
	words := camelWords(pending.function)
	first := ""
	if len(words) > 0 {
		first = strings.ToLower(words[0])
	}
	if pending.tests || first == "is" || first == "as" || first == "classify" {
		return "candidateError", "clause 3: it tests or classifies the error"
	}
	return "sourceError", "clause 3: it turns the error into something else or reports it"
}

// errorName is one err's new name, or the category that keeps it, and the producing call when there is one.
// A parameter and a named result are named from their function. With fixup, only a New, For or Of call
// names it, by the qualifier rule.
func errorName(
	info *types.Info,
	sites writeSites,
	variable *types.Var,
	errorType types.Type,
	words vocabulary,
	fixup bool,
) (string, string, string) {
	if !types.Identical(variable.Type(), errorType) {
		return "", "type is not error", variable.Type().String()
	}
	if !fixup {
		switch sites.signature[variable] {
		case "named result":
			return resultErrorName(info, sites.owners[variable], words)
		case "parameter":
			if sites.addressTaken[variable] {
				return "", "parameter whose address is taken", ""
			}
			if sites.count[variable] > 0 {
				// Split instead: a rename would rewrite its reassignment finding's message.
				return "", "parameter written in its body", ""
			}
			return parameterErrorName(info, sites.owners[variable], variable)
		}
	}
	call, category := definingCall(sites, variable)
	if call == nil && category == "right side is not a call" && !fixup {
		if newName := receivedErrorName(sites.definitions[variable].values[0]); newName != "" {
			return newName, "", "received from a channel"
		}
	}
	if call == nil {
		return "", category, ""
	}
	if fixup {
		function, _, category := producingFunction(info, call)
		if function == nil {
			return "", category, ""
		}
		callName := qualifiedName(function)
		if !qualifiedCallNames[function.Name()] {
			return "", "fixup: call is not New, For or Of", callName
		}
		newName, category := qualifiedErrorName(function, words)
		return newName, category, callName
	}
	return producerName(info, call, words)
}

// functionWords are trailing words a function value's name may carry that say only that it is a function.
var functionWords = map[string]bool{"Function": true, "Func": true, "Fn": true, "Callback": true}

// prepositionWords are trailing words a function-making function's name may end on (`DecodeOptionsInto`),
// which say what the made function is for, not what it does.
var prepositionWords = map[string]bool{
	"Into": true,
	"For":  true,
	"Of":   true,
	"From": true,
	"To":   true,
	"With": true,
	"At":   true,
	"On":   true,
	"In":   true,
}

// producerName names the error of call: a declared function by the table, then the New, For and Of rule,
// then its own name; a function value by the value's name, less a trailing Function, Func, Fn or Callback; a
// call of a call's result by the inner call's function, less a trailing preposition, since that call made
// the function (`rule.DecodeOptionsInto[T]()(raw)` gives decodeOptionsError). It returns the call's name for
// the report, or a category when the call names nothing.
func producerName(info *types.Info, call *ast.CallExpr, words vocabulary) (string, string, string) {
	function, receivers, category := producingFunction(info, call)
	if function != nil {
		callName := qualifiedName(function)
		for _, entry := range errorTable {
			if entry.matches(function, receivers) {
				if entry.newName == "" {
					return "", "constructed error", callName
				}
				return entry.newName, "", callName
			}
		}
		if qualifiedCallNames[function.Name()] {
			newName, category := qualifiedErrorName(function, words)
			return newName, category, callName
		}
		newName, category := defaultErrorName(function.Name(), words)
		return newName, category, callName
	}
	switch category {
	case "right side calls a function value":
		value, _ := typeutil.Callee(info, call).(*types.Var)
		newName, category := defaultErrorName(withoutTrailingWords(value.Name(), functionWords), words)
		return newName, category, "function value " + value.Name()
	case "right side calls a function expression":
		switch inner := ast.Unparen(call.Fun).(type) {
		case *ast.FuncLit:
			return "", "a function literal called in place", ""
		case *ast.CallExpr:
			if maker, isNamed := typeutil.Callee(info, inner).(interface{ Name() string }); isNamed && maker != nil {
				newName, category := defaultErrorName(withoutTrailingWords(maker.Name(), prepositionWords), words)
				return newName, category, "function made by " + maker.Name()
			}
		}
	}
	return "", category, ""
}

// receivedErrorName names an error received from a channel by the channel, where its name says what it
// carries: one named for errors (`errs`, `errors`) gives receivedError, one named for results (`result`,
// `results`) gives resultError. A channel named for when (`finished`, `done`) or already named as an error
// says nothing more, and names nothing.
func receivedErrorName(value ast.Expr) string {
	receive, isReceive := ast.Unparen(value).(*ast.UnaryExpr)
	if !isReceive || receive.Op != token.ARROW {
		return ""
	}
	name := ""
	switch channel := ast.Unparen(receive.X).(type) {
	case *ast.Ident:
		name = channel.Name
	case *ast.SelectorExpr:
		name = channel.Sel.Name
	default:
		return ""
	}
	words := camelWords(name)
	switch strings.ToLower(words[len(words)-1]) {
	case "errs", "errors":
		return "receivedError"
	case "result", "results":
		return "resultError"
	}
	return ""
}

// withoutTrailingWords drops name's trailing camel words that are in trailing, keeping at least one word.
func withoutTrailingWords(name string, trailing map[string]bool) string {
	words := camelWords(name)
	for len(words) > 1 && trailing[strings.ToUpper(words[len(words)-1][:1])+words[len(words)-1][1:]] {
		words = words[:len(words)-1]
	}
	return strings.Join(words, "")
}

// resultErrorName names a named result from its function: the declared function's name in full words plus
// Error, by the same rule as a call's (`func (writer *Writer) Close() (closeError error)`). A literal's or a
// function type's result has no function name to take.
func resultErrorName(info *types.Info, owner ast.Node, words vocabulary) (string, string, string) {
	declaration, isDeclaration := owner.(*ast.FuncDecl)
	if !isDeclaration {
		return "", "named result of a function literal or type", ""
	}
	function, isFunction := info.Defs[declaration.Name].(*types.Func)
	if !isFunction {
		return "", "named result of a function literal or type", ""
	}
	if qualifiedCallNames[function.Name()] {
		newName, category := qualifiedErrorName(function, words)
		return newName, category, qualifiedName(function)
	}
	newName, category := defaultErrorName(function.Name(), words)
	return newName, category, qualifiedName(function)
}

// parameterErrorName names an error parameter. A walk callback's third parameter, a function literal taking
// a path, the path's entry or information, and the error related to that path, and returning an error, is
// pathError. Every other parameter waits on a naming rule.
func parameterErrorName(info *types.Info, owner ast.Node, variable *types.Var) (string, string, string) {
	literal, isLiteral := owner.(*ast.FuncLit)
	if !isLiteral {
		return "", "parameter", ""
	}
	signature, isSignature := info.TypeOf(literal).(*types.Signature)
	if !isSignature || !isWalkCallback(signature) || signature.Params().At(2) != variable {
		return "", "parameter", ""
	}
	return walkCallbackErrorName, "", "walk callback"
}

// isWalkCallback reports whether signature is a walk's callback: (string, fs.DirEntry or fs.FileInfo, error)
// returning error.
func isWalkCallback(signature *types.Signature) bool {
	errorType := types.Universe.Lookup("error").Type()
	if signature.Params().Len() != 3 ||
		signature.Results().Len() != 1 ||
		!types.Identical(signature.Results().At(0).Type(), errorType) {
		return false
	}
	if basic, isBasic := signature.Params().At(0).Type().(*types.Basic); !isBasic || basic.Kind() != types.String {
		return false
	}
	entry := namedTypeOf(signature.Params().At(1).Type())
	if entry != (typeName{packagePath: "io/fs", name: "DirEntry"}) &&
		entry != (typeName{packagePath: "io/fs", name: "FileInfo"}) {
		return false
	}
	return types.Identical(signature.Params().At(2).Type(), errorType)
}

// abbreviatedErrorName expands a local's name that abbreviates error: the word Err becomes Error, and a
// leading err moves to the end as Error (`errOne` gives oneError), with every other vocabulary word expanded.
// It reports false for a name with no such word. A name holding an advice-only or matcher-judged word gets a
// category instead of a name.
func abbreviatedErrorName(name string, words vocabulary) (string, string, bool) {
	parts := camelWords(name)
	if len(parts) < 2 {
		return "", "", false
	}
	found := parts[0] == "err"
	for _, word := range parts {
		found = found || word == "Err"
	}
	if !found {
		return "", "", false
	}
	if parts[0] == "err" {
		parts = append(parts[1:], "Err")
		parts[0] = strings.ToLower(parts[0][:1]) + parts[0][1:]
	}
	var builder strings.Builder
	for index, word := range parts {
		lowered := strings.ToLower(word)
		if word == "Err" {
			builder.WriteString("Error")
			continue
		}
		if words.adviceWords[lowered] || words.matcherWords[lowered] {
			return "", "advice-only or matcher-judged word beside Err", true
		}
		if expansion, abbreviated := words.expansions[lowered]; abbreviated && lowered != "err" {
			if index > 0 {
				expansion = strings.ToUpper(expansion[:1]) + expansion[1:]
			}
			builder.WriteString(expansion)
			continue
		}
		builder.WriteString(houseCased(word, index))
	}
	return builder.String(), "", true
}

// houseCased writes a name's word as the house writes it: an initialism as any other word (`CSS` after the
// first word is `Css`), and the first word lowered whole.
func houseCased(word string, index int) string {
	if index == 0 {
		return strings.ToLower(word[:1]) + word[1:]
	}
	if len(word) > 1 && strings.ToUpper(word) == word && strings.ToLower(word) != word {
		return word[:1] + strings.ToLower(word[1:])
	}
	return word
}

// otherAbbreviatedError reports whether a local error not named err still carries an abbreviated name: one
// or two letters, or a camel word in the vocabulary. They are out of this pass and counted for the waves.
func otherAbbreviatedError(variable *types.Var, words vocabulary) bool {
	if variable.Name() == "err" || variable.Name() == "_" {
		return false
	}
	if len(variable.Name()) <= 2 {
		return true
	}
	for _, word := range camelWords(variable.Name()) {
		lowered := strings.ToLower(word)
		if _, abbreviated := words.expansions[lowered]; abbreviated || words.adviceWords[lowered] {
			return true
		}
	}
	return false
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
	// split is the plan of a variable split into several.
	split *splitPlan
}

// declaredAt finds the position an engine conflict reason names as the other declaration.
var declaredAt = regexp.MustCompile(`declared at (\S+) instead of`)

// lineNumber finds a line number in a category, which the counts leave out.
var lineNumber = regexp.MustCompile(`line \d+`)

// insteadOf finds the old name of the object an engine conflict reason says the occurrence belongs to.
var insteadOf = regexp.MustCompile(`instead of "([^"]*)"$`)

func run(root string, skips []string, dryRun bool, fixup bool) int {
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
	allRetargets := map[rename.EditKey]int{}
	var allInsertions []rename.Insertion
	insertedAlready := map[rename.Insertion]bool{}
	var allUnresolved []string
	records := map[string]*declarationRecord{}
	var recordOrder []string
	otherAbbreviated := map[string]string{}
	// First every package's locals, so the parameter stage can read each caller's arguments by the names
	// they will have; then the parameters; then each package is decided and rehearsed.
	finalNames := map[string]string{}
	candidatesOf := map[*packages.Package][]candidate{}
	for _, analyzedPackage := range loaded.Analyzed {
		candidatesOf[analyzedPackage] = findCandidates(
			analyzedPackage.Types,
			analyzedPackage.TypesInfo,
			analyzedPackage.Fset,
			loaded.RenamePackage(analyzedPackage).Files,
			words,
			fixup,
			finalNames,
		)
	}
	parameterLines := nameParameters(loaded, candidatesOf, finalNames, words)
	for _, analyzedPackage := range loaded.Analyzed {
		renamePackage := loaded.RenamePackage(analyzedPackage)
		candidates := candidatesOf[analyzedPackage]
		recordKey := func(object types.Object) string {
			position := analyzedPackage.Fset.PositionFor(object.Pos(), false)
			return fmt.Sprintf("%s:%d", position.Filename, position.Offset)
		}
		isCandidate := map[*types.Var]bool{}
		for _, decided := range candidates {
			isCandidate[decided.variable] = true
			key := recordKey(decided.variable)
			if _, seen := records[key]; seen {
				continue
			}
			position := analyzedPackage.Fset.PositionFor(decided.variable.Pos(), false)
			record := &declarationRecord{
				position: relativePosition(loaded, position),
				filename: position.Filename,
				offset:   position.Offset,
				oldName:  decided.variable.Name(),
				newName:  decided.newName,
				category: decided.category,
				call:     decided.call,
			}
			if decided.split != nil {
				record.split = decided.split
				record.newName = splitNames(decided.split)
			}
			records[key] = record
			recordOrder = append(recordOrder, key)
		}
		for _, file := range renamePackage.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				identifier, isIdentifier := node.(*ast.Ident)
				if !isIdentifier {
					return true
				}
				variable, isVariable := analyzedPackage.TypesInfo.Defs[identifier].(*types.Var)
				if isVariable &&
					!isCandidate[variable] &&
					rename.IsLocal(variable, analyzedPackage.Types) &&
					types.Identical(variable.Type(), types.Universe.Lookup("error").Type()) &&
					otherAbbreviatedError(variable, words) {
					otherAbbreviated[recordKey(variable)] = variable.Name()
				}
				return true
			})
		}
		decided := decidePackage(analyzedPackage, renamePackage, candidates)
		if decided.checkError != nil {
			fmt.Fprintf(streams.Errors, "%s: %v (left to the final check)\n", commandName, decided.checkError)
		}
		for _, unresolved := range decided.plan.Unresolved {
			allUnresolved = append(allUnresolved, analyzedPackage.ID+": "+unresolved)
		}
		for _, rejection := range decided.plan.Rejections {
			record, found := records[recordKey(rejection.Object)]
			if !found || record.rejection != "" {
				continue
			}
			record.rejection = reasonClass(rejection.Reason, rejection.NewName, decided.proposedAt, decided.oldNames)
			record.reason = strings.ReplaceAll(rejection.Reason, loaded.Root+"/", "")
		}
		for variable, reason := range decided.dropped {
			if record := records[recordKey(variable)]; record.rejection == "" {
				record.rejection = "refused by the package check"
				record.reason = strings.ReplaceAll(reason, loaded.Root+"/", "")
			}
		}
		allEdits = append(allEdits, decided.edits...)
		for key, declaration := range decided.retargets {
			allRetargets[key] = declaration
		}
		for _, insertion := range decided.insertions {
			// A file two variants hold is decided twice, the same way.
			if !insertedAlready[insertion] {
				insertedAlready[insertion] = true
				allInsertions = append(allInsertions, insertion)
			}
		}
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
			notEligibleByCategory[lineNumber.ReplaceAllString(record.category, "line N")]++
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
	if fixup {
		fmt.Fprintf(streams.Output, "candidates (locals named newError, forError or ofError): %d\n", candidateCount)
	} else {
		fmt.Fprintf(
			streams.Output,
			"candidates (locals named err, errors named e, and locals abbreviating Err): %d\n",
			candidateCount,
		)
	}
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
	otherNames := map[string]int{}
	for _, name := range otherAbbreviated {
		otherNames[name]++
	}
	fmt.Fprintf(
		streams.Output,
		"other error locals with an abbreviated name, out of this pass: %d\n",
		len(otherAbbreviated),
	)
	printCounts(otherNames, 15)

	fmt.Fprintf(streams.Output, "parameters named by the ruling on error parameters: %d\n", len(parameterLines))
	for _, line := range parameterLines {
		fmt.Fprintln(streams.Output, "  "+line)
	}

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
		record := records[key]
		if record.newName == "" || record.rejection != "" {
			continue
		}
		if record.split == nil {
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
			continue
		}
		fmt.Fprintf(streams.Output, "  %s: %s split -> %s\n", record.position, record.oldName, record.newName)
		if record.split.residual != "" {
			fmt.Fprintf(streams.Output, "      %s\n", record.split.residual)
		}
		for _, each := range record.split.groups {
			anchor := record.offset
			if !each.host {
				anchor = each.anchorOffset
			}
			before, after := lineBeforeAndAfter(record.filename, anchor, editsByFile[record.filename])
			fmt.Fprintf(streams.Output, "      %s\n    | %s\n", before, after)
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
	if fixup {
		if dryRun {
			return 0
		}
		return workspace.ApplyVerifiedRetargeted(
			loaded,
			root,
			skips,
			edits,
			editsByFile,
			allRetargets,
			allInsertions,
			commandName,
			streams.Output,
			streams.Errors,
		)
	}
	if dryRun {
		planGlobals(loaded, words)
		return 0
	}
	if code := workspace.ApplyVerifiedRetargeted(
		loaded,
		root,
		skips,
		edits,
		editsByFile,
		allRetargets,
		allInsertions,
		commandName,
		streams.Output,
		streams.Errors,
	); code != 0 {
		return code
	}
	return applyGlobals(root, skips, words)
}

// nameParameters names every pending parameter by the ruling, from the call sites of every analyzed
// package, and returns a line per parameter saying the name it got and the clause that decided it.
func nameParameters(
	loaded *workspace.Workspace,
	candidatesOf map[*packages.Package][]candidate,
	finalNames map[string]string,
	words vocabulary,
) []string {
	sites := map[string]map[int][]string{}
	for _, analyzedPackage := range loaded.Analyzed {
		callSites(analyzedPackage, finalNames, words, sites)
	}
	var lines []string
	for _, analyzedPackage := range loaded.Analyzed {
		candidates := candidatesOf[analyzedPackage]
		for index := range candidates {
			pending := candidates[index].pending
			if pending == nil {
				continue
			}
			name, clause := nameParameter(pending, sites)
			decided := &candidates[index]
			decided.newName, decided.category, decided.split = pending.resolve(name)
			decided.call = clause
			if decided.split != nil {
				decided.category = decided.split.residual
			}
			function := pending.function
			if function == "" {
				function = "a function literal"
			}
			lines = append(lines, fmt.Sprintf(
				"%s: err in %s -> %s (%s)",
				relativePosition(loaded, analyzedPackage.Fset.PositionFor(decided.variable.Pos(), false)),
				function,
				name,
				clause,
			))
		}
	}
	sort.Strings(lines)
	return lines
}

// splitNames lists a split's names for the report, the original's first.
func splitNames(plan *splitPlan) string {
	names := make([]string, 0, len(plan.groups))
	for _, each := range plan.groups {
		names = append(names, each.newName)
	}
	return strings.Join(names, "+")
}

// packageDecision is what a package's candidates come to after the rename engine and the package check.
type packageDecision struct {
	plan      rename.Plan
	edits     []rename.Edit
	retargets map[rename.EditKey]int
	// insertions are the declarations the package's splits add.
	insertions []rename.Insertion
	// dropped is each candidate the package check refused, with the first reason it gave.
	dropped    map[*types.Var]string
	proposedAt map[string]string
	oldNames   map[string]bool
	// checkError says why the package could not be checked here, leaving it to the final reload.
	checkError error
}

// decidePackage sends the candidates' renames through the rename engine, adds the splits' edits, and runs
// checkPackage over the result. Each line it finds wrong drops the decisions of the top-level declaration
// holding it (the splits first, since a split's names are the ones the engine never saw, then the renames),
// and the package is decided again, until the check passes.
func decidePackage(goPackage *packages.Package, renamePackage rename.Package, candidates []candidate) packageDecision {
	decision := packageDecision{dropped: map[*types.Var]string{}, oldNames: map[string]bool{}}
	declarationOf := map[*types.Var]ast.Decl{}
	for _, decided := range candidates {
		decision.oldNames[decided.variable.Name()] = true
		for _, file := range renamePackage.Files {
			if file.Pos() > decided.variable.Pos() || decided.variable.Pos() >= file.End() {
				continue
			}
			for _, declaration := range file.Decls {
				if declaration.Pos() <= decided.variable.Pos() && decided.variable.Pos() < declaration.End() {
					declarationOf[decided.variable] = declaration
				}
			}
		}
	}
	for {
		var proposals []rename.Proposal
		decision.proposedAt = map[string]string{}
		for _, decided := range candidates {
			if _, isDropped := decision.dropped[decided.variable]; isDropped ||
				decided.newName == "" ||
				decided.split != nil {
				continue
			}
			proposals = append(proposals, rename.Proposal{Object: decided.variable, NewName: decided.newName})
			decision.proposedAt[goPackage.Fset.Position(decided.variable.Pos()).String()] = decided.newName
		}
		decision.plan = rename.Check(renamePackage, proposals)
		decision.edits = rename.Edits(renamePackage, decision.plan.Accepted)
		decision.retargets = map[rename.EditKey]int{}
		decision.insertions = nil
		var live []candidate
		for _, decided := range candidates {
			if _, isDropped := decision.dropped[decided.variable]; isDropped {
				continue
			}
			if decided.split != nil {
				decision.edits = append(decision.edits, decided.split.edits...)
				for key, declaration := range decided.split.retargets {
					decision.retargets[key] = declaration
				}
				decision.insertions = append(decision.insertions, decided.split.insertions...)
				live = append(live, decided)
			} else if _, accepted := decision.plan.Accepted[decided.variable]; accepted {
				live = append(live, decided)
			}
		}
		if len(decision.edits) == 0 {
			return decision
		}
		problems, checkError := checkPackage(goPackage, decision.edits, decision.retargets, decision.insertions)
		if checkError != nil {
			decision.checkError = checkError
			return decision
		}
		if len(problems) == 0 {
			return decision
		}
		droppedAny := false
		for _, problem := range problems {
			var splits []candidate
			var renames []candidate
			for _, decided := range live {
				if _, isDropped := decision.dropped[decided.variable]; isDropped {
					continue
				}
				declaration := declarationOf[decided.variable]
				if declaration == nil ||
					goPackage.Fset.PositionFor(declaration.Pos(), false).Filename != problem.filename {
					continue
				}
				start, end := goPackage.Fset.PositionFor(
					declaration.Pos(),
					false,
				).Line, goPackage.Fset.PositionFor(declaration.End(), false).Line
				if problem.line != 0 && (problem.line < start || problem.line > end) {
					continue
				}
				if decided.split != nil {
					splits = append(splits, decided)
				} else {
					renames = append(renames, decided)
				}
			}
			chosen := splits
			if len(chosen) == 0 {
				chosen = renames
			}
			for _, decided := range chosen {
				decision.dropped[decided.variable] = fmt.Sprintf(
					"%s:%d: %s",
					problem.filename,
					problem.line,
					problem.reason,
				)
				droppedAny = true
			}
		}
		if !droppedAny {
			// No declaration holds the trouble: drop every live decision of the files it names.
			for _, problem := range problems {
				for _, decided := range live {
					if goPackage.Fset.PositionFor(decided.variable.Pos(), false).Filename == problem.filename {
						decision.dropped[decided.variable] = fmt.Sprintf(
							"%s:%d: %s",
							problem.filename,
							problem.line,
							problem.reason,
						)
						droppedAny = true
					}
				}
			}
		}
		if !droppedAny {
			for _, decided := range live {
				decision.dropped[decided.variable] = "the package check failed outside every decision: " +
					problems[0].reason
			}
		}
	}
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
