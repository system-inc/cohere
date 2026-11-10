package global_rename

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"

	"golang.org/x/tools/go/packages"
)

// Proposal asks for one object to be renamed. Package is the variant the object was read from; a local must
// come from one of the workspace's analyzed variants, since the local engine checks it there.
type Proposal struct {
	Package *packages.Package
	Object  types.Object
	NewName string
}

// Options change what the engine may do beyond renaming identifiers.
type Options struct {
	// TagEncodedFields lets a field an encoder reads by name keep its key by gaining a tag that spells the old
	// name, where it has no tag yet. Without it such a field is refused.
	TagEncodedFields bool
	// ReviewedLiterals clears string literals a person read and judged unable to name a declaration: by the
	// name the literal spells, then by the file relative to the root, the reason. Every other literal of that
	// name still refuses its renames, and a review that clears nothing is reported, so the list stays true.
	ReviewedLiterals map[string]map[string]string
	// LimitLineLength refuses a rename that would make a line newly longer than 120 columns. Off by default: a
	// pass lands as rename, then the line wrap over the touched files, and the gate judges the tree after the wrap,
	// so a refusal here would leave a name unspelled for a line the wrap settles.
	LimitLineLength bool
	// PinnedLibraries are library packages outside internal/, by path, whose exports may be renamed anyway: every
	// consumer outside the repository reads them at a pin and changes its readers when it moves the pin. The value
	// is the reason, a person's. A package a file outside the load imports is still refused.
	PinnedLibraries map[string]string
	// RenameImplementations renames, with a proposed method of a workspace interface, every workspace method that
	// implements it, under the same new name, and refuses the interface's rename when any of them is refused.
	// Without it, a type implementing the method keeps the old name and the interface's rename is refused.
	RenameImplementations bool
	// HostOnly loads no other platform. A rename with a site in the directory of a package holding a Go file the
	// host's build leaves out is refused, class "platform", rather than proven under a platform that builds the
	// file, for a caller that would sooner decline such a rename than pay a load per platform.
	HostOnly bool
	// LoadPlatform is how the plan loads the tree as a platform builds it, reading overlay in place of the disk, and
	// how ProvePlatform proves the rewrite there; nil is workspace's own LoadPlatform, through go/packages. A caller
	// with a load of its own, as the Go engine has, gives it here, so the platform's proof reads the load its host
	// proof reads.
	LoadPlatform func(platform workspace.Platform, overlay map[string][]byte) (*workspace.PlatformLoad, error)
	// RequireSoundPlatforms refuses a rename that reaches a package holding a file only some platform builds when that
	// platform's load fails, or the package does not type-check under it, with a reason naming the platform: a rename
	// there is proven under the platform's types or not at all. Without it, such files are read as text and the
	// textual guard answers for them (see outsideRefusal).
	RequireSoundPlatforms bool
}

// Rename is one declaration and the name it is asked to take.
type Rename struct {
	Key      Key
	Kind     string
	OldName  string
	NewName  string
	Position token.Position
}

// Rejection is a rename the engine refused, with the reason's group for counting and the reason itself.
type Rejection struct {
	Rename
	Class  string
	Reason string

	// File is the file that may not be edited, for a rejection of class "file": the declaration's or a use's, so a
	// caller can tell why that file is not its to write. Empty for every other class.
	File string
}

// EncodedField is a proposed field an encoder or reflection can read, and what the guard decided.
type EncodedField struct {
	Rename
	Encoders []string
	Tag      string
	Outcome  string
}

// candidate is one proposed rename, with what the engine knows about its declaration.
type candidate struct {
	Rename
	object    types.Object
	goPackage *packages.Package
	local     bool
	impliedBy *Key
}

// site is one identifier that names a candidate.
type site struct {
	filename string
	offset   int
	editable bool
	key      Key
}

type engine struct {
	workspace *workspace.Workspace
	options   Options
	keyer     *keyer
	roots     []*packages.Package
	// workspacePaths holds every root package's path.
	workspacePaths map[string]bool
	editableFiles  map[string]bool
	// variantsOfFile holds every root package variant that compiles each file.
	variantsOfFile map[string][]*packages.Package
	candidates     map[Key]*candidate
	accepted       map[Key]string
	// implied maps a type to the embedded fields its rename renames with it, and impliedBy back.
	implied map[Key][]Key
	sites   map[Key][]site
	result  *Result
	// encoded is the encoding guard's analysis, kept for the report.
	encoded *encodingAnalysis
	// templateRunners holds each package variant's answer to runsTemplates, once asked.
	templateRunners map[*packages.Package]bool
	// importedPaths holds, for each package variant asked about, the paths it is or imports, directly or not.
	importedPaths map[*packages.Package]map[string]bool
	// sourceReadingTests holds every test file that reads Go source, whose literals may name any declaration.
	sourceReadingTests map[string]bool
	// usedReviews holds each reviewed literal, name and file, that cleared a site.
	usedReviews map[string]bool
	// twinEdits rename, with a candidate, its platform twins' declarations in files the load does not check.
	twinEdits map[Key][]rename.Edit
	// tagEdits add, with an encoded field's rename, the tag that keeps its key; a refused rename adds none.
	tagEdits map[Key][]rename.Edit
	// platforms is what the files only another platform builds name, read with types under that platform.
	platforms *platformReading
	// unbuilt is, under HostOnly, each directory of a root package holding a Go file the host does not build, with
	// the first such file, relative to the root.
	unbuilt map[string]string
	// methodLookup is computedMethodLookup's answer, nil until asked.
	methodLookup *string
	// unbuiltFiles are, by directory, the Go files there that the host does not build and a platform does, and
	// platformFailures what kept a platform the plan loaded from proving anything in a directory.
	unbuiltFiles     map[string][]unbuiltFile
	platformFailures map[workspace.Platform]platformFailure
}

// Plan verifies proposals against the loaded workspace and returns what survived and why the rest did not.
func Plan(loaded *workspace.Workspace, proposals []Proposal, options Options) *Result {
	engine := &engine{
		workspace:       loaded,
		options:         options,
		roots:           loaded.Roots,
		workspacePaths:  map[string]bool{},
		editableFiles:   map[string]bool{},
		variantsOfFile:  map[string][]*packages.Package{},
		candidates:      map[Key]*candidate{},
		accepted:        map[Key]string{},
		implied:         map[Key][]Key{},
		sites:           map[Key][]site{},
		result:          &Result{},
		usedReviews:     map[string]bool{},
		twinEdits:       map[Key][]rename.Edit{},
		tagEdits:        map[Key][]rename.Edit{},
		templateRunners: map[*packages.Package]bool{},
		importedPaths:   map[*packages.Package]map[string]bool{},
	}
	if len(loaded.Roots) == 0 {
		return engine.result
	}
	engine.keyer = newKeyer(loaded.Roots[0].Fset)
	engine.platforms = newPlatformReading()
	for _, rootsPackage := range engine.roots {
		engine.workspacePaths[rootsPackage.PkgPath] = true
		for _, file := range rootsPackage.Syntax {
			filename := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			engine.editableFiles[filename] = loaded.Editable(rootsPackage, file)
			engine.variantsOfFile[filename] = append(engine.variantsOfFile[filename], rootsPackage)
		}
	}
	engine.register(proposals)
	engine.addImpliedImplementations()
	engine.addImpliedEmbeddedFields()
	engine.indexSites()
	// The platforms are read once the sites are known, so only a platform whose files a candidate reaches is loaded.
	if options.HostOnly {
		engine.unbuilt = engine.unbuiltDirectories()
	} else {
		engine.platforms = engine.readPlatforms(engine.reachedDirectories())
	}
	engine.result.platforms = engine.platforms.loaded
	engine.refuseUpfront()
	engine.checkLocals()
	engine.verify()
	for engine.options.LimitLineLength && engine.refuseLongLines() {
		engine.verify()
	}
	engine.collectEdits()
	engine.noteUnusedReviews()
	engine.finish()
	return engine.result
}

// register turns proposals into candidates, refusing a declaration proposed under two names.
func (engine *engine) register(proposals []Proposal) {
	analyzed := map[*packages.Package]bool{}
	for _, analyzedPackage := range engine.workspace.Analyzed {
		analyzed[analyzedPackage] = true
	}
	conflicting := map[Key]bool{}
	for _, proposal := range proposals {
		key := engine.keyer.key(proposal.Object)
		declared := origin(proposal.Object)
		local := rename.IsLocal(declared, proposal.Package.Types)
		kind := kindOf(declared)
		if local {
			kind = KindLocal
		}
		if existing, found := engine.candidates[key]; found {
			if existing.NewName != proposal.NewName {
				conflicting[key] = true
			}
			continue
		}
		candidate := &candidate{
			Rename: Rename{
				Key: key, Kind: kind, OldName: declared.Name(), NewName: proposal.NewName,
				Position: proposal.Package.Fset.PositionFor(declared.Pos(), false),
			},
			object:    declared,
			goPackage: proposal.Package,
			local:     local,
		}
		engine.candidates[key] = candidate
		engine.accepted[key] = proposal.NewName
		if local && !analyzed[proposal.Package] {
			engine.reject(key, "proposal", "a local proposed from a package variant that is not analyzed")
		}
	}
	for key := range conflicting {
		engine.reject(key, "proposal", "two different new names proposed")
	}
}

// addImpliedEmbeddedFields makes every embedded field named by a proposed type a candidate of its own: the
// field's name is the type's, so renaming the type renames the field and every selector that names it.
func (engine *engine) addImpliedEmbeddedFields() {
	for _, rootsPackage := range engine.roots {
		for _, file := range rootsPackage.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				structType, isStruct := node.(*ast.StructType)
				if !isStruct {
					return true
				}
				for _, field := range structType.Fields.List {
					if len(field.Names) != 0 {
						continue
					}
					identifier := embeddedTypeIdentifier(field.Type)
					if identifier == nil {
						continue
					}
					typeName, isTypeName := rootsPackage.TypesInfo.Uses[identifier].(*types.TypeName)
					fieldVariable, isVariable := rootsPackage.TypesInfo.Defs[identifier].(*types.Var)
					if !isTypeName || !isVariable {
						continue
					}
					typeKey := engine.keyer.key(typeName)
					typeCandidate, proposed := engine.candidates[typeKey]
					if !proposed || typeCandidate.impliedBy != nil {
						continue
					}
					engine.addImplied(typeKey, fieldVariable, rootsPackage, KindEmbeddedField)
				}
				return true
			})
		}
	}
}

// addImpliedImplementations makes every workspace method that implements a proposed method of a named interface
// a candidate of its own, under the same new name, when Options.RenameImplementations asks: a type satisfying the
// interface through a method it declares, or through one it promotes from a type it embeds, must rename that
// method with the interface's, or it stops satisfying the interface, or satisfies it through an embedded
// interface's method in place of its own override. Each root package's own package-level types are searched
// against the interface as that package sees it.
func (engine *engine) addImpliedImplementations() {
	if !engine.options.RenameImplementations {
		return
	}
	for _, interfaceKey := range engine.sortedCandidateKeys() {
		interfaceMethod := engine.candidates[interfaceKey]
		function, isFunction := interfaceMethod.object.(*types.Func)
		if !isFunction || interfaceMethod.impliedBy != nil || function.Signature().Recv() == nil {
			continue
		}
		receiver, isNamed := types.Unalias(function.Signature().Recv().Type()).(*types.Named)
		if !isNamed || !types.IsInterface(receiver) || receiver.Obj().Parent() != receiver.Obj().Pkg().Scope() {
			continue
		}
		for _, rootsPackage := range engine.roots {
			var visible *types.Package
			for _, imported := range importClosure(rootsPackage.Types) {
				if imported.Path() == receiver.Obj().Pkg().Path() {
					visible = imported
				}
			}
			if visible == nil {
				continue
			}
			seen, isTypeName := visible.Scope().Lookup(receiver.Obj().Name()).(*types.TypeName)
			if !isTypeName {
				continue
			}
			interfaceType, isInterface := seen.Type().Underlying().(*types.Interface)
			if !isInterface {
				continue
			}
			scope := rootsPackage.Types.Scope()
			for _, name := range scope.Names() {
				typeName, isTypeName := scope.Lookup(name).(*types.TypeName)
				if !isTypeName || typeName.IsAlias() || types.IsInterface(typeName.Type()) {
					continue
				}
				for _, implementer := range []types.Type{typeName.Type(), types.NewPointer(typeName.Type())} {
					if !types.Implements(implementer, interfaceType) {
						continue
					}
					object, _, _ := types.LookupFieldOrMethod(implementer, false, function.Pkg(), function.Name())
					implementation, isMethod := object.(*types.Func)
					if !isMethod || types.IsInterface(implementation.Signature().Recv().Type()) ||
						implementation.Pkg() == nil || !engine.workspacePaths[implementation.Pkg().Path()] {
						continue
					}
					engine.addImplied(interfaceKey, implementation, rootsPackage, KindMethod)
					break
				}
			}
		}
	}
}

// addImplied makes object a candidate renamed with the candidate at impliedByKey, under its new name, unless it
// is a candidate already.
func (engine *engine) addImplied(impliedByKey Key, object types.Object, goPackage *packages.Package, kind string) {
	key := engine.keyer.key(object)
	if _, known := engine.candidates[key]; known {
		return
	}
	impliedBy := impliedByKey
	declared := origin(object)
	engine.candidates[key] = &candidate{
		Rename: Rename{
			Key: key, Kind: kind, OldName: declared.Name(), NewName: engine.candidates[impliedByKey].NewName,
			Position: goPackage.Fset.PositionFor(declared.Pos(), false),
		},
		object:    declared,
		goPackage: goPackage,
		impliedBy: &impliedBy,
	}
	engine.implied[impliedByKey] = append(engine.implied[impliedByKey], key)
	if _, open := engine.accepted[impliedByKey]; open {
		engine.accepted[key] = engine.candidates[impliedByKey].NewName
	}
}

// embeddedTypeIdentifier is the identifier that names an embedded field's type: T, *T, p.T, T[A].
func embeddedTypeIdentifier(expression ast.Expr) *ast.Ident {
	current := expression
	for {
		switch typed := current.(type) {
		case *ast.Ident:
			return typed
		case *ast.StarExpr:
			current = typed.X
		case *ast.SelectorExpr:
			return typed.Sel
		case *ast.IndexExpr:
			current = typed.X
		case *ast.IndexListExpr:
			current = typed.X
		case *ast.ParenExpr:
			current = typed.X
		default:
			return nil
		}
	}
}

// indexSites records every defining and using identifier of every candidate, across all root packages.
func (engine *engine) indexSites() {
	seen := map[rename.EditKey]map[Key]bool{}
	add := func(goPackage *packages.Package, identifier *ast.Ident, object types.Object) {
		if object == nil {
			return
		}
		key := engine.keyer.key(object)
		if _, proposed := engine.candidates[key]; !proposed {
			return
		}
		position := goPackage.Fset.PositionFor(identifier.Pos(), false)
		editKey := rename.EditKey{Filename: position.Filename, Offset: position.Offset}
		if seen[editKey] == nil {
			seen[editKey] = map[Key]bool{}
		}
		if seen[editKey][key] {
			return
		}
		seen[editKey][key] = true
		editable, known := engine.editableFiles[position.Filename]
		engine.sites[key] = append(
			engine.sites[key],
			site{filename: position.Filename, offset: position.Offset, editable: known && editable, key: key},
		)
	}
	for _, rootsPackage := range engine.roots {
		for identifier, object := range rootsPackage.TypesInfo.Defs {
			add(rootsPackage, identifier, object)
		}
		for identifier, object := range rootsPackage.TypesInfo.Uses {
			add(rootsPackage, identifier, object)
		}
	}
}

// reject refuses a candidate with a reason, and everything that must move with it: a type and the embedded
// fields it names.
func (engine *engine) reject(key Key, class string, reason string) {
	engine.rejectFor(key, class, "", reason)
}

// rejectFor is reject naming file, the file that may not be edited, on the rejection and on every one it implies.
func (engine *engine) rejectFor(key Key, class string, file string, reason string) {
	candidate, known := engine.candidates[key]
	if !known {
		return
	}
	if _, open := engine.accepted[key]; !open {
		return
	}
	delete(engine.accepted, key)
	engine.result.Rejections = append(
		engine.result.Rejections,
		Rejection{Rename: candidate.Rename, Class: class, Reason: reason, File: file},
	)
	if candidate.impliedBy != nil {
		relation := "embedded as the field"
		if candidate.Kind == KindMethod {
			relation = "implemented by the method"
		}
		engine.rejectFor(
			*candidate.impliedBy,
			class,
			file,
			fmt.Sprintf("%s at %s, which was refused: %s", relation, candidate.Position, reason),
		)
	}
	for _, fieldKey := range engine.implied[key] {
		delete(engine.accepted, fieldKey)
	}
}

// finalName is the name object has once every accepted rename is applied.
func (engine *engine) finalName(object types.Object) string {
	if newName, renamed := engine.accepted[engine.keyer.key(object)]; renamed {
		return newName
	}
	return object.Name()
}

func actualName(object types.Object) string { return object.Name() }

// renamed reports whether object is accepted for a rename.
func (engine *engine) renamed(object types.Object) bool {
	_, renamed := engine.accepted[engine.keyer.key(object)]
	return renamed
}

// checkLocals runs the local engine over each analyzed variant's local proposals. What it refuses is refused;
// what it accepts goes on to the whole-workspace verification with everything else.
func (engine *engine) checkLocals() {
	byPackage := map[*packages.Package][]rename.Proposal{}
	for key, candidate := range engine.candidates {
		if !candidate.local {
			continue
		}
		if _, open := engine.accepted[key]; !open {
			continue
		}
		byPackage[candidate.goPackage] = append(
			byPackage[candidate.goPackage],
			rename.Proposal{Object: candidate.object, NewName: candidate.NewName},
		)
	}
	for goPackage, proposals := range byPackage {
		plan := rename.Check(engine.workspace.RenamePackage(goPackage), proposals)
		for _, rejection := range plan.Rejections {
			class := rejection.Reason
			if strings.HasPrefix(class, "conflict:") {
				class = "local engine: conflict"
			} else {
				class = "local engine: " + class
			}
			engine.reject(engine.keyer.key(rejection.Object), class, rejection.Reason)
		}
		for _, unresolved := range plan.Unresolved {
			engine.result.Unresolved = append(engine.result.Unresolved, goPackage.ID+": "+unresolved)
		}
	}
}

// verify runs every whole-workspace check to a fixpoint: each pass refuses every rename found in a conflict,
// and the next pass checks the smaller set from scratch, until a pass finds none.
func (engine *engine) verify() {
	checks := newVerification(engine)
	for {
		drops := checks.pass()
		if len(drops) == 0 {
			return
		}
		for _, key := range sortedDropKeys(drops) {
			engine.reject(key, drops[key].class, drops[key].reason)
		}
	}
}

// drop is a conflict a check found against one rename.
type drop struct {
	class  string
	reason string
}

func sortedDropKeys(drops map[Key]drop) []Key {
	keys := make([]Key, 0, len(drops))
	for key := range drops {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left].String() < keys[right].String() })
	return keys
}

// noteUnusedReviews reports each reviewed literal that cleared no site of a proposed name: the tree moved under
// the review, or the review names the wrong file.
func (engine *engine) noteUnusedReviews() {
	proposedNames := map[string]bool{}
	for _, candidate := range engine.candidates {
		proposedNames[candidate.OldName] = true
	}
	for name, files := range engine.options.ReviewedLiterals {
		if !proposedNames[name] {
			continue
		}
		for file := range files {
			if !engine.usedReviews[name+"\x00"+file] {
				engine.result.Notes = append(
					engine.result.Notes,
					fmt.Sprintf("reviewed literal %q in %s cleared nothing", name, file),
				)
			}
		}
	}
	sort.Strings(engine.result.Notes)
}

// finish sorts the result for reading.
func (engine *engine) finish() {
	for key, newName := range engine.accepted {
		candidate := engine.candidates[key]
		renamed := candidate.Rename
		renamed.NewName = newName
		engine.result.Accepted = append(engine.result.Accepted, renamed)
	}
	sort.Slice(engine.result.Accepted, func(left, right int) bool {
		return positionLess(engine.result.Accepted[left].Position, engine.result.Accepted[right].Position)
	})
	sort.Slice(engine.result.Rejections, func(left, right int) bool {
		return positionLess(engine.result.Rejections[left].Position, engine.result.Rejections[right].Position)
	})
	sort.Slice(engine.result.EncodedFields, func(left, right int) bool {
		return positionLess(engine.result.EncodedFields[left].Position, engine.result.EncodedFields[right].Position)
	})
	sort.Strings(engine.result.Unresolved)
	sort.Strings(engine.result.SelectionModelMismatches)
	sort.Strings(engine.result.SatisfactionModelMismatches)
	sort.Strings(engine.result.OpaqueEncoderArguments)
}

func positionLess(left, right token.Position) bool {
	if left.Filename != right.Filename {
		return left.Filename < right.Filename
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	return left.Column < right.Column
}

func (engine *engine) analyzeEncoding() *encodingAnalysis {
	analysis := &encodingAnalysis{
		engine:              engine,
		fields:              map[Key]encoder{},
		types:               map[Key]encoder{},
		parameters:          map[Key]encoder{},
		typeParameters:      map[Key]encoder{},
		methodSinks:         map[string]map[int]encoder{},
		parameterKeys:       map[Key]bool{},
		methodParameters:    map[Key]methodParameter{},
		assignments:         map[Key][]valueSite{},
		fieldAssignments:    map[Key][]valueSite{},
		returns:             map[Key]map[int][]valueSite{},
		literalReturns:      map[*ast.FuncLit]map[int][]valueSite{},
		functionLiterals:    map[Key]*ast.FuncLit{},
		typeParametersOf:    map[Key][]Key{},
		receiverTypeAliases: map[Key]Key{},
		visitedTypes:        map[types.Type]encoder{},
		visitedExpressions:  map[siteKey]encoder{},
		visitedKeys:         map[valueKey]encoder{},
		opaque:              map[string]bool{},
		reflectNameReaders:  map[*packages.Package]bool{},
	}
	analysis.typeNames = newTypeNameFlow(analysis)
	for _, analyzedPackage := range engine.workspace.Analyzed {
		analysis.index(analyzedPackage)
	}
	for {
		before := analysis.sinkCount()
		for _, call := range analysis.calls {
			analysis.visitCall(call)
		}
		for _, instance := range analysis.instances {
			analysis.visitInstance(instance)
		}
		for key, set := range analysis.parameters {
			if parameter, isMethod := analysis.methodParameters[key]; isMethod {
				if analysis.methodSinks[parameter.name] == nil {
					analysis.methodSinks[parameter.name] = map[int]encoder{}
				}
				analysis.methodSinks[parameter.name][parameter.index] |= set
			}
		}
		if analysis.sinkCount() == before {
			break
		}
	}
	for _, value := range analysis.functionValues {
		analysis.checkFunctionValue(value)
	}
	for description := range analysis.opaque {
		engine.result.OpaqueEncoderArguments = append(engine.result.OpaqueEncoderArguments, description)
	}
	readers := 0
	for _, reads := range analysis.reflectNameReaders {
		if reads {
			readers++
		}
	}
	engine.result.Notes = append(engine.result.Notes, fmt.Sprintf(
		"package variants calling reflect: %d, of them reading Go names through it or handing its values on: %d",
		len(analysis.reflectNameReaders), readers))
	return analysis
}

// fieldDeclarations finds the declaration of every field candidate.
func (engine *engine) fieldDeclarations() map[Key]fieldDeclaration {
	declarations := map[Key]fieldDeclaration{}
	for _, rootsPackage := range engine.roots {
		for _, file := range rootsPackage.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				structType, isStruct := node.(*ast.StructType)
				if !isStruct {
					return true
				}
				for _, field := range structType.Fields.List {
					identifiers := field.Names
					if len(identifiers) == 0 {
						if embedded := embeddedTypeIdentifier(field.Type); embedded != nil {
							identifiers = []*ast.Ident{embedded}
						}
					}
					for _, identifier := range identifiers {
						variable, isVariable := rootsPackage.TypesInfo.Defs[identifier].(*types.Var)
						if !isVariable {
							continue
						}
						key := engine.keyer.key(variable)
						if _, proposed := engine.candidates[key]; proposed {
							if _, recorded := declarations[key]; !recorded {
								declarations[key] = fieldDeclaration{
									goPackage: rootsPackage,
									field:     field,
									structure: structType,
								}
							}
						}
					}
				}
				return true
			})
		}
	}
	return declarations
}

// refuseEncoded applies the encoding guard: a field an encoder or reflection reads by name keeps its name
// unless a tag already names its key, or, with TagEncodedFields, gains one that spells the old name. A type
// keeps its name only where something reads a type's name (typeNameReaders): json and gob read a struct's field
// names and never its type's, and a reflect value that only walks a type's fields reads none. A method keeps its
// name where a template or a computed reflect lookup calls it by name (methodReaderRefusal).
func (engine *engine) refuseEncoded() {
	declarations := engine.fieldDeclarations()
	for _, key := range engine.sortedCandidateKeys() {
		candidate := engine.candidates[key]
		if _, open := engine.accepted[key]; !open {
			continue
		}
		switch candidate.Kind {
		case KindType:
			set := engine.encoded.types[key] & typeNameReaders
			if set != 0 {
				engine.reject(
					key,
					"encoding guard",
					fmt.Sprintf("the type's name is read at run time by %s", strings.Join(set.names(), ", ")),
				)
			}
			continue
		case KindMethod:
			if reason := engine.methodReaderRefusal(candidate); reason != "" {
				engine.reject(key, "encoding guard", reason)
			}
			continue
		case KindField, KindEmbeddedField:
		default:
			continue
		}
		set := engine.encoded.fields[key] &^ typeNameFamilies
		if set == 0 {
			continue
		}
		declaration, found := declarations[key]
		record := EncodedField{Rename: candidate.Rename, Encoders: set.names()}
		if !found {
			record.Outcome = "refused: declaration not found"
			engine.result.EncodedFields = append(engine.result.EncodedFields, record)
			engine.reject(key, "encoding guard", "an encoder reaches the field and its declaration was not found")
			continue
		}
		tag := tagOf(declaration.field)
		record.Tag = string(tag)
		if !token.IsExported(candidate.OldName) {
			// Every encoder but reflection skips an unexported field.
			set &= encoderReflect
			if set == 0 {
				record.Outcome = "kept: unexported, encoders skip it"
				engine.result.EncodedFields = append(engine.result.EncodedFields, record)
				continue
			}
		}
		if candidate.Kind == KindEmbeddedField && set&^(encoderJson|encoderSlog) == 0 && !namesKey(tag, "json") {
			embeddedType, _ := dereference(candidate.object.Type())
			if _, isStruct := embeddedType.Underlying().(*types.Struct); isStruct {
				record.Outcome = "kept: an embedded struct, which json flattens"
				engine.result.EncodedFields = append(engine.result.EncodedFields, record)
				continue
			}
		}
		var unmet []string
		unfixable := false
		for _, entry := range encoderNames {
			if set&entry.encoder == 0 {
				continue
			}
			if entry.tag == "" {
				unfixable = true
				unmet = append(unmet, entry.name)
				continue
			}
			if !namesKey(tag, entry.tag) {
				unmet = append(unmet, entry.name)
			}
		}
		switch {
		case len(unmet) == 0:
			record.Outcome = "kept: its tag names the key"
		case unfixable:
			record.Outcome = "refused: " + strings.Join(unmet, ", ") + " read the Go name, which no tag changes"
			engine.reject(
				key,
				"encoding guard",
				"reached by "+strings.Join(set.names(), ", ")+"; "+strings.Join(unmet, ", ")+" read the Go name",
			)
		case !engine.options.TagEncodedFields:
			record.Outcome = "refused: no tag names its key"
			engine.reject(
				key,
				"encoding guard",
				"reached by "+strings.Join(set.names(), ", ")+" with no tag naming its key",
			)
		case declaration.field.Tag != nil:
			record.Outcome = "refused: it has a tag without that key, which is edited by hand"
			engine.reject(
				key,
				"encoding guard",
				"reached by "+strings.Join(set.names(), ", ")+"; it has a tag without that key, edit by hand",
			)
		case candidate.Kind == KindEmbeddedField || len(declaration.field.Names) != 1:
			record.Outcome = "refused: a tag here would name more than this field, or an embedded one"
			engine.reject(
				key,
				"encoding guard",
				"reached by "+strings.Join(set.names(), ", ")+"; a tag cannot be added to this declaration alone",
			)
		default:
			tagText := engine.keepingTag(candidate.OldName, set, declaration.structure)
			position := declaration.goPackage.Fset.PositionFor(declaration.field.Type.End(), false)
			engine.tagEdits[key] = append(engine.tagEdits[key], rename.Edit{
				Filename: position.Filename, Offset: position.Offset, OldText: "", NewText: " `" + tagText + "`",
			})
			record.Outcome = "tag added: `" + tagText + "`"
		}
		engine.result.EncodedFields = append(engine.result.EncodedFields, record)
	}
}

// keepingTag is the tag that keeps an encoded field's key: json, and the same for every other key-naming
// encoder that reaches it or that its sibling fields already tag.
func (engine *engine) keepingTag(oldName string, set encoder, structure *ast.StructType) string {
	wanted := map[string]bool{"json": true}
	for _, entry := range encoderNames {
		if set&entry.encoder != 0 && entry.tag != "" {
			wanted[entry.tag] = true
		}
	}
	for _, sibling := range structure.Fields.List {
		siblingTag := tagOf(sibling)
		for _, name := range []string{"xml", "yaml", "toml"} {
			if _, present := siblingTag.Lookup(name); present {
				wanted[name] = true
			}
		}
	}
	var parts []string
	for _, name := range []string{"json", "xml", "yaml", "toml"} {
		if !wanted[name] {
			continue
		}
		key := oldName
		if name == "yaml" {
			// yaml's default key is the field name in lower case.
			key = strings.ToLower(oldName)
		}
		parts = append(parts, fmt.Sprintf("%s:%q", name, key))
	}
	return strings.Join(parts, " ")
}

// readPlatforms loads the tree under each platform that the build-constrained files of a reached directory need, and
// reads the files only those loads build. A platform no reached directory needs is never loaded, so a plan whose
// renames reach no such package costs what a host-only plan does. It notes every constrained file no platform builds
// and every package a platform cannot type-check, since the textual guard still answers for their files, and keeps
// what failed (platformFailures), which refuses the renames that reach it, naming the platform.
func (engine *engine) readPlatforms(reached map[string]map[Key]bool) *platformReading {
	reading := newPlatformReading()
	engine.unbuiltFiles = map[string][]unbuiltFile{}
	engine.platformFailures = map[workspace.Platform]platformFailure{}
	engine.result.platformKeys = map[workspace.Platform]map[Key]bool{}
	engine.result.platformDifferences = map[workspace.Platform]platformDifference{}
	hostBuilt := map[string]bool{}
	for _, rootsPackage := range engine.roots {
		for _, list := range [][]string{rootsPackage.GoFiles, rootsPackage.CompiledGoFiles, rootsPackage.OtherFiles} {
			for _, filename := range list {
				hostBuilt[filepath.Clean(filename)] = true
			}
		}
	}
	needed := map[workspace.Platform]bool{}
	unbuilt := map[string]bool{}
	seen := map[string]bool{}
	for _, rootsPackage := range engine.roots {
		for _, filename := range rootsPackage.IgnoredFiles {
			filename = filepath.Clean(filename)
			relative, inside := engine.workspace.Relative(filename)
			if !inside || !strings.HasSuffix(filename, ".go") || hostBuilt[filename] || isTypeScript(relative) ||
				seen[filename] {
				continue
			}
			seen[filename] = true
			directory := filepath.Dir(filename)
			if len(reached[directory]) == 0 {
				continue
			}
			platform, builds := workspace.PlatformFor(filename)
			if !builds {
				unbuilt[relative] = true
				continue
			}
			needed[platform] = true
			engine.unbuiltFiles[directory] = append(engine.unbuiltFiles[directory],
				unbuiltFile{relative: relative, platform: platform})
			if engine.result.platformKeys[platform] == nil {
				engine.result.platformKeys[platform] = map[Key]bool{}
			}
			for key := range reached[directory] {
				engine.result.platformKeys[platform][key] = true
			}
		}
	}
	sortedUnbuilt(engine.unbuiltFiles)
	for _, relative := range sortedNames(unbuilt) {
		engine.result.Notes = append(
			engine.result.Notes,
			"build-constrained, built by no platform, so read as text: "+relative,
		)
	}
	for _, platform := range workspace.Platforms {
		if !needed[platform] {
			continue
		}
		load, loadError := engine.loadPlatform(platform, nil)
		if loadError != nil {
			engine.platformFailures[platform] = platformFailure{load: loadError.Error()}
			engine.result.Notes = append(
				engine.result.Notes,
				fmt.Sprintf("platform %s: %v; its files are read as text", platform, loadError),
			)
			continue
		}
		reading.loaded = append(reading.loaded, platform)
		reading.read(engine, load, hostBuilt)
		// The proof's load before the rewrite is this one, so its fingerprint is taken while the plan holds it, and
		// nothing need keep the load for the proof. Only its difference from the host's is kept, and the host's
		// fingerprint, taken once for every platform, is kept for Expect.
		if engine.result.hostFingerprint == nil {
			host := fingerprintBefore(engine.workspace)
			engine.result.hostFingerprint = &host
			engine.result.planned = engine.workspace
		}
		engine.result.platformDifferences[platform] = differenceFrom(
			*engine.result.hostFingerprint,
			fingerprintBefore(load.Clean),
		)
		failure := platformFailure{packages: map[string]string{}}
		for id, reason := range load.Failed {
			if directory := engine.packageDirectory(id); directory != "" {
				failure.packages[directory] = reason
			}
		}
		if len(failure.packages) > 0 {
			engine.platformFailures[platform] = failure
		}
		failed := load.FailedSummary()
		engine.result.Notes = append(
			engine.result.Notes,
			fmt.Sprintf("platform %s: %d packages type-check, %d do not", platform, len(load.Clean.Roots), len(failed)),
		)
		for _, line := range failed {
			engine.result.Notes = append(engine.result.Notes, fmt.Sprintf(
				"platform %s, not type-checked, its files read as text: %s",
				platform,
				engine.relativePosition(line),
			))
		}
	}
	return reading
}

// platformRefusal renames a candidate in every file only another platform builds, by what each identifier there
// resolves to, and is why it cannot be, or "". An identifier renames with the candidate when it names the
// candidate's own declaration, in a file both builds share, or the candidate's platform twin, a package-level
// declaration of the same name and kind in a file only that platform builds. Any other spelling of the name
// names something else and stays. A package-level candidate is refused when such a file of its package, or one it
// would edit, spells the new name; a field or method when the platform declares a method of the new name on its
// type. Whatever this cannot see, the rewrite's verification under every platform's load catches before anything
// is written.
func (engine *engine) platformRefusal(candidate *candidate) string {
	reading := engine.platforms
	if reading == nil || len(reading.files) == 0 {
		return ""
	}
	packageLevel := candidate.Kind == KindFunc ||
		candidate.Kind == KindVariable ||
		candidate.Kind == KindConst ||
		candidate.Kind == KindType
	packageLevel = packageLevel && candidate.object.Parent() == candidate.object.Pkg().Scope()
	if refusal := reading.methodRefusal(candidate); refusal != "" {
		return refusal
	}
	verdicts := map[rename.EditKey]bool{}
	var edits []rename.Edit
	for _, relative := range sortedPlatformFiles(reading.files) {
		file := reading.files[relative]
		samePackage := file.packagePath == candidate.Key.PackagePath
		if file.spellings[candidate.OldName] == 0 {
			if packageLevel && samePackage && file.spellings[candidate.NewName] > 0 {
				return fmt.Sprintf(
					"the new name appears in %s, a file of its own package that only %s builds",
					relative,
					file.platformNames(),
				)
			}
			continue
		}
		sitesHere := false
		for _, fileReading := range file.readings {
			counterparts, refusal := reading.counterparts(candidate, fileReading.platform, file, packageLevel)
			if refusal != "" {
				return refusal
			}
			for _, identifier := range fileReading.identifiers {
				if identifier.name != candidate.OldName {
					continue
				}
				ours := identifier.resolved && counterparts[identifier.key]
				position := rename.EditKey{Filename: file.filename, Offset: identifier.offset}
				if previous, seen := verdicts[position]; seen && previous != ours {
					return fmt.Sprintf(
						"the name at offset %d of %s resolves to it under one platform and not under another",
						identifier.offset,
						relative,
					)
				}
				if _, seen := verdicts[position]; seen {
					continue
				}
				verdicts[position] = ours
				if !ours {
					continue
				}
				sitesHere = true
				edits = append(edits, rename.Edit{
					Filename: file.filename,
					Offset:   identifier.offset,
					OldText:  candidate.OldName,
					NewText:  candidate.NewName,
				})
				if identifier.doc == nil {
					continue
				}
				for index, comment := range identifier.doc.List {
					for _, offset := range commentIndexes(comment.Text, candidate.OldName) {
						edits = append(edits, rename.Edit{
							Filename: file.filename,
							Offset:   identifier.docOffsets[index] + offset,
							OldText:  candidate.OldName,
							NewText:  candidate.NewName,
						})
					}
				}
			}
		}
		if !sitesHere {
			if packageLevel && samePackage && file.spellings[candidate.NewName] > 0 {
				return fmt.Sprintf(
					"the new name appears in %s, a file of its own package that only %s builds",
					relative,
					file.platformNames(),
				)
			}
			continue
		}
		if !file.editable {
			return "used in a file that may not be edited: " + relative
		}
		if packageLevel && file.spellings[candidate.NewName] > 0 {
			return fmt.Sprintf(
				"the new name appears in %s, which only %s builds and the rename would edit",
				relative,
				file.platformNames(),
			)
		}
	}
	if len(edits) > 0 {
		kept, _ := rename.Deduplicate(edits)
		engine.twinEdits[candidate.Key] = append(engine.twinEdits[candidate.Key], kept...)
	}
	return ""
}

// localInterfaces lists every interface declared inside a function or spelled as a literal, in every root
// package, that wanted keeps.
func (engine *engine) localInterfaces(wanted func(*types.Interface) bool) []interfaceEntry {
	var entries []interfaceEntry
	seen := map[string]bool{}
	for _, rootsPackage := range engine.roots {
		for identifier, object := range rootsPackage.TypesInfo.Defs {
			typeName, isTypeName := object.(*types.TypeName)
			if !isTypeName ||
				typeName.IsAlias() ||
				typeName.Parent() == rootsPackage.Types.Scope() ||
				typeName.Parent() == nil {
				continue
			}
			interfaceType, isInterface := typeName.Type().Underlying().(*types.Interface)
			if !isInterface || !wanted(interfaceType) {
				continue
			}
			identity := engine.keyer.key(typeName).String() + " in " + rootsPackage.ID
			if !seen[identity] {
				seen[identity] = true
				entries = append(entries, interfaceEntry{
					identity:      identity,
					name:          identifier.Name,
					interfaceType: interfaceType,
					home:          rootsPackage.PkgPath,
				})
			}
		}
		for expression, value := range rootsPackage.TypesInfo.Types {
			interfaceType, isInterface := value.Type.(*types.Interface)
			if !value.IsType() || !isInterface || !wanted(interfaceType) {
				continue
			}
			position := rootsPackage.Fset.PositionFor(expression.Pos(), false)
			identity := fmt.Sprintf(
				"literal %s:%d:%d in %s",
				position.Filename,
				position.Line,
				position.Column,
				rootsPackage.ID,
			)
			if !seen[identity] {
				seen[identity] = true
				entries = append(entries, interfaceEntry{
					identity:      identity,
					name:          "the interface literal at " + position.String(),
					interfaceType: interfaceType,
					home:          rootsPackage.PkgPath,
				})
			}
		}
	}
	return entries
}
