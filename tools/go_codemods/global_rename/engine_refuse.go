package global_rename

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/system-inc/cohere/tools/go_codemods/rename"

	"golang.org/x/tools/go/packages"
)

// refuseUpfront applies every refusal that depends on one candidate alone. Locals need only the first few:
// nothing outside their function can name them.
func (engine *engine) refuseUpfront() {
	outside := engine.scanOutside()
	literals := engine.scanStringLiterals()
	directed := engine.scanDirectives()
	unkeyed := engine.scanUnkeyedReferences()
	engine.encoded = engine.analyzeEncoding()
	engine.sourceReadingTests = engine.scanSourceReadingTests()

	for _, key := range engine.sortedCandidateKeys() {
		candidate := engine.candidates[key]
		if _, open := engine.accepted[key]; !open {
			continue
		}
		newName := candidate.NewName
		switch {
		case !token.IsIdentifier(newName):
			engine.reject(key, "name", "new name is not a valid identifier")
			continue
		case token.IsKeyword(newName):
			engine.reject(key, "name", "new name is a keyword")
			continue
		case newName == "_" || candidate.OldName == "_":
			engine.reject(key, "name", "blank name")
			continue
		case newName == candidate.OldName:
			engine.reject(key, "name", "new name is the old name")
			continue
		}
		switch candidate.object.(type) {
		case *types.PkgName, *types.Builtin, *types.Nil:
			engine.reject(key, "kind", "not a renamable declaration")
			continue
		}
		if candidate.local {
			continue
		}
		if !engine.editableFiles[candidate.Position.Filename] {
			engine.reject(
				key,
				"file",
				"declared in a file that may not be edited (generated, skipped or outside the root)",
			)
			continue
		}
		if file := engine.uneditableSite(key); file != "" {
			engine.reject(key, "file", "used in a file that may not be edited: "+engine.relative(file))
			continue
		}
		if !engine.packageVisible(candidate) {
			// A constant or type declared in a function is seen by nothing outside it, so its capital exports
			// nothing and may come or go: LS reads lineSeparator.
			continue
		}
		if token.IsExported(newName) != token.IsExported(candidate.OldName) {
			engine.reject(key, "name", "the new name changes whether the name is exported")
			continue
		}
		if reason := engine.exportRefusal(candidate, outside); reason != "" {
			engine.reject(key, "exported outside the workspace", reason)
			continue
		}
		if reason := engine.outsideRefusal(candidate, outside); reason != "" {
			engine.reject(key, "named in a file outside the load", reason)
			continue
		}
		if reason := engine.platformRefusal(candidate); reason != "" {
			engine.reject(key, "named in a file only another platform builds", reason)
			continue
		}
		if example := unkeyed[candidate.OldName]; example != "" {
			engine.reject(
				key,
				"model: unkeyed reference",
				"a root package reaches a declaration of this name through a variant without source: "+example,
			)
			continue
		}
		wholeSites := engine.nameReadingSites(candidate, literals.whole[candidate.OldName])
		if where := engine.unreviewedLiteral(wholeSites); where != "" {
			engine.reject(
				key,
				"string literal",
				fmt.Sprintf("appears as the string literal %q at %s", candidate.OldName, where),
			)
			continue
		}
		partSites := engine.nameReadingSites(candidate, literals.parts[candidate.OldName])
		if where := engine.unreviewedLiteral(partSites); where != "" {
			engine.reject(
				key,
				"string literal",
				fmt.Sprintf("appears as a dotted part of a string literal at %s", where),
			)
			continue
		}
		if isTestRunnerFunction(candidate) {
			if where := engine.unreviewedLiteral(literals.testNames[candidate.OldName]); where != "" {
				engine.reject(
					key,
					"string literal",
					fmt.Sprintf("a test run by name: it appears as a word of the string literal at %s", where),
				)
				continue
			}
		}
		if reason := directed[key]; reason != "" {
			engine.reject(key, "directive", reason)
			continue
		}
		if reason := directed[Key{
			PackagePath: strings.TrimSuffix(key.PackagePath, "_test"),
			Name:        "example " + candidate.OldName,
		}]; reason != "" {
			engine.reject(key, "directive", reason)
			continue
		}
	}
	engine.refuseForeignInterfaces()
	engine.refuseEncoded()
}

func (engine *engine) sortedCandidateKeys() []Key {
	keys := make([]Key, 0, len(engine.candidates))
	for key := range engine.candidates {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		return positionLess(engine.candidates[keys[left]].Position, engine.candidates[keys[right]].Position)
	})
	return keys
}

// uneditableSite is a file holding an identifier of the candidate that may not be edited, or "".
func (engine *engine) uneditableSite(key Key) string {
	for _, site := range engine.sites[key] {
		if !site.editable {
			return site.filename
		}
	}
	return ""
}

func (engine *engine) relative(filename string) string {
	if relative, inside := engine.workspace.Relative(filename); inside {
		return relative
	}
	return filename
}

// packageVisible reports whether code outside the declaring function can name the candidate: a package-level
// declaration, a field or a method. A local type or constant, a type parameter and a label cannot.
func (engine *engine) packageVisible(candidate *candidate) bool {
	switch candidate.Kind {
	case KindField, KindEmbeddedField, KindMethod:
		return true
	case KindLabel, KindTypeParameter, KindLocal:
		return false
	}
	return candidate.object.Parent() != nil && candidate.object.Parent() == candidate.object.Pkg().Scope()
}

// outsideRefusal is why a file the load does not type-check keeps a candidate's name, or "". A file another
// platform builds and type-checks is answered by its types in platformRefusal instead, and is left out here; what
// follows is for the rest, read as text. An exported field or method can be reached from anywhere a value of its
// type goes, so any file spelling the name keeps it; an unexported one is selected only inside its own package,
// so only a file in its own directory can (the rule of handwork-fields 243492f). A package-level
// function, variable, constant or type is seen only by its own package and, exported, by a file that imports it,
// so any other file spelling the name spells something else. A file of its own package, a build-constrained
// platform twin such as a _windows.go or a //go:build !(darwin || linux) file, whose every spelling of the name
// is a top-level declaration of the same kind, has those declarations and their doc comments renamed with it,
// provided no file of the package outside the load spells the new name.
func (engine *engine) outsideRefusal(candidate *candidate, outside *outsideFiles) string {
	var files []string
	for _, relative := range outside.names[candidate.OldName] {
		if !engine.platforms.resolves(relative) {
			files = append(files, relative)
		}
	}
	if len(files) == 0 {
		return ""
	}
	refusal := func(relative string) string {
		return fmt.Sprintf(
			"the name appears in %s, which the load does not type-check (build-constrained or outside the packages)",
			relative,
		)
	}
	directory := filepath.Dir(candidate.Position.Filename)
	switch candidate.Kind {
	case KindFunc, KindVariable, KindConst, KindType:
	default:
		if token.IsExported(candidate.OldName) {
			return refusal(files[0])
		}
		for _, relative := range files {
			// A file not read for its package (seen only through its imports) is kept to, as before.
			if file := outside.files[relative]; file == nil || file.directory == directory {
				return refusal(relative)
			}
		}
		return ""
	}
	packageName := candidate.object.Pkg().Name()
	samePackage := func(file *outsideFile) bool {
		return file.directory == directory && file.packageName == packageName
	}
	switch candidate.Kind {
	case KindFunc, KindVariable, KindConst, KindType:
	case KindField, KindMethod:
		// An unexported field or method is selected only inside its own package, so a file of another package
		// spelling the name spells something else: a planted rule's `ctx rule.Context` parameter is not a
		// walker's ctx field.
		if token.IsExported(candidate.OldName) {
			return refusal(files[0])
		}
		for _, relative := range files {
			if file := outside.files[relative]; file == nil || samePackage(file) {
				return refusal(relative)
			}
		}
		return ""
	default:
		return refusal(files[0])
	}
	for _, file := range outside.files {
		if samePackage(file) && file.spellings[candidate.NewName] > 0 {
			return fmt.Sprintf(
				"the new name appears in %s, a file of its own package that the load does not type-check",
				engine.relative(file.filename),
			)
		}
	}
	var twinEdits []rename.Edit
	for _, relative := range files {
		file := outside.files[relative]
		if file == nil {
			return refusal(relative)
		}
		if !samePackage(file) {
			if token.IsExported(candidate.OldName) && file.imports[candidate.Key.PackagePath] {
				return refusal(relative)
			}
			continue
		}
		declarations := file.declarations[candidate.OldName]
		if len(declarations) != file.spellings[candidate.OldName] {
			return fmt.Sprintf(
				"the name appears in %s, a file of its own package that the load does not type-check, other than as a declaration there",
				relative,
			)
		}
		for _, declaration := range declarations {
			if declaration.kind != candidate.Kind {
				return fmt.Sprintf("%s declares the name as a %s, not a %s", relative, declaration.kind, candidate.Kind)
			}
			twinEdits = append(twinEdits, rename.Edit{
				Filename: file.filename,
				Offset:   declaration.offset,
				OldText:  candidate.OldName,
				NewText:  candidate.NewName,
			})
			if declaration.doc == nil {
				continue
			}
			for _, comment := range declaration.doc.List {
				offset := outside.fileSet.PositionFor(comment.Pos(), false).Offset
				for _, index := range wholeWordIndexes(comment.Text, candidate.OldName) {
					twinEdits = append(twinEdits, rename.Edit{
						Filename: file.filename,
						Offset:   offset + index,
						OldText:  candidate.OldName,
						NewText:  candidate.NewName,
					})
				}
			}
		}
	}
	if len(twinEdits) > 0 {
		engine.twinEdits[candidate.Key] = twinEdits
	}
	return ""
}

// exportRefusal refuses an exported name that code the workspace does not hold may read: one in a package a
// file outside the load imports, or one in a library package outside internal/, which anything may import, unless
// Options.PinnedLibraries names it. A main package, an external test package and a declaration in a _test.go file
// are importable by nobody.
func (engine *engine) exportRefusal(candidate *candidate, outside *outsideFiles) string {
	if !token.IsExported(candidate.OldName) {
		return ""
	}
	path := candidate.Key.PackagePath
	if importer := outside.seenFrom[path]; importer != "" {
		return fmt.Sprintf(
			"package %s is imported, directly or not, by %s, which the load does not hold",
			path,
			importer,
		)
	}
	if candidate.goPackage.Name == "main" ||
		strings.HasSuffix(candidate.goPackage.Name, "_test") ||
		strings.HasSuffix(candidate.Position.Filename, "_test.go") {
		return ""
	}
	if isInternal(path) {
		return ""
	}
	if _, pinned := engine.options.PinnedLibraries[path]; pinned {
		return ""
	}
	return fmt.Sprintf(
		"exported from %s, a library package outside internal/ that code outside the repository may import",
		path,
	)
}

func isInternal(path string) bool {
	for _, element := range strings.Split(path, "/") {
		if element == "internal" {
			return true
		}
	}
	return false
}

// stringLiterals holds every string literal in the root packages that could name a declaration: whole, and
// split at dots (a template's .Field, a cmp path, "pkg.Name"), each with every site that spells it. A test's
// name is also held wherever it is a word of a literal, since go test runs a test by a pattern that spells it
// ("-run=^TestName$") and a check may look for its source ("func TestName(").
type stringLiterals struct {
	whole     map[string][]literalSite
	parts     map[string][]literalSite
	testNames map[string][]literalSite
}

// isTestRunnerFunction is whether a candidate is a function go test runs by name: a Test, Benchmark, Fuzz or
// Example function in a _test.go file.
func isTestRunnerFunction(candidate *candidate) bool {
	if candidate.Kind != KindFunc || !strings.HasSuffix(candidate.Position.Filename, "_test.go") {
		return false
	}
	return isTestRunnerName(candidate.OldName)
}

// isTestRunnerName is go test's reading of a test function's name: a prefix, then the end or anything but a
// lower-case letter.
func isTestRunnerName(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if rest, found := strings.CutPrefix(name, prefix); found {
			first, _ := utf8.DecodeRuneInString(rest)
			return rest == "" || !unicode.IsLower(first)
		}
	}
	return false
}

// literalSite is one string literal: its file relative to the root and as loaded, its position for a reader, the
// name it spells (whole or as a dotted part) and its whole value.
type literalSite struct {
	file     string
	filename string
	where    string
	name     string
	value    string
}

// nameReadingSites is the literal sites that could name candidate. A string reaches a Go identifier only through
// something that reads Go names while the program runs, or through a test that reads Go source. Reflect reads a
// field's, a method's and a type's name (StructField.Name, MethodByName, Type.Name), a template selects a field or
// a method by name, an encoder without a tag reads a field's, and gob and xml a type's. The encoding guard follows
// every value handed to one of those and refuses the field or type it reaches (refuseEncoded), whichever package
// spells the string. What the literal guard adds is a second net for what that analysis cannot follow, cast where
// a reader is:
//   - in a package variant that reads Go names through reflect (readsNamesThroughReflect) or runs a template and
//     sees the declaration's package, for a field, a method or a type, the names those read: a value of a type
//     reaches a package that cannot name it only as an argument the encoding analysis follows;
//   - anywhere, for a method whose receiver the encoding analysis finds reaching reflect or a template, since the
//     encoding guard judges no method;
//   - anywhere, for a dotted literal that also spells the declaration's package name ("program.Graph",
//     "program.(*Graph).Run"), the shape fmt's %T, reflect's Type.String and a runtime function name print;
//   - in a test file that reads Go source (scanSourceReadingTests), for any declaration.
//
// A function, variable or constant is read by no reflect call or template, so only the last two reach it. A literal
// anywhere else cannot name the declaration: "config" handed to git, "minItems" as a JSON schema keyword, "t" as a
// CSS unit.
func (engine *engine) nameReadingSites(candidate *candidate, sites []literalSite) []literalSite {
	if len(sites) == 0 {
		return nil
	}
	readByReflection := false
	switch candidate.Kind {
	case KindField, KindEmbeddedField, KindType:
		readByReflection = true
	case KindMethod:
		if engine.receiverReachesNameReader(candidate) {
			return sites
		}
		readByReflection = true
	}
	packageName := strings.TrimSuffix(candidate.object.Pkg().Name(), "_test")
	var kept []literalSite
	for _, site := range sites {
		switch {
		case readByReflection && engine.readsNames(site.filename, candidate.Key.PackagePath):
		case spellsQualified(site, packageName):
		case engine.sourceReadingTests[site.filename]:
		default:
			continue
		}
		kept = append(kept, site)
	}
	return kept
}

// receiverReachesNameReader reports whether a value of a method's receiver type reaches reflect, in a package
// reading names through it, or a template: either may call the method by name.
func (engine *engine) receiverReachesNameReader(candidate *candidate) bool {
	function, isFunction := candidate.object.(*types.Func)
	if !isFunction || function.Signature().Recv() == nil {
		return false
	}
	receiverType, _ := dereference(function.Signature().Recv().Type())
	named, isNamed := types.Unalias(receiverType).(*types.Named)
	if !isNamed {
		return false
	}
	return engine.encoded.types[engine.keyer.key(named.Origin().Obj())]&(encoderReflect|encoderTemplate) != 0
}

// spellsQualified reports whether a dotted literal spells the site's name together with the package name before
// it: "program.Graph", "*program.Graph", "cohere/internal/program.(*Graph).Run".
func spellsQualified(site literalSite, packageName string) bool {
	parts := strings.Split(site.value, ".")
	for index := 1; index < len(parts); index++ {
		if parts[index] != site.name && !strings.HasPrefix(parts[index], site.name+"(") {
			continue
		}
		for _, before := range parts[:index] {
			if strings.TrimLeft(before[strings.LastIndex(before, "/")+1:], "*&[]") == packageName {
				return true
			}
		}
	}
	return false
}

// readsNames reports whether any package variant holding the file reads Go names through reflect or runs a
// template, and is or imports, directly or not, the package at path.
func (engine *engine) readsNames(filename string, path string) bool {
	for _, variant := range engine.variantsOfFile[filename] {
		if !engine.encoded.readsNamesThroughReflect(variant) && !engine.runsTemplates(variant) {
			continue
		}
		if engine.importedPaths[variant] == nil {
			engine.importedPaths[variant] = map[string]bool{}
			for _, imported := range importClosure(variant.Types) {
				engine.importedPaths[variant][imported.Path()] = true
			}
		}
		if engine.importedPaths[variant][path] {
			return true
		}
	}
	return false
}

// runsTemplates reports whether a package variant uses text/template or html/template, which select a field or
// call a method by the name a template's text spells.
func (engine *engine) runsTemplates(goPackage *packages.Package) bool {
	if runs, known := engine.templateRunners[goPackage]; known {
		return runs
	}
	runs := false
	for _, object := range goPackage.TypesInfo.Uses {
		if object != nil &&
			object.Pkg() != nil &&
			(object.Pkg().Path() == "text/template" || object.Pkg().Path() == "html/template") {
			runs = true
			break
		}
	}
	engine.templateRunners[goPackage] = runs
	return runs
}

// sourceParsers are the functions that read Go source from disk by themselves, by package path and name.
// go/parser's ParseFile is judged apart: it reads the disk only when handed no source.
var sourceParsers = map[string]map[string]bool{
	"go/parser":                      {"ParseDir": true},
	"go/build":                       {"Import": true, "ImportDir": true},
	"golang.org/x/tools/go/packages": {"Load": true},
}

// fileReaders are the functions that read a file or walk a directory, by package path and name: a call of one
// reads Go source when its arguments spell a Go file's name (os.ReadFile(filepath.Join(directory, "rule.go")),
// filepath.Glob("*.go"), a walk whose callback keeps the paths ending in ".go").
var fileReaders = map[string]map[string]bool{
	"os":            {"ReadFile": true, "Open": true, "ReadDir": true, "DirFS": true},
	"io/fs":         {"ReadFile": true, "ReadDir": true, "WalkDir": true, "Glob": true},
	"path/filepath": {"Glob": true, "Walk": true, "WalkDir": true},
}

// scanSourceReadingTests finds the test files that read Go source text: one that calls a source parser, go/parser's
// ParseFile with no source handed to it, or a file reader whose arguments spell a Go file's name. A literal in such
// a file may be matched against the source it read, so it can name any declaration. A test that writes a Go file
// as a fixture and reads other files is no reader of source.
func (engine *engine) scanSourceReadingTests() map[string]bool {
	readers := map[string]bool{}
	seenFiles := map[string]bool{}
	for _, rootsPackage := range engine.roots {
		for _, file := range rootsPackage.Syntax {
			filename := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			if seenFiles[filename] || !strings.HasSuffix(filename, "_test.go") {
				continue
			}
			seenFiles[filename] = true
			reads := false
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall || reads {
					return !reads
				}
				function := calledFunction(rootsPackage, call)
				if function == nil || function.Pkg() == nil {
					return true
				}
				path, name := function.Pkg().Path(), function.Name()
				switch {
				case sourceParsers[path][name]:
					reads = true
				case path == "go/parser" &&
					name == "ParseFile" &&
					len(call.Args) >= 3 &&
					rootsPackage.TypesInfo.Types[call.Args[2]].IsNil():
					reads = true
				case fileReaders[path][name]:
					for _, argument := range call.Args {
						reads = reads || spellsGoFile(argument)
					}
				}
				return !reads
			})
			if reads {
				readers[filename] = true
				engine.result.Notes = append(
					engine.result.Notes,
					"a test reading Go source, whose literals may name any declaration: "+engine.relative(filename),
				)
			}
		}
	}
	return readers
}

// spellsGoFile reports whether an expression holds a string literal ending in ".go".
func spellsGoFile(expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		literal, isLiteral := node.(*ast.BasicLit)
		if isLiteral && literal.Kind == token.STRING {
			value, unquoteError := strconv.Unquote(literal.Value)
			found = found || (unquoteError == nil && strings.HasSuffix(value, ".go"))
		}
		return !found
	})
	return found
}

// calledFunction is the function or method a call names, or nil for a call through a value or a conversion.
func calledFunction(goPackage *packages.Package, call *ast.CallExpr) *types.Func {
	function := ast.Unparen(call.Fun)
	switch typed := function.(type) {
	case *ast.IndexExpr:
		function = typed.X
	case *ast.IndexListExpr:
		function = typed.X
	}
	var object types.Object
	switch typed := function.(type) {
	case *ast.Ident:
		object = goPackage.TypesInfo.Uses[typed]
	case *ast.SelectorExpr:
		if selection, found := goPackage.TypesInfo.Selections[typed]; found {
			object = selection.Obj()
		} else {
			object = goPackage.TypesInfo.Uses[typed.Sel]
		}
	}
	called, _ := object.(*types.Func)
	return called
}

// unreviewedLiteral lists every site no reviewed literal clears, or is "". A review clears a name in one file,
// read by a person and judged unable to name the declaration (a word in a list, a JavaScript global, an
// environment variable handed to a command): see Options.ReviewedLiterals.
func (engine *engine) unreviewedLiteral(sites []literalSite) string {
	var unreviewed []string
	for _, site := range sites {
		if _, reviewed := engine.options.ReviewedLiterals[site.name][site.file]; reviewed {
			engine.usedReviews[site.name+"\x00"+site.file] = true
			continue
		}
		unreviewed = append(unreviewed, site.where)
	}
	sort.Strings(unreviewed)
	return strings.Join(unreviewed, ", ")
}

func (engine *engine) scanStringLiterals() stringLiterals {
	literals := stringLiterals{
		whole:     map[string][]literalSite{},
		parts:     map[string][]literalSite{},
		testNames: map[string][]literalSite{},
	}
	seenFiles := map[string]bool{}
	for _, rootsPackage := range engine.roots {
		for _, file := range rootsPackage.Syntax {
			filename := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			if seenFiles[filename] {
				continue
			}
			seenFiles[filename] = true
			skip := map[*ast.BasicLit]bool{}
			ast.Inspect(file, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.Field:
					if typed.Tag != nil {
						skip[typed.Tag] = true
					}
				case *ast.ImportSpec:
					skip[typed.Path] = true
				case *ast.BasicLit:
					if typed.Kind != token.STRING || skip[typed] {
						return true
					}
					value, unquoteError := strconv.Unquote(typed.Value)
					if unquoteError != nil {
						return true
					}
					where := positionString(rootsPackage.Fset.PositionFor(typed.Pos(), false))
					where = engine.relativePosition(where)
					file := engine.relative(filename)
					siteOf := func(name string) literalSite {
						return literalSite{file: file, filename: filename, where: where, name: name, value: value}
					}
					if token.IsIdentifier(value) {
						literals.whole[value] = append(literals.whole[value], siteOf(value))
					}
					if strings.Contains(value, ".") {
						for _, part := range strings.Split(value, ".") {
							if token.IsIdentifier(part) {
								literals.parts[part] = append(literals.parts[part], siteOf(part))
							}
						}
					}
					words := strings.FieldsFunc(value, func(character rune) bool {
						return character != '_' && !unicode.IsLetter(character) && !unicode.IsDigit(character)
					})
					for _, word := range words {
						if isTestRunnerName(word) {
							literals.testNames[word] = append(literals.testNames[word], siteOf(word))
						}
					}
				}
				return true
			})
		}
	}
	return literals
}

func (engine *engine) relativePosition(where string) string {
	return strings.TrimPrefix(where, engine.workspace.Root+"/")
}

// scanDirectives finds the declarations whose names something outside Go's resolution fixes: the local name of
// a //go:linkname and the target it names, a function a cgo //export hands to C, and what an Example function's
// name refers to, which go vet checks.
func (engine *engine) scanDirectives() map[Key]string {
	directed := map[Key]string{}
	seenFiles := map[string]bool{}
	for _, rootsPackage := range engine.roots {
		for _, file := range rootsPackage.Syntax {
			filename := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			for _, group := range file.Comments {
				for _, comment := range group.List {
					fields := strings.Fields(comment.Text)
					if len(fields) < 2 || fields[0] != "//go:linkname" {
						continue
					}
					if object := rootsPackage.Types.Scope().Lookup(fields[1]); object != nil {
						directed[engine.keyer.key(
							object,
						)] = "named by a //go:linkname directive at " + engine.relative(filename)
					}
					if len(fields) >= 3 {
						dot := strings.LastIndex(fields[2], ".")
						if dot > 0 {
							for _, other := range engine.roots {
								if other.PkgPath == fields[2][:dot] {
									if object := other.Types.Scope().Lookup(fields[2][dot+1:]); object != nil {
										directed[engine.keyer.key(
											object,
										)] = "the target of a //go:linkname directive at " + engine.relative(filename)
									}
								}
							}
						}
					}
				}
			}
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction {
					continue
				}
				if function.Recv == nil && function.Doc != nil {
					for _, comment := range function.Doc.List {
						if comment.Text == "//export "+function.Name.Name {
							if object := rootsPackage.TypesInfo.Defs[function.Name]; object != nil {
								directed[engine.keyer.key(object)] = "handed to C by a cgo //export directive"
							}
						}
					}
				}
				if seenFiles[filename] ||
					function.Recv != nil ||
					!strings.HasSuffix(filename, "_test.go") ||
					!isExampleName(function.Name.Name) {
					continue
				}
				for _, part := range strings.Split(strings.TrimPrefix(function.Name.Name, "Example"), "_") {
					if part == "" {
						continue
					}
					directed[Key{
						PackagePath: strings.TrimSuffix(rootsPackage.PkgPath, "_test"),
						Name:        "example " + part,
					}] =
						"named by the example function " +
							function.Name.Name +
							", which go vet checks against the package"
				}
			}
			seenFiles[filename] = true
		}
	}
	return directed
}

// isExampleName is go test's reading of an example's name: Example, or Example followed by anything but a
// lower-case letter.
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

// scanUnkeyedReferences finds names a root package reaches, in a workspace package, through an object whose
// key matches no declaration in the root packages' syntax: a variant the load type-checked without source.
// The engine cannot edit what it cannot see, so any rename of such a name is refused.
func (engine *engine) scanUnkeyedReferences() map[string]string {
	declared := map[Key]bool{}
	for _, rootsPackage := range engine.roots {
		for _, object := range rootsPackage.TypesInfo.Defs {
			if object != nil {
				declared[engine.keyer.key(object)] = true
			}
		}
	}
	unkeyed := map[string]string{}
	for _, rootsPackage := range engine.roots {
		for identifier, object := range rootsPackage.TypesInfo.Uses {
			if object == nil || object.Pkg() == nil || !engine.workspacePaths[object.Pkg().Path()] {
				continue
			}
			switch typed := object.(type) {
			case *types.PkgName, *types.Label:
				continue
			case *types.Var:
				if !typed.IsField() && typed.Parent() != typed.Pkg().Scope() {
					continue
				}
			case *types.TypeName:
				if typed.Parent() != typed.Pkg().Scope() && typed.Parent() != nil {
					continue
				}
			}
			if declared[engine.keyer.key(object)] {
				continue
			}
			if _, recorded := unkeyed[object.Name()]; !recorded {
				unkeyed[object.Name()] = fmt.Sprintf(
					"%s in %s",
					rootsPackage.Fset.Position(identifier.Pos()),
					rootsPackage.ID,
				)
				engine.result.Notes = append(
					engine.result.Notes,
					"unkeyed reference: "+object.Name()+" at "+unkeyed[object.Name()],
				)
			}
		}
	}
	return unkeyed
}

// refuseForeignInterfaces refuses a method whose type satisfies, with that method, an interface declared in
// another package: the rules circle's foreign interfaces (every interface in the import graph, generic ones by
// shape, and error) and any interface of another workspace package that the type satisfies, judged in every
// root package that can see both. An interface whose own method of that name is renamed alike forces nothing.
func (engine *engine) refuseForeignInterfaces() {
	methodNames := map[string]bool{}
	var methods []*candidate
	for _, key := range engine.sortedCandidateKeys() {
		candidate := engine.candidates[key]
		if _, open := engine.accepted[key]; !open || candidate.Kind != KindMethod {
			continue
		}
		methods = append(methods, candidate)
		methodNames[candidate.OldName] = true
	}
	if len(methods) == 0 {
		return
	}
	errorType := types.Universe.Lookup("error").Type()
	localInterfaces := engine.localInterfaces(
		func(interfaceType *types.Interface) bool { return interfaceHasMethodNamed(interfaceType, methodNames) },
	)
	for _, rootsPackage := range engine.roots {
		closure := importClosure(rootsPackage.Types)
		byPath := map[string]*types.Package{}
		var interfaces []interfaceEntry
		for _, visible := range closure {
			byPath[visible.Path()] = visible
			scope := visible.Scope()
			for _, name := range scope.Names() {
				typeName, isTypeName := scope.Lookup(name).(*types.TypeName)
				if !isTypeName || typeName.IsAlias() {
					continue
				}
				interfaceType, isInterface := typeName.Type().Underlying().(*types.Interface)
				if !isInterface || !interfaceHasMethodNamed(interfaceType, methodNames) {
					continue
				}
				named, _ := typeName.Type().(*types.Named)
				interfaces = append(interfaces, interfaceEntry{
					name: visible.Path() + "." + name, interfaceType: interfaceType, home: visible.Path(),
					generic: named != nil && named.TypeParams().Len() > 0,
				})
			}
		}
		// error is the universe's interface, so this always holds; were it not, there is no error interface to keep.
		if errorInterface, isInterface := errorType.Underlying().(*types.Interface); isInterface {
			interfaces = append(interfaces, interfaceEntry{name: "error", interfaceType: errorInterface, home: ""})
		}
		interfaces = append(interfaces, localInterfaces...)
		for _, method := range methods {
			if _, open := engine.accepted[method.Key]; !open {
				continue
			}
			implementer := engine.implementerIn(method, rootsPackage, byPath)
			if implementer == nil {
				continue
			}
			for _, entry := range interfaces {
				if entry.home == method.Key.PackagePath ||
					!interfaceHasMethodNamed(entry.interfaceType, map[string]bool{method.OldName: true}) {
					continue
				}
				if engine.renamedAlike(entry.interfaceType, method) {
					continue
				}
				test := satisfies
				if entry.generic {
					test = satisfiesInShape
				}
				if !test(implementer, entry.interfaceType, actualName) {
					continue
				}
				found := lookupMember(implementer, true, method.object.Pkg(), method.OldName, actualName)
				if found.object == nil || engine.keyer.key(found.object) != method.Key {
					continue
				}
				engine.reject(
					method.Key,
					"implements an interface of another package",
					fmt.Sprintf("%s satisfies %s, which names %s", implementer, entry.name, method.OldName),
				)
				break
			}
		}
	}
}

// renamedAlike reports whether an interface's own method of the method's old name is accepted under its new name:
// the interface is ours and moves with its implementations, which the verification's satisfaction check then
// holds to it, refusing them all together if either side is refused later.
func (engine *engine) renamedAlike(interfaceType *types.Interface, method *candidate) bool {
	for index := range interfaceType.NumMethods() {
		declared := interfaceType.Method(index)
		if declared.Name() != method.OldName {
			continue
		}
		newName, accepted := engine.accepted[engine.keyer.key(origin(declared))]
		return accepted && newName == method.NewName
	}
	return false
}

func interfaceHasMethodNamed(interfaceType *types.Interface, names map[string]bool) bool {
	for index := range interfaceType.NumMethods() {
		if names[interfaceType.Method(index).Name()] {
			return true
		}
	}
	return false
}

// implementerIn is the type whose method set holds method, as goPackage sees it: a pointer to the receiver's named
// type, or the interface for an interface's method. Nil when goPackage cannot see the type.
func (engine *engine) implementerIn(
	method *candidate,
	goPackage *packages.Package,
	byPath map[string]*types.Package,
) types.Type {
	function, isFunction := method.object.(*types.Func)
	if !isFunction {
		// A method candidate's object is a *types.Func; anything else has no receiver to implement on.
		return nil
	}
	receiverType, _ := dereference(function.Signature().Recv().Type())
	named, isNamed := types.Unalias(receiverType).(*types.Named)
	if !isNamed {
		// A method of an interface literal: only its own package spells it.
		if goPackage == method.goPackage {
			return receiverType
		}
		return nil
	}
	typeName := named.Origin().Obj()
	if typeName.Parent() != typeName.Pkg().Scope() {
		if goPackage == method.goPackage {
			return implementerOf(named.Origin())
		}
		return nil
	}
	visible := byPath[method.Key.PackagePath]
	if visible == nil {
		return nil
	}
	seen, isTypeName := visible.Scope().Lookup(typeName.Name()).(*types.TypeName)
	if !isTypeName {
		return nil
	}
	return implementerOf(seen.Type())
}

func implementerOf(goType types.Type) types.Type {
	if types.IsInterface(goType) {
		return goType
	}
	return types.NewPointer(goType)
}
