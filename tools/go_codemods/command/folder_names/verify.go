package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"

	"github.com/system-inc/cohere/internal/go_rename/rename"

	"golang.org/x/tools/go/packages"
)

// record is one identifier, in source order, with the declarations it defines and uses.
type record struct {
	Name string
	Key  string
}

// translation says how a fingerprint taken before the moves reads after them: where each file and package path
// lands, the identifiers that take a new name, and the locals renamed. A fingerprint taken after the moves uses
// the identity.
type translation struct {
	file           func(string) string
	packagePath    func(string) string
	names          map[rename.EditKey]string
	renamedObjects map[types.Object]string
}

func identity(value string) string { return value }

// fingerprint records each file of pkg, by its translated name: every identifier outside an import spec in source
// order, under its translated name, and what it defines and uses. A declaration of the package is keyed by its
// declaring identifier's ordinal in its file, a moved file keeping its ordinals since only names change and import
// specs are not counted; an import's name by the path it imports; anything else by its package path and its
// description. Import specs are compared on their own, by importsOf, since gofmt re-sorts them.
func fingerprint(goPackage *packages.Package, translate translation) map[string][]record {
	ordinals := map[token.Pos]string{}
	identifiersByFile := map[string][]*ast.Ident{}
	for _, file := range goPackage.Syntax {
		filename := goPackage.Fset.PositionFor(file.Pos(), false).Filename
		ordinal := 0
		ast.Inspect(file, func(node ast.Node) bool {
			if _, isImport := node.(*ast.ImportSpec); isImport {
				return false
			}
			if identifier, isIdentifier := node.(*ast.Ident); isIdentifier {
				ordinals[identifier.Pos()] = fmt.Sprintf("%s#%d", translate.file(filename), ordinal)
				identifiersByFile[filename] = append(identifiersByFile[filename], identifier)
				ordinal++
			}
			return true
		})
	}
	key := func(object types.Object) string {
		switch {
		case object == nil:
			return "-"
		case object.Pkg() == nil:
			return "universe:" + object.Name()
		}
		if packageName, isPackageName := object.(*types.PkgName); isPackageName {
			return "import:" + translate.packagePath(packageName.Imported().Path())
		}
		if object.Pkg() == goPackage.Types {
			if ordinal, found := ordinals[object.Pos()]; found {
				return "here:" + ordinal
			}
		}
		name := object.Name()
		if renamed, found := translate.renamedObjects[object]; found {
			name = renamed
		}
		owner := ""
		if function, isFunction := object.(*types.Func); isFunction {
			if signature, isSignature := function.Type().(*types.Signature); isSignature && signature.Recv() != nil {
				owner = types.TypeString(signature.Recv().Type(), func(*types.Package) string { return "" }) + "."
			}
		}
		position := goPackage.Fset.PositionFor(object.Pos(), false)
		return fmt.Sprintf(
			"there:%s:%T %s%s @%s",
			translate.packagePath(object.Pkg().Path()),
			object,
			owner,
			name,
			filepath.Base(position.Filename),
		)
	}
	result := map[string][]record{}
	for filename, identifiers := range identifiersByFile {
		records := make([]record, 0, len(identifiers))
		for _, identifier := range identifiers {
			name := identifier.Name
			offset := goPackage.Fset.PositionFor(identifier.Pos(), false).Offset
			if newName, renamed := translate.names[rename.EditKey{Filename: filename, Offset: offset}]; renamed {
				name = newName
			}
			records = append(records, record{
				Name: name,
				Key:  key(goPackage.TypesInfo.Defs[identifier]) + "|" + key(goPackage.TypesInfo.Uses[identifier]),
			})
		}
		result[translate.file(filename)] = records
	}
	return result
}

// importsOf lists each file's imports by translated file, sorted, each as its name (empty when implicit) and
// translated path. An alias the moves drop is expected gone.
func importsOf(
	goPackage *packages.Package,
	translate translation,
	dropped map[rename.EditKey]bool,
) map[string][]string {
	result := map[string][]string{}
	for _, file := range goPackage.Syntax {
		filename := goPackage.Fset.PositionFor(file.Pos(), false).Filename
		var imports []string
		for _, spec := range file.Imports {
			name := ""
			if spec.Name != nil {
				name = spec.Name.Name
				if dropped[rename.EditKey{
					Filename: filename,
					Offset:   goPackage.Fset.PositionFor(spec.Name.Pos(), false).Offset,
				}] {
					name = ""
				}
			}
			path := spec.Path.Value
			if packageName := importedName(goPackage.TypesInfo, spec); packageName != nil {
				path = translate.packagePath(packageName.Imported().Path())
			}
			imports = append(imports, name+" "+path)
		}
		sort.Strings(imports)
		result[translate.file(filename)] = imports
	}
	return result
}

// expect takes a load's fingerprints before the moves and translates them.
func expect(
	roots []*packages.Package,
	translate translation,
	dropped map[rename.EditKey]bool,
	mapping *mapping,
) *expectation {
	result := &expectation{identifiers: map[string]map[string][]record{}, imports: map[string]map[string][]string{}}
	for _, rootsPackage := range roots {
		id := mapping.NewPackageId(rootsPackage.ID)
		result.identifiers[id] = fingerprint(rootsPackage, translate)
		result.imports[id] = importsOf(rootsPackage, translate, dropped)
	}
	return result
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
