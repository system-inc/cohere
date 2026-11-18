package build_reads

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"

	"golang.org/x/tools/go/types/objectpath"
)

// functionUsesRecorder records what each top-level declaration of one package uses as the use walk reaches each node
// of it (useWalk), in the walk's one pass rather than a second (#kr1m304): an identifier's resolved object and its
// instance's type arguments as the walk reaches the identifier, a selection as it places the selector, an
// expression's named type as it applies the expression, and an implicit object as it reaches the clause or field
// that declares it. The use walk reaches each node of a declaration once, a function literal's and a composite
// literal's included, so each is recorded once.
//
// It keeps the package's record compact as it goes (FunctionUses): each thing used gets an entry once, by
// objectEntries and selectionEntries, and each part's uses are appended to the record's as packed entries, an entry
// used again in the same part joining its kinds to the use already there, found by lastUses. None of the maps outlive
// the walk.
type functionUsesRecorder struct {
	own    *types.Package
	info   *types.Info
	record FunctionUses

	// objectEntries and selectionEntries are each entry's index in the record's objects and selections, and indexes
	// each index path once, so every selection of one path shares one string.
	objectEntries    map[types.Object]uint32
	selectionEntries map[usedSelection]uint32
	indexes          map[string]string
	index            []byte

	// lastObjectUses and lastSelectionUses are, by entry, one past the place in the record's uses of the entry's last
	// use, 0 for none: one at or past partStart is the current part's.
	lastObjectUses    []uint32
	lastSelectionUses []uint32
	partStart         uint32

	// objectsUnknown and selectionsUnknown are, by entry, whether its identity renders unknown, its object's or, for a
	// selection, its receiver's, as encoder renders it (unplacedIdentity), and partUnknown whether the current part
	// holds a use of one, which marks the part unknown.
	encoder           objectpath.Encoder
	objectsUnknown    []bool
	selectionsUnknown []bool
	partUnknown       bool

	// namedReached are, by type, the named types its structure reaches (addNamed), each type walked once a package
	// however many expressions hold it, and visited and reaching the types one walk has reached so far and the named
	// types among them.
	namedReached map[types.Type][]*types.TypeName
	visited      map[types.Type]bool
	reaching     []*types.TypeName

	// declaring is whether the walk is in a declaration, declaration the one it is in, and recording whether its uses
	// are recorded as the walk reaches them, which a const spec repeating another's are not. rangeStart and rangeEnd
	// are its range, which bounds its own objects, and bodyStart the node its body starts at, where its signature
	// ends, nil for none.
	recording   bool
	declaring   bool
	declaration declarationRecord
	rangeStart  token.Pos
	rangeEnd    token.Pos
	bodyStart   ast.Node

	// fileStart is the start of the file being walked and ordinal the next ordinal in it of a declaration that declares
	// nothing, and inherited the index in the record of the last const spec with values of the declaration being
	// walked, whose type and values a spec with neither repeats, as iota's do, -1 for none.
	fileStart token.Pos
	ordinal   int32
	inherited int

	// overflowed is set when the package used more things than a PartUse can index, which leaves its uses unrecorded.
	overflowed bool
}

// newFunctionUsesRecorder is a recorder of own's declarations by info, own's type information, or nil when files, the
// syntax, or any of the maps a use is read from is missing: a use could go unseen, so nothing is recorded, which is
// unknown, never empty.
func newFunctionUsesRecorder(own *types.Package, info *types.Info, files []*ast.File) *functionUsesRecorder {
	if len(files) == 0 || info.Types == nil || info.Uses == nil || info.Defs == nil || info.Selections == nil ||
		info.Instances == nil || info.Implicits == nil {
		return nil
	}
	return &functionUsesRecorder{own: own, info: info, objectEntries: map[types.Object]uint32{},
		selectionEntries: map[usedSelection]uint32{}, indexes: map[string]string{},
		namedReached: map[types.Type][]*types.TypeName{}, visited: map[types.Type]bool{}, inherited: -1}
}

// startFile starts file, whose declarations' ordinals count from 0.
func (recorder *functionUsesRecorder) startFile(file *ast.File) {
	if recorder == nil {
		return
	}
	recorder.fileStart, recorder.ordinal = file.FileStart, 0
}

// beginFunction begins declaration, a function or method: its signature (receiver, type parameters, parameters and
// results) until its body, when it has one. An init function, and one named `_`, declares nothing an objectpath
// reaches.
func (recorder *functionUsesRecorder) beginFunction(declaration *ast.FuncDecl) {
	if recorder == nil {
		return
	}
	var objects []types.Object
	isInit := declaration.Recv == nil && declaration.Name.Name == "init"
	if defined := recorder.info.Defs[declaration.Name]; defined != nil && !isInit && declaration.Name.Name != "_" {
		objects = []types.Object{defined}
	}
	recorder.begin(indexOfFunction, declaration.Name.Name, objects, declaration.Pos(), declaration.End())
	recorder.declaration.hasBody = declaration.Body != nil
	if declaration.Body != nil {
		recorder.bodyStart = declaration.Body
	}
}

// beginGeneral begins a var, const, type or import declaration, whose specs each begin on their own (beginSpec).
func (recorder *functionUsesRecorder) beginGeneral() {
	if recorder == nil {
		return
	}
	recorder.inherited = -1
}

// beginSpec begins spec, one spec of a general declaration of keyword: a type spec's signature alone, or a value spec's
// type as its signature and its values as its body. A const spec with neither repeats the last one with values, so it
// records nothing as it is walked and takes that one's parts as its own once it ends. An import spec declares nothing
// recorded.
func (recorder *functionUsesRecorder) beginSpec(keyword token.Token, spec ast.Spec) {
	if recorder == nil {
		return
	}
	switch spec := spec.(type) {
	case *ast.TypeSpec:
		var objects []types.Object
		if defined := recorder.info.Defs[spec.Name]; defined != nil && spec.Name.Name != "_" {
			objects = []types.Object{defined}
		}
		recorder.begin(indexOfType, spec.Name.Name, objects, spec.Pos(), spec.End())
	case *ast.ValueSpec:
		var objects []types.Object
		var blank bool
		for _, name := range spec.Names {
			if defined := recorder.info.Defs[name]; defined != nil && name.Name != "_" {
				objects = append(objects, defined)
				continue
			}
			blank = true
		}
		var name string
		if len(objects) == 0 && blank {
			name = "_"
		}
		recorder.begin(indexOfValue, name, objects, spec.Pos(), spec.End())
		if keyword == token.CONST && spec.Type == nil && len(spec.Values) == 0 && recorder.inherited >= 0 {
			// Nothing of its own is recorded: the walk reaches only its names, which declare.
			recorder.recording = false
			return
		}
		recorder.inherited = len(recorder.record.declarations)
		recorder.declaration.hasBody = len(spec.Values) > 0
		if len(spec.Values) > 0 {
			recorder.bodyStart = spec.Values[0]
		}
	}
}

// begin begins a declaration of kind, named name, declaring objects, whose own objects lie between start and end.
func (recorder *functionUsesRecorder) begin(
	kind declarationKindIndex,
	name string,
	objects []types.Object,
	start token.Pos,
	end token.Pos,
) {
	recorder.declaring, recorder.recording = true, true
	recorder.rangeStart, recorder.rangeEnd, recorder.bodyStart = start, end, nil
	uses := recorder.usesLength()
	recorder.declaration = declarationRecord{kind: kind, name: name, file: recorder.fileStart, usesStart: uses,
		signatureEnd: uses, bodyEnd: uses}
	recorder.declaration.objectsStart = uint32(len(recorder.record.declared))
	recorder.record.declared = append(recorder.record.declared, objects...)
	recorder.declaration.objectsEnd = uint32(len(recorder.record.declared))
	if len(objects) == 0 {
		recorder.declaration.ordinal = recorder.ordinal
		recorder.ordinal++
	}
	recorder.partStart, recorder.partUnknown = uses, false
}

// end ends the declaration begun last, if any: its signature, when its body never started, and its body end where
// the walk has reached, or, for a const spec that repeats another, they are that one's.
func (recorder *functionUsesRecorder) end() {
	if recorder == nil || !recorder.declaring {
		return
	}
	declaration := recorder.declaration
	if recorder.recording {
		uses := recorder.usesLength()
		if recorder.bodyStart != nil {
			declaration.bodyEnd, declaration.bodyUnknown = uses, recorder.partUnknown
		} else {
			declaration.signatureEnd, declaration.bodyEnd = uses, uses
			declaration.signatureUnknown = recorder.partUnknown
		}
	} else {
		repeated := recorder.record.declarations[recorder.inherited]
		declaration.usesStart, declaration.signatureEnd = repeated.usesStart, repeated.signatureEnd
		declaration.bodyEnd, declaration.hasBody = repeated.bodyEnd, repeated.hasBody
		declaration.signatureUnknown, declaration.bodyUnknown = repeated.signatureUnknown, repeated.bodyUnknown
	}
	recorder.record.declarations = append(recorder.record.declarations, declaration)
	recorder.declaring, recorder.recording, recorder.bodyStart = false, false, nil
}

// usesLength is how many uses the record holds, every part's so far.
func (recorder *functionUsesRecorder) usesLength() uint32 {
	return uint32(len(recorder.record.uses))
}

// finish is the record, each of its slices as long as it holds and no longer, nil when it could not be recorded.
func (recorder *functionUsesRecorder) finish() *FunctionUses {
	if recorder == nil || recorder.overflowed {
		return nil
	}
	return &FunctionUses{
		objects:      slices.Clone(recorder.record.objects),
		selections:   slices.Clone(recorder.record.selections),
		declarations: slices.Clone(recorder.record.declarations),
		declared:     slices.Clone(recorder.record.declared),
		uses:         slices.Clone(recorder.record.uses),
	}
}

// reach records what node itself uses, as the walk reaches it, and ends the signature where the body starts: an
// identifier's resolved object and its instance's type arguments, and the implicit object a clause or field declares.
// A selector's selection and an expression's type are recorded where the walk reads them (selection, valueType).
func (recorder *functionUsesRecorder) reach(node ast.Node) {
	if recorder == nil || !recorder.recording || node == nil {
		return
	}
	if node == recorder.bodyStart {
		recorder.declaration.signatureEnd = recorder.usesLength()
		recorder.declaration.signatureUnknown = recorder.partUnknown
		recorder.partStart, recorder.partUnknown = recorder.declaration.signatureEnd, false
	}
	switch node := node.(type) {
	case *ast.Ident:
		if used := recorder.info.Uses[node]; used != nil {
			kinds := UsedNamed
			if _, isConstant := used.(*types.Const); isConstant {
				kinds |= UsedConstant
			}
			recorder.add(used, kinds)
		}
		if instance, instantiated := recorder.info.Instances[node]; instantiated {
			for index := range instance.TypeArgs.Len() {
				recorder.addNamed(instance.TypeArgs.At(index), UsedTypeArgument)
			}
		}
	case *ast.CaseClause, *ast.Field, *ast.ImportSpec:
		// The only nodes an implicit object is recorded at. A case clause's implicit variable is the declaration's
		// own, left out by add, and an embedded field's is a use of its type the identifier records; any other is
		// recorded by name, as the identifier it stands for would be.
		if implicit := recorder.info.Implicits[node]; implicit != nil {
			recorder.add(implicit, UsedNamed)
		}
	}
}

// valueType records every named type an expression's type, typed, reaches, as the walk applies the expression.
func (recorder *functionUsesRecorder) valueType(typed types.Type) {
	if recorder == nil || !recorder.recording {
		return
	}
	recorder.addNamed(typed, UsedValueType)
}

// selection records selection, from the named type it starts from to what it selects, by the path through embedded
// fields, and each embedded field on that path as a selection of its own name from the same type at the path's start,
// so a field another build adds or moves on the way reaches the part through the field's own file. A selection is
// left out only when what it selects and the type it starts from are both the declaration's own: a selection of its
// own method from another type is a use of that type's embedding.
func (recorder *functionUsesRecorder) selection(selection *types.Selection) {
	if recorder == nil || !recorder.recording {
		return
	}
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
		recorder.addSelected(receiver, embedded, path[:position+1])
		current = embedded.Type()
	}
	recorder.addSelected(receiver, selection.Obj(), path)
}

// add records a use of kinds of object, as its generic declares it, unless it is the declaration's own.
func (recorder *functionUsesRecorder) add(object types.Object, kinds UseKinds) {
	if object == nil || recorder.declaredWithin(object) {
		return
	}
	generic := genericOf(object)
	entry, entered := recorder.objectEntries[generic]
	if !entered {
		entry = uint32(len(recorder.record.objects))
		recorder.objectEntries[generic] = entry
		recorder.record.objects = append(recorder.record.objects, generic)
		recorder.lastObjectUses = append(recorder.lastObjectUses, 0)
		recorder.objectsUnknown = append(recorder.objectsUnknown, recorder.unknown(generic))
	}
	recorder.lastObjectUses[entry] = recorder.use(entry, 0, recorder.lastObjectUses[entry], kinds)
	recorder.partUnknown = recorder.partUnknown || recorder.objectsUnknown[entry]
}

// addSelected records the selection of selected, as its generic declares it, from receiver, nil when the selection
// starts from no named type, at path, unless both are the declaration's own.
func (recorder *functionUsesRecorder) addSelected(receiver *types.TypeName, selected types.Object, path []int) {
	generic := genericOf(selected)
	if recorder.declaredWithin(generic) && (receiver == nil || recorder.declaredWithin(receiver)) {
		return
	}
	recorder.index = recorder.index[:0]
	for _, step := range path {
		recorder.index = strconv.AppendInt(recorder.index, int64(step), 10)
		recorder.index = append(recorder.index, ',')
	}
	index, written := recorder.indexes[string(recorder.index)]
	if !written {
		index = string(recorder.index)
		recorder.indexes[index] = index
	}
	key := usedSelection{receiver: receiver, object: generic, index: index}
	entry, entered := recorder.selectionEntries[key]
	if !entered {
		entry = uint32(len(recorder.record.selections))
		recorder.selectionEntries[key] = entry
		recorder.record.selections = append(recorder.record.selections, key)
		recorder.lastSelectionUses = append(recorder.lastSelectionUses, 0)
		recorder.selectionsUnknown = append(recorder.selectionsUnknown,
			recorder.unknown(generic) || receiver != nil && recorder.unknown(receiver))
	}
	recorder.lastSelectionUses[entry] = recorder.use(entry, partUseSelected, recorder.lastSelectionUses[entry],
		UsedSelected)
	recorder.partUnknown = recorder.partUnknown || recorder.selectionsUnknown[entry]
}

// unknown reports whether object's identity renders unknown, so a part that uses it can never be matched to another
// build's.
func (recorder *functionUsesRecorder) unknown(object types.Object) bool {
	return unplacedIdentity(&recorder.encoder, object).Kind == IdentityUnknown
}

// use records a use of kinds of entry, of the table selected names, in the part being walked: joined to its use
// there when it has one, by last, one past the place of its last use, or appended as its own. It returns one past
// the place of the entry's use now.
func (recorder *functionUsesRecorder) use(entry uint32, selected PartUse, last uint32, kinds UseKinds) uint32 {
	if last > recorder.partStart {
		recorder.record.uses[last-1] |= PartUse(kinds)
		return last
	}
	if entry >= partUseEntryLimit {
		recorder.overflowed = true
		return last
	}
	recorder.record.uses = append(recorder.record.uses, PartUse(entry)<<partUseEntryShift|selected|PartUse(kinds))
	return recorder.usesLength()
}

// addNamed records a use of kinds of every named type typed reaches, as its generic declares it: typed itself when it
// is named, or the named types its structure holds, the elements, keys, parameters, results, fields, methods and
// embedded types of an unnamed type, and each named type's type arguments at any depth, since comparing, copying or
// converting `[1]other.T`, or checking it against a constraint, reads T as surely as a value of T does. The walk stops
// at each named type, whose own structure is its declaration's, and records each alias on the way as well as what it
// stands for. A type parameter records nothing, being the declaration's own.
func (recorder *functionUsesRecorder) addNamed(typed types.Type, kinds UseKinds) {
	// A basic type, and a named type with no type arguments, held directly or through a pointer, are most expressions'
	// and reach nothing more, so they skip the walk and its memo.
	held := typed
	if pointer, isPointer := held.(*types.Pointer); isPointer {
		held = pointer.Elem()
	}
	switch held := held.(type) {
	case nil, *types.Basic, *types.TypeParam:
		return
	case *types.Named:
		if held.TypeArgs().Len() == 0 {
			recorder.add(held.Origin().Obj(), kinds)
			return
		}
	}
	reached, walked := recorder.namedReached[typed]
	if !walked {
		clear(recorder.visited)
		recorder.reaching = recorder.reaching[:0]
		recorder.reachNamed(typed)
		reached = slices.Clone(recorder.reaching)
		recorder.namedReached[typed] = reached
	}
	for _, name := range reached {
		recorder.add(name, kinds)
	}
}

// reachNamed adds to reaching every named type and alias typed reaches, as its generic declares it (see addNamed),
// each type walked once by visited, which also ends a walk that comes round to a type again.
func (recorder *functionUsesRecorder) reachNamed(typed types.Type) {
	if typed == nil || recorder.visited[typed] {
		return
	}
	recorder.visited[typed] = true
	switch typed := typed.(type) {
	case *types.Alias:
		recorder.reaching = append(recorder.reaching, typed.Origin().Obj())
		for index := range typed.TypeArgs().Len() {
			recorder.reachNamed(typed.TypeArgs().At(index))
		}
		recorder.reachNamed(types.Unalias(typed))
	case *types.Named:
		recorder.reaching = append(recorder.reaching, typed.Origin().Obj())
		for index := range typed.TypeArgs().Len() {
			recorder.reachNamed(typed.TypeArgs().At(index))
		}
	case *types.Pointer:
		recorder.reachNamed(typed.Elem())
	case *types.Slice:
		recorder.reachNamed(typed.Elem())
	case *types.Array:
		recorder.reachNamed(typed.Elem())
	case *types.Chan:
		recorder.reachNamed(typed.Elem())
	case *types.Map:
		recorder.reachNamed(typed.Key())
		recorder.reachNamed(typed.Elem())
	case *types.Signature:
		// A receiver is left out: only an interface's method has one in a type the walk reaches, and it is that
		// interface.
		recorder.reachNamed(typed.Params())
		recorder.reachNamed(typed.Results())
	case *types.Tuple:
		for index := range typed.Len() {
			recorder.reachNamed(typed.At(index).Type())
		}
	case *types.Struct:
		for index := range typed.NumFields() {
			recorder.reachNamed(typed.Field(index).Type())
		}
	case *types.Interface:
		for index := range typed.NumExplicitMethods() {
			recorder.reachNamed(typed.ExplicitMethod(index).Type())
		}
		for index := range typed.NumEmbeddeds() {
			recorder.reachNamed(typed.EmbeddedType(index))
		}
	case *types.Union:
		for index := range typed.Len() {
			recorder.reachNamed(typed.Term(index).Type())
		}
	}
}

// declaredWithin reports whether object is the declaration's own: of the package, declared within the declaration's
// range, and in no package's or universe's scope, as a local, a parameter, a closure's variable, a label or a field
// of a type declared in a body is.
func (recorder *functionUsesRecorder) declaredWithin(object types.Object) bool {
	if object.Pkg() != recorder.own || object.Pos() < recorder.rangeStart || object.Pos() >= recorder.rangeEnd {
		return false
	}
	parent := object.Parent()
	return parent != recorder.own.Scope() && parent != types.Universe
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
