package global_rename

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"sort"

	"github.com/system-inc/cohere/tools/go_codemods/workspace"
)

// platformReading is what the files the host does not build name, read with types under a platform that
// builds them. A package with a build-constrained file (memory_linux.go, file_identity_windows.go) is loaded a
// second time under each `GOOS` its constrained files need, and each identifier in a file only that load builds is
// resolved there: a spelling of a renamed field's name that resolves to a local of the same name is not the field.
// A file no platform builds, or one in a package that does not type-check under its platform, is not read here,
// and the textual guard in outsideRefusal still holds for it.
type platformReading struct {
	// loaded are the platforms loaded, in order, which Apply loads again to verify the rewrite under each.
	loaded []workspace.Platform
	// files are the files only another platform builds, by their name relative to the root.
	files map[string]*platformFile
	// built holds, per platform, every file of a package that type-checks under it.
	built map[workspace.Platform]map[string]bool
	// twins holds, per platform and package path, each package-level declaration in a file only that platform
	// builds, by name.
	twins map[workspace.Platform]map[string]map[string]platformTwin
	// methods holds, per platform and receiver type, the methods declared in files only that platform builds.
	methods map[workspace.Platform]map[Key]map[string]bool
}

// platformFileReading is a file's identifiers as one platform resolves them.
type platformFileReading struct {
	platform    workspace.Platform
	identifiers []platformIdentifier
}

// platformIdentifier is one identifier and the declaration it defines or uses, keyed as the host keys it: by
// package path and position, which a file both loads build reads the same way in each.
type platformIdentifier struct {
	name     string
	offset   int
	key      Key
	resolved bool
	// doc is a package-level declaration's doc comment, for the identifier that declares it.
	doc *ast.CommentGroup
	// docOffsets are the doc comment's lines' offsets in the file.
	docOffsets []int
}

// platformTwin is a package-level declaration in a file only one platform builds: the twin of a declaration
// the host builds in a file of its own, such as availableMemory in memory_linux.go and memory_darwin.go.
type platformTwin struct {
	key  Key
	kind string
}

// read records, from one platform's load, the files it builds, and for each file only it builds, what every
// identifier resolves to, the package-level twins declared there and the methods.
func (reading *platformReading) read(engine *engine, load *workspace.PlatformLoad, hostBuilt map[string]bool) {
	platform := load.Platform
	reading.built[platform] = map[string]bool{}
	if len(load.Clean.Roots) == 0 {
		return
	}
	keyer := newKeyer(load.Clean.Roots[0].Fset)
	built := map[string]bool{}
	reading.built[platform] = built
	reading.twins[platform] = map[string]map[string]platformTwin{}
	reading.methods[platform] = map[Key]map[string]bool{}
	readFiles := map[string]bool{}
	for _, rootsPackage := range load.Clean.Roots {
		for _, file := range rootsPackage.Syntax {
			filename := filepath.Clean(rootsPackage.Fset.PositionFor(file.Pos(), false).Filename)
			built[filename] = true
			relative, inside := load.Clean.Relative(filename)
			if hostBuilt[filename] || !inside || readFiles[filename] {
				continue
			}
			readFiles[filename] = true
			described := reading.files[relative]
			if described == nil {
				described = &platformFile{
					filename: filename, relative: relative, packagePath: rootsPackage.PkgPath,
					editable: load.Clean.Editable(rootsPackage, file), spellings: map[string]int{},
				}
				reading.files[relative] = described
			}
			docs := packageLevelDocs(file)
			fileReading := platformFileReading{platform: platform}
			countSpellings := len(described.readings) == 0
			ast.Inspect(file, func(node ast.Node) bool {
				identifier, isIdentifier := node.(*ast.Ident)
				if !isIdentifier {
					return true
				}
				if countSpellings {
					described.spellings[identifier.Name]++
				}
				record := platformIdentifier{
					name:   identifier.Name,
					offset: rootsPackage.Fset.PositionFor(identifier.Pos(), false).Offset,
				}
				object := rootsPackage.TypesInfo.Defs[identifier]
				if object == nil {
					object = rootsPackage.TypesInfo.Uses[identifier]
				}
				if object != nil {
					record.key = keyer.key(object)
					record.resolved = true
				}
				if doc, documented := docs[identifier]; documented {
					record.doc = doc
					for _, comment := range doc.List {
						record.docOffsets = append(
							record.docOffsets,
							rootsPackage.Fset.PositionFor(comment.Pos(), false).Offset,
						)
					}
				}
				fileReading.identifiers = append(fileReading.identifiers, record)
				return true
			})
			described.readings = append(described.readings, fileReading)
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction || function.Recv == nil {
					continue
				}
				method, isMethod := rootsPackage.TypesInfo.Defs[function.Name].(*types.Func)
				if !isMethod {
					continue
				}
				receiverType, _ := dereference(method.Signature().Recv().Type())
				named, isNamed := types.Unalias(receiverType).(*types.Named)
				if !isNamed {
					continue
				}
				owner := keyer.key(named.Origin().Obj())
				if reading.methods[platform][owner] == nil {
					reading.methods[platform][owner] = map[string]bool{}
				}
				reading.methods[platform][owner][method.Name()] = true
			}
			for identifier, object := range rootsPackage.TypesInfo.Defs {
				if object == nil || object.Parent() != rootsPackage.Types.Scope() {
					continue
				}
				if filepath.Clean(rootsPackage.Fset.PositionFor(identifier.Pos(), false).Filename) != filename {
					continue
				}
				if reading.twins[platform][rootsPackage.PkgPath] == nil {
					reading.twins[platform][rootsPackage.PkgPath] = map[string]platformTwin{}
				}
				reading.twins[platform][rootsPackage.PkgPath][object.Name()] = platformTwin{
					key:  keyer.key(object),
					kind: kindOf(object),
				}
			}
		}
	}
}

// packageLevelDocs maps each identifier a file's top-level declarations declare to the doc comment that reads
// for it: the declaration's own, or its spec's in a group.
func packageLevelDocs(file *ast.File) map[*ast.Ident]*ast.CommentGroup {
	docs := map[*ast.Ident]*ast.CommentGroup{}
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if typed.Recv == nil && typed.Doc != nil {
				docs[typed.Name] = typed.Doc
			}
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				doc := typed.Doc
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					if spec.Doc != nil || len(typed.Specs) > 1 {
						doc = spec.Doc
					}
					if doc != nil {
						docs[spec.Name] = doc
					}
				case *ast.ValueSpec:
					if spec.Doc != nil || len(typed.Specs) > 1 {
						doc = spec.Doc
					}
					for _, name := range spec.Names {
						if doc != nil {
							docs[name] = doc
						}
					}
				}
			}
		}
	}
	return docs
}

// resolves reports whether a file, by its name relative to the root, was read with types under some platform.
func (reading *platformReading) resolves(relative string) bool {
	if reading == nil {
		return false
	}
	_, read := reading.files[relative]
	return read
}

// counterparts is what a candidate is under platform, for a file only it builds: the candidate's own key when
// the platform builds the file declaring it, or its twin's when it is a package-level name the platform declares
// in a file of its own, or nothing when the platform has neither. A field or method declared in a file the
// platform does not build, while a file of its package that the platform builds spells its name, is refused: a
// twin type's members are not modeled.
func (reading *platformReading) counterparts(
	candidate *candidate,
	platform workspace.Platform,
	file *platformFile,
	packageLevel bool,
) (map[Key]bool, string) {
	if reading.built[platform][filepath.Clean(candidate.Position.Filename)] {
		return map[Key]bool{candidate.Key: true}, ""
	}
	if packageLevel {
		twin, found := reading.twins[platform][candidate.Key.PackagePath][candidate.OldName]
		if !found {
			return nil, ""
		}
		if twin.kind != candidate.Kind {
			return nil, fmt.Sprintf(
				"%s declares the name as a %s, not a %s, under %s",
				file.relative,
				twin.kind,
				candidate.Kind,
				platform,
			)
		}
		return map[Key]bool{twin.key: true}, ""
	}
	if file.packagePath == candidate.Key.PackagePath {
		return nil, fmt.Sprintf(
			"declared in a file %s does not build, while %s, which it does, spells the name",
			platform,
			file.relative,
		)
	}
	return nil, ""
}

// methodRefusal refuses a field or method whose type gains, under some platform, a method of the new name in
// a file only that platform builds.
func (reading *platformReading) methodRefusal(candidate *candidate) string {
	if candidate.Kind != KindField && candidate.Kind != KindEmbeddedField && candidate.Kind != KindMethod {
		return ""
	}
	owner, found := ownerKey(candidate)
	if !found {
		return ""
	}
	for _, platform := range reading.loaded {
		if reading.methods[platform][owner][candidate.NewName] {
			return fmt.Sprintf(
				"under %s, a file only it builds declares a method %s on the same type",
				platform,
				candidate.NewName,
			)
		}
	}
	return ""
}

// ownerKey is the key of the named type a field or method belongs to, when there is one.
func ownerKey(candidate *candidate) (Key, bool) {
	if method, isMethod := candidate.object.(*types.Func); isMethod && method.Signature().Recv() != nil {
		receiverType, _ := dereference(method.Signature().Recv().Type())
		if named, isNamed := types.Unalias(receiverType).(*types.Named); isNamed {
			return keyOfTypeName(candidate, named.Origin().Obj()), true
		}
		return Key{}, false
	}
	field, isField := candidate.object.(*types.Var)
	if !isField || !field.IsField() {
		return Key{}, false
	}
	scope := field.Pkg().Scope()
	for _, name := range scope.Names() {
		typeName, isTypeName := scope.Lookup(name).(*types.TypeName)
		if !isTypeName {
			continue
		}
		structType, isStruct := typeName.Type().Underlying().(*types.Struct)
		if !isStruct {
			continue
		}
		for index := range structType.NumFields() {
			if structType.Field(index) == field {
				return keyOfTypeName(candidate, typeName), true
			}
		}
	}
	return Key{}, false
}

// keyOfTypeName keys a type name by its position, as every keyer of a source load does.
func keyOfTypeName(candidate *candidate, typeName *types.TypeName) Key {
	return newKeyer(candidate.goPackage.Fset).key(typeName)
}

func sortedPlatformFiles(files map[string]*platformFile) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
