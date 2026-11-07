package go_origin_names

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/types/typeutil"
)

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
	{packagePath: "os/exec", receiver: "Cmd", name: "CombinedOutput", newName: "combinedOutputError"},
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

// defaultErrorName is lowerCamel(functionName) with every vocabulary word expanded, plus Error unless the
// name already ends in it. It returns "" and a category when the name is left for the waves.
func defaultErrorName(functionName string, words Vocabulary) (string, string) {
	if strings.Contains(functionName, "_") {
		return "", "function name holds an underscore"
	}
	if words.AllowedNames[functionName] {
		return "", "function name is an allowed name in the vocabulary"
	}
	for _, allowed := range words.AllowedSegments {
		if strings.Contains(functionName, allowed) {
			return "", "function name holds an allowed segment in the vocabulary"
		}
	}
	var builder strings.Builder
	lastWord := ""
	for index, word := range CamelWords(functionName) {
		lowered := strings.ToLower(word)
		if words.AdviceWords[lowered] {
			return "", "advice-only word in the function name"
		}
		if words.MatcherWords[lowered] {
			return "", "matcher-judged word (ms) in the function name"
		}
		expansion, abbreviated := words.Expansions[lowered]
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
			word = HouseCased(word, index)
		}
		builder.WriteString(word)
		lastWord = strings.ToLower(CamelWords(word)[len(CamelWords(word))-1])
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

// IsQualifiedCall reports whether function is a New, For or Of, whose error QualifiedErrorName names.
func IsQualifiedCall(function *types.Func) bool {
	return qualifiedCallNames[function.Name()]
}

// QualifiedErrorName names the error of a New, For or Of call: New gives `new` + Qualifier + `Error`
// (`prettier.New` gives newPrettierError), For and Of give qualifier + Name + `Error` (`release.For` gives
// releaseForError, `Of` on a `*Graph` gives graphOfError). A qualifier QualifierSpelling knows takes its camel
// spelling; otherwise a vocabulary word in it is expanded.
func QualifiedErrorName(function *types.Func, words Vocabulary) (string, string) {
	qualifier := ""
	if signature, isSignature := function.Type().(*types.Signature); isSignature && signature.Recv() != nil {
		receiverWords := CamelWords(NamedTypeOf(signature.Recv().Type()).Name)
		qualifier = receiverWords[len(receiverWords)-1]
	} else if function.Pkg() != nil {
		qualifier = function.Pkg().Name()
	}
	if qualifier == "" {
		return "", "New, For or Of call without a named package or receiver type"
	}
	lowered := strings.ToLower(qualifier)
	if words.AdviceWords[lowered] || words.MatcherWords[lowered] {
		return "", "advice-only or matcher-judged word in the qualifier"
	}
	if camel, known := QualifierSpelling(lowered); known {
		lowered = camel
	} else if expansion, abbreviated := words.Expansions[lowered]; abbreviated {
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

// FunctionErrorName names the error a declared function produces by the function alone: a New, For or Of by
// its qualifier, any other by its own name in full words plus Error. It returns "" and a category when the name
// is left for the waves.
func FunctionErrorName(function *types.Func, words Vocabulary) (string, string) {
	if IsQualifiedCall(function) {
		return QualifiedErrorName(function, words)
	}
	return defaultErrorName(function.Name(), words)
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

// CallErrorName names the error of call: a declared function by the table, then the New, For and Of rule,
// then its own name; a function value by the value's name, less a trailing Function, Func, Fn or Callback; a
// call of a call's result by the inner call's function, less a trailing preposition, since that call made
// the function (`rule.DecodeOptionsInto[T]()(raw)` gives decodeOptionsError). It returns the call's name for
// a report, or a category when the call names nothing.
func CallErrorName(info *types.Info, call *ast.CallExpr, words Vocabulary) (string, string, string) {
	function, receivers, category := ProducingFunction(info, call)
	if function != nil {
		callName := QualifiedName(function)
		for _, entry := range errorTable {
			if entry.matches(function, receivers) {
				if entry.newName == "" {
					return "", "constructed error", callName
				}
				return entry.newName, "", callName
			}
		}
		newName, category := FunctionErrorName(function, words)
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
