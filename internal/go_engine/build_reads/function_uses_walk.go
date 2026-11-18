package build_reads

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
)

// functionUsesWalk is one package's walk of its declarations' parts: the package, its type information, the range of
// the declaration being walked, which bounds its own objects, the uses of the part being walked, emptied for each
// part, and visit, visitNode made once, so walking a part makes no function value of its own.
type functionUsesWalk struct {
	own   *types.Package
	info  *types.Info
	start token.Pos
	end   token.Pos
	uses  map[partUseKey]UseKinds
	visit func(ast.Node) bool
}

// collectFunctionUses is what each top-level declaration of files, own's, uses by info, own's type information, and
// whether it could be told: without the syntax or any of the maps a use is read from, a use could go unseen, so
// nothing is recorded, which is unknown, never empty.
func collectFunctionUses(own *types.Package, info *types.Info, files []*ast.File) ([]DeclarationUses, bool) {
	if len(files) == 0 || info.Types == nil || info.Uses == nil || info.Defs == nil || info.Selections == nil ||
		info.Instances == nil || info.Implicits == nil {
		return nil, false
	}
	walk := &functionUsesWalk{own: own, info: info, uses: map[partUseKey]UseKinds{}}
	walk.visit = walk.visitNode
	var collected []DeclarationUses
	for _, file := range files {
		ordinal := 0
		// keyed is declaration keyed by its objects, or by its file and ordinal when it declares none.
		keyed := func(declaration Declaration) Declaration {
			declaration.File = file.FileStart
			if len(declaration.Objects) == 0 {
				declaration.Ordinal = ordinal
				ordinal++
			}
			return declaration
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				collected = append(collected, walk.function(declaration, keyed))
			case *ast.GenDecl:
				// inherited is the last const spec of the declaration with values, whose type and values a spec with
				// neither repeats, as iota's do.
				var inherited *ast.ValueSpec
				for _, spec := range declaration.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						collected = append(collected, walk.typeSpec(spec, keyed))
					case *ast.ValueSpec:
						if declaration.Tok == token.CONST && spec.Type == nil && len(spec.Values) == 0 &&
							inherited != nil {
							collected = append(collected, walk.valueSpec(spec, inherited, keyed))
							continue
						}
						inherited = spec
						collected = append(collected, walk.valueSpec(spec, spec, keyed))
					}
				}
			}
		}
	}
	return collected, true
}

// function is what declaration, a function or method, uses, its signature (receiver, type parameters, parameters and
// results) and its body apart. An init function, and one named `_`, declares nothing an objectpath reaches.
func (walk *functionUsesWalk) function(
	declaration *ast.FuncDecl,
	keyed func(Declaration) Declaration,
) DeclarationUses {
	named := Declaration{Kind: DeclaredFunction, Name: declaration.Name.Name}
	isInit := declaration.Recv == nil && declaration.Name.Name == "init"
	if defined := walk.info.Defs[declaration.Name]; defined != nil && !isInit && declaration.Name.Name != "_" {
		named.Objects = []types.Object{defined}
	}
	walk.start, walk.end = declaration.Pos(), declaration.End()
	if declaration.Recv != nil {
		ast.Inspect(declaration.Recv, walk.visit)
	}
	ast.Inspect(declaration.Type, walk.visit)
	uses := DeclarationUses{Declaration: keyed(named), Signature: walk.part()}
	if declaration.Body != nil {
		ast.Inspect(declaration.Body, walk.visit)
		uses.Body, uses.HasBody = walk.part(), true
	}
	return uses
}

// typeSpec is what spec, one type of a type declaration, uses, its signature alone.
func (walk *functionUsesWalk) typeSpec(spec *ast.TypeSpec, keyed func(Declaration) Declaration) DeclarationUses {
	named := Declaration{Kind: DeclaredType, Name: spec.Name.Name}
	if defined := walk.info.Defs[spec.Name]; defined != nil && spec.Name.Name != "_" {
		named.Objects = []types.Object{defined}
	}
	walk.start, walk.end = spec.Pos(), spec.End()
	ast.Inspect(spec, walk.visit)
	return DeclarationUses{Declaration: keyed(named), Signature: walk.part()}
}

// valueSpec is what spec, one spec of a var or const declaration, uses, its type as its signature and its values as
// its body, read from source: spec itself, or for a const spec with neither, the last spec before it with values,
// whose type and values it repeats. The range its own objects lie in runs from source to spec.
func (walk *functionUsesWalk) valueSpec(
	spec *ast.ValueSpec,
	source *ast.ValueSpec,
	keyed func(Declaration) Declaration,
) DeclarationUses {
	named := Declaration{Kind: DeclaredValue}
	var blank bool
	for _, name := range spec.Names {
		if defined := walk.info.Defs[name]; defined != nil && name.Name != "_" {
			named.Objects = append(named.Objects, defined)
			continue
		}
		blank = true
	}
	if len(named.Objects) == 0 && blank {
		named.Name = "_"
	}
	walk.start, walk.end = source.Pos(), spec.End()
	if source.Type != nil {
		ast.Inspect(source.Type, walk.visit)
	}
	uses := DeclarationUses{Declaration: keyed(named), Signature: walk.part()}
	for _, value := range source.Values {
		ast.Inspect(value, walk.visit)
	}
	uses.Body, uses.HasBody = walk.part(), len(source.Values) > 0
	return uses
}

// part is the uses of the part walked since the last, each once with its kinds, and empties them for the next.
func (walk *functionUsesWalk) part() []PartUse {
	if len(walk.uses) == 0 {
		return nil
	}
	uses := make([]PartUse, 0, len(walk.uses))
	for key, kinds := range walk.uses {
		uses = append(uses, PartUse{Object: key.object, Receiver: key.receiver, Index: key.index, Kinds: kinds})
	}
	clear(walk.uses)
	return uses
}

// visitNode records what node uses and goes on into its children: an identifier's resolved object and its instance's
// type arguments, a selection, an expression's named type, and an implicit object.
func (walk *functionUsesWalk) visitNode(node ast.Node) bool {
	switch node := node.(type) {
	case nil:
		return false
	case *ast.Ident:
		if used := walk.info.Uses[node]; used != nil {
			kinds := UsedNamed
			if _, isConstant := used.(*types.Const); isConstant {
				kinds |= UsedConstant
			}
			walk.add(used, kinds)
		}
		if instance, instantiated := walk.info.Instances[node]; instantiated {
			for index := range instance.TypeArgs.Len() {
				walk.addNamed(instance.TypeArgs.At(index), UsedTypeArgument)
			}
		}
	case *ast.SelectorExpr:
		if selection := walk.info.Selections[node]; selection != nil {
			walk.addSelection(selection)
		}
	}
	if expression, isExpression := node.(ast.Expr); isExpression {
		if typeAndValue, recorded := walk.info.Types[expression]; recorded {
			walk.addNamed(typeAndValue.Type, UsedValueType)
		}
	}
	// A case clause's implicit variable is the declaration's own, left out by add, and an embedded field's is a use
	// of its type the identifier records; any other is recorded by name, as the identifier it stands for would be.
	if implicit := walk.info.Implicits[node]; implicit != nil {
		walk.add(implicit, UsedNamed)
	}
	return true
}

// add records a use of kinds of object, as its generic declares it, unless it is the declaration's own.
func (walk *functionUsesWalk) add(object types.Object, kinds UseKinds) {
	if object == nil || walk.declaredWithin(object) {
		return
	}
	walk.uses[partUseKey{object: genericOf(object)}] |= kinds
}

// addSelection records selection, from the named type it starts from to what it selects, by the path through
// embedded fields, and each embedded field on that path as a selection of its own name from the same type at the
// path's start, so a field another build adds or moves on the way reaches the part through the field's own file. A
// selection is left out only when what it selects and the type it starts from are both the declaration's own: a
// selection of its own method from another type is a use of that type's embedding.
func (walk *functionUsesWalk) addSelection(selection *types.Selection) {
	var receiver *types.TypeName
	if named := namedAt(selection.Recv()); named != nil {
		receiver = named.Origin().Obj()
	}
	path := selection.Index()
	current := selection.Recv()
	for position, step := range path[:len(path)-1] {
		if pointer, isPointer := current.Underlying().(*types.Pointer); isPointer {
			current = pointer.Elem()
		}
		structure, isStruct := current.Underlying().(*types.Struct)
		if !isStruct {
			break
		}
		embedded := structure.Field(step)
		walk.addSelected(receiver, embedded, path[:position+1])
		current = embedded.Type()
	}
	walk.addSelected(receiver, selection.Obj(), path)
}

// addSelected records the selection of selected, as its generic declares it, from receiver, nil when the selection
// starts from no named type, at path, unless both are the declaration's own.
func (walk *functionUsesWalk) addSelected(receiver *types.TypeName, selected types.Object, path []int) {
	generic := genericOf(selected)
	if walk.declaredWithin(generic) && (receiver == nil || walk.declaredWithin(receiver)) {
		return
	}
	index := make([]byte, 0, 4*len(path))
	for _, step := range path {
		index = strconv.AppendInt(index, int64(step), 10)
		index = append(index, ',')
	}
	walk.uses[partUseKey{object: generic, receiver: receiver, index: string(index)}] |= UsedSelected
}

// addNamed records a use of kinds of the named type typed is or points to, as its generic declares it, and of each
// such type among its type arguments; any other type records nothing, a type parameter being the declaration's own.
func (walk *functionUsesWalk) addNamed(typed types.Type, kinds UseKinds) {
	named := namedAt(typed)
	if named == nil {
		return
	}
	walk.add(named.Origin().Obj(), kinds)
	for index := range named.TypeArgs().Len() {
		if argumentNamed := namedAt(named.TypeArgs().At(index)); argumentNamed != nil {
			walk.add(argumentNamed.Origin().Obj(), kinds)
		}
	}
}

// declaredWithin reports whether object is the declaration's own: of the package, declared within the declaration's
// range, and in no package's or universe's scope, as a local, a parameter, a closure's variable, a label or a field
// of a type declared in a body is.
func (walk *functionUsesWalk) declaredWithin(object types.Object) bool {
	if object.Pkg() != walk.own || object.Pos() < walk.start || object.Pos() >= walk.end {
		return false
	}
	parent := object.Parent()
	return parent != walk.own.Scope() && parent != types.Universe
}

// namedAt is the named type typed is or points to, nil for any other type.
func namedAt(typed types.Type) *types.Named {
	if typed == nil {
		return nil
	}
	pointedTo := types.Unalias(typed)
	if pointer, isPointer := pointedTo.(*types.Pointer); isPointer {
		pointedTo = types.Unalias(pointer.Elem())
	}
	named, isNamed := pointedTo.(*types.Named)
	if !isNamed {
		return nil
	}
	return named
}

// genericOf is object as its generic declares it: an instance's field, method or function as the generic's.
func genericOf(object types.Object) types.Object {
	switch instantiated := object.(type) {
	case *types.Func:
		return instantiated.Origin()
	case *types.Var:
		return instantiated.Origin()
	}
	return object
}
