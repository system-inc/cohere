package global_rename

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// outsideFiles is what the Go files the load does not type-check can name. A file excluded by a build
// constraint (a _windows.go file, a //go:build tag) belongs to a loaded package yet is never checked, so any
// identifier in it may name a declaration a rename would move; so may a file outside every package (testdata,
// the TypeScript submodule) that imports a workspace package.
type outsideFiles struct {
	// names maps an identifier name to the files outside the load that spell it, relative to the root. A
	// selector on a package the file imports from outside the workspace, `runtime.GOOS`, spells that package's
	// name, not ours, and is not counted.
	names map[string][]string
	// files describes each file whose identifiers were read, by its relative name.
	files map[string]*outsideFile
	// fileSet positions the files read.
	fileSet *token.FileSet
	// seenFrom maps a workspace package path to a file outside the load that imports it, directly or through
	// other workspace packages, when that file's identifiers were not read.
	seenFrom map[string]string
	// scanned counts the files read.
	scanned int
}

// outsideFile is what one file outside the load holds, enough to tell whether it can see a package-level name
// and whether its only spellings of that name are declarations of its own.
type outsideFile struct {
	filename    string
	directory   string
	packageName string
	imports     map[string]bool
	// declarations maps a name to the identifiers of the file's top-level declarations of it, by kind.
	declarations map[string][]outsideDeclaration
	// spellings counts each name's identifiers in the file, declarations included.
	spellings map[string]int
}

// outsideDeclaration is one top-level declaration in a file outside the load.
type outsideDeclaration struct {
	kind   string
	offset int
	doc    *ast.CommentGroup
}

func (engine *engine) scanOutside() *outsideFiles {
	outside := &outsideFiles{
		names:    map[string][]string{},
		files:    map[string]*outsideFile{},
		seenFrom: map[string]string{},
	}
	loaded := map[string]bool{}
	ignored := map[string]bool{}
	for _, rootsPackage := range engine.roots {
		for _, list := range [][]string{rootsPackage.GoFiles, rootsPackage.CompiledGoFiles, rootsPackage.OtherFiles} {
			for _, filename := range list {
				loaded[filepath.Clean(filename)] = true
			}
		}
		for _, filename := range rootsPackage.IgnoredFiles {
			ignored[filepath.Clean(filename)] = true
		}
	}
	directImporters := map[string]string{}
	fileSet := token.NewFileSet()
	outside.fileSet = fileSet
	readFile := func(filename string, identifiers bool) {
		if loaded[filename] {
			return
		}
		mode := parser.ImportsOnly | parser.SkipObjectResolution
		if identifiers {
			// Object resolution tells a package name, which it leaves unresolved, from a local of the same name.
			mode = parser.ParseComments
		}
		file, parseError := parser.ParseFile(fileSet, filename, nil, mode)
		if parseError != nil {
			if file == nil {
				engine.result.Notes = append(
					engine.result.Notes,
					"outside the load, unparsable: "+engine.relative(filename),
				)
				return
			}
		}
		outside.scanned++
		importsWorkspace := false
		for _, spec := range file.Imports {
			path, unquoteError := strconv.Unquote(spec.Path.Value)
			if unquoteError == nil && engine.workspacePaths[path] {
				importsWorkspace = true
				if !identifiers {
					if _, recorded := directImporters[path]; !recorded {
						directImporters[path] = engine.relative(filename)
					}
				}
			}
		}
		if !identifiers || (!importsWorkspace && !ignored[filename]) {
			return
		}
		relative := engine.relative(filename)
		described := &outsideFile{
			filename: filename, directory: filepath.Dir(filename), packageName: file.Name.Name,
			imports: map[string]bool{}, declarations: map[string][]outsideDeclaration{}, spellings: map[string]int{},
		}
		outside.files[relative] = described
		foreignPackageNames := map[string]bool{}
		for _, spec := range file.Imports {
			path, unquoteError := strconv.Unquote(spec.Path.Value)
			if unquoteError != nil {
				continue
			}
			described.imports[path] = true
			if engine.workspacePaths[path] {
				continue
			}
			name := filepath.Base(path)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			foreignPackageNames[name] = true
		}
		for _, declaration := range file.Decls {
			record := func(identifier *ast.Ident, kind string, doc *ast.CommentGroup) {
				offset := fileSet.PositionFor(identifier.Pos(), false).Offset
				described.declarations[identifier.Name] = append(
					described.declarations[identifier.Name],
					outsideDeclaration{kind: kind, offset: offset, doc: doc},
				)
			}
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if typed.Recv == nil {
					record(typed.Name, KindFunc, typed.Doc)
				}
			case *ast.GenDecl:
				kind := map[token.Token]string{
					token.VAR:   KindVariable,
					token.CONST: KindConst,
					token.TYPE:  KindType,
				}[typed.Tok]
				for _, spec := range typed.Specs {
					doc := typed.Doc
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if spec.Doc != nil || len(typed.Specs) > 1 {
							doc = spec.Doc
						}
						record(spec.Name, kind, doc)
					case *ast.ValueSpec:
						if spec.Doc != nil || len(typed.Specs) > 1 {
							doc = spec.Doc
						}
						for _, name := range spec.Names {
							record(name, kind, doc)
						}
					}
				}
			}
		}
		foreignSelections := map[*ast.Ident]bool{}
		ast.Inspect(file, func(node ast.Node) bool {
			if selector, isSelector := node.(*ast.SelectorExpr); isSelector {
				if qualifier, isIdentifier := selector.X.(*ast.Ident); isIdentifier &&
					qualifier.Obj == nil &&
					foreignPackageNames[qualifier.Name] {
					foreignSelections[selector.Sel] = true
				}
			}
			return true
		})
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier || foreignSelections[identifier] {
				return true
			}
			if described.spellings[identifier.Name] == 0 {
				outside.names[identifier.Name] = append(outside.names[identifier.Name], relative)
			}
			described.spellings[identifier.Name]++
			return true
		})
	}

	root := engine.workspace.Root
	typeScript := filepath.Join(root, "TypeScript")
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || path == typeScript) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		readFile(filepath.Clean(path), true)
		return nil
	})
	if walkError != nil {
		engine.result.Notes = append(engine.result.Notes, "walking the root: "+walkError.Error())
	}
	// The TypeScript submodule, often a symbolic link, is too large to read whole: its imports are enough, since a
	// file there can reach a workspace package only by importing it.
	if _, statError := os.Stat(typeScript); statError == nil {
		if resolved, resolveError := filepath.EvalSymlinks(typeScript); resolveError == nil {
			readImport := func(path string, entry fs.DirEntry, pathError error) error {
				if pathError != nil {
					return nil
				}
				if entry.IsDir() {
					if path != resolved && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
						return filepath.SkipDir
					}
					return nil
				}
				if strings.HasSuffix(path, ".go") {
					readFile(filepath.Clean(path), false)
				}
				return nil
			}
			if typeScriptWalkError := filepath.WalkDir(resolved, readImport); typeScriptWalkError != nil {
				engine.result.Notes = append(
					engine.result.Notes,
					"walking the TypeScript submodule: "+typeScriptWalkError.Error(),
				)
			}
		}
	}

	// A package imported from outside makes everything it imports visible there too.
	importsOf := map[string][]string{}
	for _, rootsPackage := range engine.roots {
		for path := range rootsPackage.Imports {
			importsOf[rootsPackage.PkgPath] = append(importsOf[rootsPackage.PkgPath], path)
		}
	}
	paths := make([]string, 0, len(directImporters))
	for path := range directImporters {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		pending := []string{path}
		for len(pending) > 0 {
			current := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if _, recorded := outside.seenFrom[current]; recorded || !engine.workspacePaths[current] {
				continue
			}
			outside.seenFrom[current] = directImporters[path]
			pending = append(pending, importsOf[current]...)
		}
	}
	for name := range outside.names {
		sort.Strings(outside.names[name])
	}
	engine.result.Notes = append(engine.result.Notes,
		"files outside the load read: "+
			strconv.Itoa(outside.scanned)+
			"; workspace packages imported from outside the load: "+
			strconv.Itoa(len(outside.seenFrom)))
	return outside
}
