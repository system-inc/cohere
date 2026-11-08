// Command generate_symbols writes std_version's table of the standard library's exported symbols, each with the minor
// version of Go that introduced it, from the toolchain's own record of its API, `$GOROOT/api/go1.*.txt`.
//
// Rerun it from the root of the cohere checkout whenever go.mod's go or toolchain line moves to a new minor version
// (TestTheCommittedTableIsTheToolchainsTable beside this file goes red until it is):
//
//	go run ./internal/go_engine/rules/vet/std_version/generate_symbols
//
// (an agent runs that line through `go run ./command/cohere-dev exec -- `). It writes
// internal/go_engine/rules/vet/std_version/symbols_generated.go whole.
//
// It is translated from golang.org/x/tools@v0.50.0 internal/stdlib/generate.go, whose manifest vet's stdversion pass
// reads and cohere cannot import, and reads the API files as that generator does: a symbol's version is the first
// file that names it, an interface's methods and a struct's fields are symbols of their own, an embedded field is
// named for its type, and an interface's unexported or deprecated methods are left out. It differs in three ways,
// none of which changes what the pass reports:
//   - It reads go1.txt through the minor version go.mod pins, and no further, rather than every file the toolchain
//     ships and then api/next. The table is then the same under any toolchain at or past the pin, and it stops where
//     the manifest of the x/tools cohere links stops: v0.50.0's names nothing past go1.27, the version go.mod pins.
//   - It leaves out every symbol of go1.0, and syscall/js and unsafe, which upstream computes by type-checking them and
//     marks go1.0. No version of Go is before go1.0, so a go1.0 symbol is never too new, and the pass never reads one.
//   - It writes no function signatures and no kinds: the pass reads neither, and a symbol's kind is in its name (a
//     method's starts with a parenthesis, a field's has a dot, a package-level symbol's has neither).
package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"go/version"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

// tablePath is where the table is written, relative to the module's root.
const tablePath = "internal/go_engine/rules/vet/std_version/symbols_generated.go"

func main() {
	if generateError := generate(); generateError != nil {
		// Ignored: the exit code below says the run failed even when standard error cannot.
		fmt.Fprintf(os.Stderr, "generate_symbols: %v\n", generateError)
		os.Exit(1)
	}
}

// generate writes the table for the module the working directory is in.
func generate() error {
	root, rootError := moduleRoot()
	if rootError != nil {
		return rootError
	}
	table, tableError := tableFor(root)
	if tableError != nil {
		return tableError
	}
	return os.WriteFile(filepath.Join(root, tablePath), table, 0o644)
}

// moduleRoot is the directory of the go.mod of the module the working directory is in.
func moduleRoot() (string, error) {
	goModulePath, goModuleError := goEnvironment("GOMOD")
	if goModuleError != nil {
		return "", goModuleError
	}
	if goModulePath == "" || goModulePath == os.DevNull {
		return "", errors.New("run this from inside the cohere checkout, where go env GOMOD names its go.mod")
	}
	return filepath.Dir(goModulePath), nil
}

// tableFor is the table for the module at moduleRoot: the API of the toolchain go env names, read through the minor
// version the module's go.mod pins.
func tableFor(moduleRoot string) ([]byte, error) {
	goModulePath := filepath.Join(moduleRoot, "go.mod")
	goModule, readError := os.ReadFile(goModulePath)
	if readError != nil {
		return nil, readError
	}
	pinned, pinnedError := pinnedMinor(goModulePath, goModule)
	if pinnedError != nil {
		return nil, pinnedError
	}
	root, rootError := goEnvironment("GOROOT")
	if rootError != nil {
		return nil, rootError
	}
	return generatedTable(filepath.Join(root, "api"), pinned)
}

// goEnvironment is what go env prints for one variable.
func goEnvironment(name string) (string, error) {
	output, outputError := exec.Command("go", "env", name).Output()
	if outputError != nil {
		return "", fmt.Errorf("go env %s: %w", name, outputError)
	}
	return strings.TrimSpace(string(output)), nil
}

// pinnedMinor is the minor version of Go a go.mod pins: its toolchain line's when it has one, since that names the
// toolchain the module builds with, and its go line's otherwise.
func pinnedMinor(path string, content []byte) (int, error) {
	parsed, parseError := modfile.Parse(path, content, nil)
	if parseError != nil {
		return 0, parseError
	}
	pinned := ""
	if parsed.Go != nil {
		pinned = "go" + parsed.Go.Version
	}
	if parsed.Toolchain != nil {
		pinned = parsed.Toolchain.Name
	}
	language := version.Lang(pinned)
	minor, found := strings.CutPrefix(language, "go1.")
	if !found {
		return 0, fmt.Errorf("%s pins %q, which is no Go 1 language version", path, pinned)
	}
	return strconv.Atoi(minor)
}

// symbolLine is one line of an API file: its package's path, what it declares (var, func, type, const, or a method
// with its receiver), the name it declares, and the rest of the line. x/tools' generate.go's symRE.
var symbolLine = regexp.MustCompile(`^pkg (\S+).*?, (var|func|type|const|method \([^)]*\)) ([\pL\p{Nd}_]+)(.*)`)

// generatedTable is the table's Go source for the API files in apiDirectory, go1.txt through go1.<pinned>.txt.
func generatedTable(apiDirectory string, pinned int) ([]byte, error) {
	introduced := introducedSymbols{}
	for minor := 0; minor <= pinned; minor++ {
		base := "go1.txt"
		if minor > 0 {
			base = fmt.Sprintf("go1.%d.txt", minor)
		}
		fileName := filepath.Join(apiDirectory, base)
		content, readError := os.ReadFile(fileName)
		if errors.Is(readError, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s is missing: the toolchain is older than go1.%d, which go.mod pins", fileName,
				pinned)
		}
		if readError != nil {
			return nil, readError
		}
		if parseError := introduced.addApiFile(fileName, content, minor); parseError != nil {
			return nil, parseError
		}
	}
	// upstream computes these two packages' symbols by type-checking them, since the API files leave them out, and
	// marks every one go1.0, so the pass never reports one.
	delete(introduced, "syscall/js")
	delete(introduced, "unsafe")

	var source bytes.Buffer
	source.WriteString("// Code generated by generate_symbols from GOROOT/api, go1.txt through " +
		fmt.Sprintf("go1.%d.txt. DO NOT EDIT.\n\n", pinned) +
		"package std_version\n\n" +
		"// standardSymbols is each standard-library package's exported symbols introduced after go1.0, sorted\n" +
		"// by the name x/tools' internal/stdlib manifest gives each, with the minor version that introduced it.\n" +
		"var standardSymbols = map[string][]symbol{\n")
	for _, path := range slices.Sorted(maps.Keys(introduced)) {
		names := slices.Sorted(maps.Keys(introduced[path]))
		// The symbols of go1.0 are left out, and a package with no later symbol has no entry.
		kept := slices.DeleteFunc(names, func(name string) bool { return introduced[path][name] == 0 })
		if len(kept) == 0 {
			continue
		}
		fmt.Fprintf(&source, "%q: {\n", path)
		for _, name := range kept {
			fmt.Fprintf(&source, "{%q, %d},\n", name, introduced[path][name])
		}
		source.WriteString("},\n")
	}
	source.WriteString("}\n")
	return format.Source(source.Bytes())
}

// introducedSymbols is each package's symbols, by the name the pass looks them up by, and the minor version of the
// first API file that names each one.
type introducedSymbols map[string]map[string]int

// addApiFile adds the symbols of one API file, the one for minor, keeping the version of a symbol an earlier file
// named. x/tools' generate.go's parse.
func (introduced introducedSymbols) addApiFile(fileName string, content []byte, minor int) error {
	for index, line := range strings.Split(string(content), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := symbolLine.FindStringSubmatch(line)
		if parts == nil {
			return fmt.Errorf("%s:%d: a line no API file holds: %s", fileName, index+1, line)
		}
		path, kind, name, rest := parts[1], parts[2], parts[3], parts[4]
		symbolName, isSymbol := symbolNameOf(kind, name, rest)
		if !isSymbol {
			continue
		}
		symbols, known := introduced[path]
		if !known {
			symbols = map[string]int{}
			introduced[path] = symbols
		}
		// A later file names a symbol again when an enum's encoding changes or the symbol is deprecated, and the
		// version that counts is the first.
		if _, named := symbols[symbolName]; !named {
			symbols[symbolName] = minor
		}
	}
	return nil
}

// symbolNameOf is the name an API line's symbol has in the manifest: `(*T).m` or `(T).m` for a method, a concrete
// type's or an interface's, `T.f` for a struct's field, and the bare name for a package-level symbol. It is false for
// an interface's unexported or deprecated methods, which are no symbol. x/tools' generate.go's parse, its middle.
func symbolNameOf(kind string, name string, rest string) (string, bool) {
	if receiver, isMethod := strings.CutPrefix(kind, "method "); isMethod {
		// "method (*Func) Pos() token.Pos": the receiver, without its type parameters.
		return withoutTypeParameters(receiver) + "." + name, true
	}
	if method, isInterfaceMethod := strings.CutPrefix(rest, " interface, "); isInterfaceMethod && kind == "type" {
		// "type Type interface, Comparable() bool", or "type Error interface, Temporary //deprecated".
		if strings.HasPrefix(method, "unexported methods") || strings.Contains(method, " //deprecated") {
			return "", false
		}
		methodName, _, hasParameters := strings.Cut(method, "(")
		if !hasParameters {
			// Upstream logs such a line and leaves it out; no API file holds one.
			return "", false
		}
		return "(" + name + ")." + methodName, true
	}
	if field, isField := strings.CutPrefix(rest, " struct, "); isField && kind == "type" {
		// "type ParenExpr struct, Lparen token.Pos".
		fieldName, fieldType, _ := strings.Cut(field, " ")
		// The API files name an anonymous field "embedded", so it is named for its type: "*pkg.T[P]" is "T".
		if fieldName == "embedded" {
			unqualified := strings.TrimPrefix(fieldType, "*")
			if _, afterPackage, qualified := strings.Cut(unqualified, "."); qualified {
				unqualified = afterPackage
			}
			fieldName = withoutTypeParameters(unqualified)
		}
		return name + "." + fieldName, true
	}
	return name, true
}

// withoutTypeParameters is a type's name without its brackets: `(*Foo[T])` is `(*Foo)`, `Foo[T]` is `Foo`. x/tools'
// generate.go's removeTypeParam.
func withoutTypeParameters(typeName string) string {
	opening := strings.IndexByte(typeName, '[')
	closing := strings.LastIndexByte(typeName, ']')
	if opening > 0 && closing > opening {
		return typeName[:opening] + typeName[closing+len("]"):]
	}
	return typeName
}
