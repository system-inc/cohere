package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"sort"
	"strconv"
	"strings"
)

// surroundings is what a split reads beyond a directory's own files: the name an import binds, and whether the build
// the rules run under compiles a file.
type surroundings struct {
	importName func(string) string
	builds     func(directory string, name string, source []byte) bool
	// pathsInCode is every path from the root ending in .go that the repository's Go code spells in a string
	// literal, so a file another directory's code reads by path keeps its name too.
	pathsInCode map[string]bool
}

// goFileLiterals is every string literal of syntax that ends in .go, unquoted.
func goFileLiterals(syntax *ast.File) []string {
	var literals []string
	ast.Inspect(syntax, func(node ast.Node) bool {
		literal, isLiteral := node.(*ast.BasicLit)
		if !isLiteral || literal.Kind != token.STRING {
			return true
		}
		if value, unquoteError := strconv.Unquote(
			literal.Value,
		); unquoteError == nil && strings.HasSuffix(value, ".go") {
			literals = append(literals, value)
		}
		return true
	})
	return literals
}

// hostBuild is whether context's build compiles a file, read as go/build reads it: the name's `GOOS` and `GOARCH`
// suffix, a leading underscore or dot, the //go:build line, and cgo. cohere's Go engine loads packages with
// go/packages under the environment it runs in, so a file this rejects is one its rules never see.
func hostBuild(context build.Context) func(directory string, name string, source []byte) bool {
	return func(directory string, name string, source []byte) bool {
		local := context
		local.OpenFile = func(string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(source)), nil
		}
		matched, matchError := local.MatchFile(directory, name)
		return matchError == nil && matched
	}
}

// packageGroup is the files of one directory that share a package clause.
type packageGroup struct {
	directory   string
	packageName string
	fileSet     *token.FileSet
	files       []*sourceFile
	// namedInCode is the file names of the directory that Go code spells in a string literal, a test reading a
	// source file or a list of them: a name the code reads, which the name pass never takes away.
	namedInCode map[string]bool
}

type methodRecord struct {
	declaration *ast.FuncDecl
	file        *sourceFile
}

// parseGroups parses a directory's files and groups them by package clause. pinned names files
// nothing may move into or out of, with the reason. A file the build does not compile, a generated
// file and a declared copy are pinned too, since the rules never judge them.
func parseGroups(
	directory string,
	sources map[string][]byte,
	pinned map[string]string,
	around surroundings,
) ([]*packageGroup, error) {
	fileSet := token.NewFileSet()
	groupsByName := map[string]*packageGroup{}
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		syntax, parseFileError := parser.ParseFile(fileSet, name, sources[name], parser.ParseComments)
		if parseFileError != nil {
			return nil, fmt.Errorf("%s: %w", name, parseFileError)
		}
		file := &sourceFile{name: name, source: sources[name], syntax: syntax, tokenFile: fileSet.File(syntax.Pos())}
		buildExpression := ""
		copied := false
		for _, group := range syntax.Comments {
			if group.Pos() >= syntax.Package {
				break
			}
			for _, comment := range group.List {
				if strings.HasPrefix(comment.Text, "//go:build") || strings.HasPrefix(comment.Text, "// +build") {
					file.buildLines = append(file.buildLines, comment.Text)
				}
				if strings.HasPrefix(comment.Text, "//go:build") {
					buildExpression = strings.TrimSpace(strings.TrimPrefix(comment.Text, "//go:build"))
				}
				// The header cohere's copied package reads: the engine withholds every finding in such a file.
				if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(comment.Text, "//")), "cohere:copied") {
					copied = true
				}
			}
		}
		_, suffix := fileNameParts(name)
		file.key = suffix + "|" + buildExpression
		file.built = around.builds(directory, name, sources[name])
		file.judged = file.built && !ast.IsGenerated(syntax) && !copied
		switch reason, isPinned := pinned[name]; {
		case isPinned:
			file.pinned = reason
		case !file.built:
			file.pinned = "not built where the rules run"
		case ast.IsGenerated(syntax):
			file.pinned = "generated"
		case copied:
			file.pinned = "a copy of upstream source"
		}
		file.importNames = map[string]*ast.ImportSpec{}
		for _, spec := range syntax.Imports {
			path := importPath(spec)
			if path == "C" {
				file.pinned = "cgo"
				continue
			}
			bound := boundImportName(spec, around.importName)
			if bound == "_" {
				continue
			}
			if bound == "." {
				file.pinned = "dot import"
				continue
			}
			file.importNames[bound] = spec
		}
		if file.pinned == "" {
			used := usedSelectorNames(syntax)
			for bound, spec := range file.importNames {
				if !used[bound] {
					file.pinned = "import name not known: " + importPath(spec)
				}
			}
		}
		group := groupsByName[syntax.Name.Name]
		if group == nil {
			group = &packageGroup{directory: directory, packageName: syntax.Name.Name, fileSet: fileSet}
			groupsByName[syntax.Name.Name] = group
		}
		group.files = append(group.files, file)
	}
	namedInCode := map[string]bool{}
	for _, group := range groupsByName {
		for _, file := range group.files {
			for _, literal := range goFileLiterals(file.syntax) {
				if !strings.Contains(literal, "/") {
					namedInCode[literal] = true
				}
			}
		}
	}
	for _, group := range groupsByName {
		for _, file := range group.files {
			if around.pathsInCode[joinPath(directory, file.name)] {
				namedInCode[file.name] = true
			}
		}
		group.namedInCode = namedInCode
	}
	groupNames := make([]string, 0, len(groupsByName))
	for name := range groupsByName {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	groups := make([]*packageGroup, 0, len(groupNames))
	for _, name := range groupNames {
		groups = append(groups, groupsByName[name])
	}
	return groups, nil
}

func importPath(spec *ast.ImportSpec) string {
	return strings.Trim(spec.Path.Value, "\"`")
}

func boundImportName(spec *ast.ImportSpec, importName func(string) string) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	return importName(importPath(spec))
}

// usedSelectorNames is every identifier used as the left side of a selector that resolves to nothing
// in its file: an import, or a package-level declaration of another file.
func usedSelectorNames(syntax *ast.File) map[string]bool {
	used := map[string]bool{}
	ast.Inspect(syntax, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if identifier, ok := selector.X.(*ast.Ident); ok && identifier.Obj == nil {
				used[identifier.Name] = true
			}
		}
		return true
	})
	return used
}

// receiverTypeName is the name of the type a method is declared on, through pointers and type
// parameters, or empty for a function.
func receiverTypeName(declaration *ast.FuncDecl) string {
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
		return ""
	}
	expression := declaration.Recv.List[0].Type
	for {
		switch node := expression.(type) {
		case *ast.StarExpr:
			expression = node.X
		case *ast.ParenExpr:
			expression = node.X
		case *ast.IndexExpr:
			expression = node.X
		case *ast.IndexListExpr:
			expression = node.X
		case *ast.Ident:
			return node.Name
		default:
			return ""
		}
	}
}

// typesOf lists the group's top-level named types in file and source order, with the methods the
// build compiles. A receiver spelled with a local alias is the aliased type's method, as the rules
// read it through the types.
func typesOf(group *packageGroup) []*typeRecord {
	var records []*typeRecord
	byName := map[string][]*typeRecord{}
	aliasOf := map[string]string{}
	for _, file := range group.files {
		for _, declaration := range file.syntax.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
				if !isTypeSpec {
					continue
				}
				if typeSpec.Assign != token.NoPos {
					if file.built {
						aliasOf[typeSpec.Name.Name] = typeExpressionName(typeSpec.Type)
					}
					continue
				}
				if typeSpec.Name.Name == "_" {
					continue
				}
				record := &typeRecord{name: typeSpec.Name.Name, file: file, declaration: general, spec: typeSpec}
				byName[record.name] = append(byName[record.name], record)
				records = append(records, record)
			}
		}
	}
	for _, sameName := range byName {
		if len(sameName) > 1 {
			for _, record := range sameName {
				record.ambiguous = true
			}
		}
	}
	for _, file := range group.files {
		if !file.built {
			continue
		}
		for _, declaration := range file.syntax.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			for _, record := range byName[resolveAlias(receiverTypeName(function), aliasOf)] {
				record.methods = append(record.methods, methodRecord{declaration: function, file: file})
			}
		}
	}
	return records
}

// typeExpressionName is the local type an alias names, through a pointer-free generic instance, or empty.
func typeExpressionName(expression ast.Expr) string {
	current := expression
	for {
		switch node := current.(type) {
		case *ast.ParenExpr:
			current = node.X
		case *ast.IndexExpr:
			current = node.X
		case *ast.IndexListExpr:
			current = node.X
		case *ast.Ident:
			return node.Name
		default:
			return ""
		}
	}
}

// resolveAlias follows name through local aliases to the type they name, stopping at a cycle.
func resolveAlias(name string, aliasOf map[string]string) string {
	seen := map[string]bool{}
	current := name
	for {
		target, isAlias := aliasOf[current]
		if !isAlias || target == "" || seen[current] {
			return current
		}
		seen[current] = true
		current = target
	}
}

// isMarkerOnly mirrors cohere's internal/goengine/rules/markertype.IsMarkerOnly, which the rules call
// and which a module outside cohere cannot import: methods are all markers, each with a receiver, no
// parameters, no results and a body with no statements, the way a sum type's variant declares its
// membership. None at all is no marker type.
func isMarkerOnly(methods []*ast.FuncDecl) bool {
	if len(methods) == 0 {
		return false
	}
	for _, method := range methods {
		isMarker := method.Recv != nil && method.Body != nil && len(method.Body.List) == 0 &&
			method.Type.Params.NumFields() == 0 && method.Type.Results.NumFields() == 0
		if !isMarker {
			return false
		}
	}
	return true
}

// measure counts a group's types with methods, the files the rules judge that serve more than one of
// them, and the files past lineLimit. Pinned files are counted on their own.
func measure(group *packageGroup) counts {
	var result counts
	parts := partsOf(group)
	for _, file := range group.files {
		if file.pinned != "" {
			result.pinnedFiles++
			if len(parts[file]) > 1 {
				result.pinnedFilesServingSeveral++
			}
		}
		if !file.judged {
			continue
		}
		if bytes.Count(file.source, []byte("\n")) > lineLimit {
			result.filesOverLineLimit++
		}
		if len(parts[file]) > 1 {
			result.filesServingSeveral++
			result.typesInFilesServingSeveral += len(parts[file])
			result.extraTypes += len(parts[file]) - 1
		}
	}
	result.misnamed = len(misnamedFiles(group))
	for _, record := range typesOf(group) {
		if record.counted() && record.file.judged {
			result.typesWithMethods++
			if record.ambiguous {
				result.ambiguousTypesWithMethods++
			}
		}
	}
	return result
}

// servingSeveral is each judged file of group that serves more than one type, with the types in
// source order, as "name\tFirst Second".
func servingSeveral(group *packageGroup) []string {
	var lines []string
	parts := partsOf(group)
	for _, file := range group.files {
		if !file.judged || len(parts[file]) < 2 {
			continue
		}
		var names []string
		for _, piece := range parts[file] {
			names = append(names, piece.name)
		}
		lines = append(lines, file.name+"\t"+strings.Join(names, " "))
	}
	return lines
}
