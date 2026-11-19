package build_dependence

import (
	"go/ast"
	"go/token"
	"go/types"
)

// kindEdge says the from object's bit depends on the to object's bit (rule 3): from is an object a unit declares, and
// either bit is the underlying one when its flag is set. Edges hold objects rather than nodes, since the to object can
// be another package's, added later or never (resolve).
type kindEdge struct {
	from           types.Object
	fromUnderlying bool
	to             types.Object
	toUnderlying   bool
}

// collector is one package's walk of its declarations: its type information, the units and kind edges collected so
// far, each edge once, and a walker of types' bits shared by the walk. Its maps and slices are a worker's scratch,
// kept from one package to the next (workerScratch.collector).
type collector struct {
	typesInformation *types.Info
	units            []unitRecord
	edges            []kindEdge
	edgeSeen         map[kindEdge]bool
	walker           *bitWalker
	bodies           *bodyWalk

	// declarationsOnly says the walk collects no unit's references or callees, its graph's mode
	// (Graph.declarationsOnly), set for each package's walk (Graph.addPackage). Every unit's size, universe names
	// and declares and every kind edge are collected as a whole walk collects them.
	declarationsOnly bool

	// calleeSeen are the callees of the unit staticCallees is collecting, and calleeStore every unit's callees, each
	// unit's a slice of it, which newRecord reads into the record's own.
	calleeSeen  map[types.Object]bool
	calleeStore []types.Object

	// declareStore is every unit's declares, each unit's a slice of it from declaresStart (declare), which newRecord
	// reads into the record's own, and owners the objects whose members collectMembers is declaring.
	declareStore  []types.Object
	declaresStart int
	owners        []types.Object

	// passedTypes are the types the initializers of the var collectValue is collecting pass through, in the order its
	// walk met them, each once by passedSeen, for a var that declares no type.
	passedTypes []types.Type
	passedSeen  map[types.Type]bool

	// unitWalk, membersWalk, usesWalk and initializerWalk are newUnit's, collectMembers', addUses' and
	// collectValue's walks, kept here so none of them makes a closure for each walk.
	unitWalk        unitVisitor
	membersWalk     membersVisitor
	usesWalk        usesVisitor
	initializerWalk initializerVisitor
}

// collectFile collects every unit of file, whose place tokenFile holds. Imports are no units.
func (collecting *collector) collectFile(tokenFile *token.File, file *ast.File) {
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			collecting.collectFunction(tokenFile, declaration)
		case *ast.GenDecl:
			if declaration.Tok == token.IMPORT {
				continue
			}
			var lastType ast.Expr
			var lastValues []ast.Expr
			for _, spec := range declaration.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					collecting.collectType(tokenFile, spec)
				case *ast.ValueSpec:
					specType, specValues := spec.Type, spec.Values
					if declaration.Tok == token.CONST {
						// A const spec with neither type nor values repeats the last one's, and depends on what
						// that one's value refers to.
						if spec.Type == nil && len(spec.Values) == 0 {
							specType, specValues = lastType, lastValues
						} else {
							lastType, lastValues = spec.Type, spec.Values
						}
					}
					collecting.collectValue(tokenFile, declaration.Tok, spec, specType, specValues)
				}
			}
		}
	}
}

// newUnit adds the unit of declaration to the collector's units, sized and placed, and the universe's names it uses,
// with nothing declared yet, its size and names read in one walk of it. It is the last unit until the next newUnit,
// and nothing else adds one, so its collector fills it in place through the pointer.
func (collecting *collector) newUnit(tokenFile *token.File, declaration ast.Node) *unitRecord {
	collecting.unitWalk = unitVisitor{typesInformation: collecting.typesInformation}
	ast.Walk(&collecting.unitWalk, declaration)
	collecting.units = append(collecting.units, unitRecord{
		// The file a reader sees, past any //line directive, so a cgo package's unit carries its .go file as written
		// rather than the cgo tool's output in the build cache, whose name changes from run to run.
		file:          tokenFile.Position(declaration.Pos()).Filename,
		start:         tokenFile.Offset(declaration.Pos()),
		end:           tokenFile.Offset(declaration.End()),
		nodes:         collecting.unitWalk.nodes,
		universeNames: collecting.unitWalk.universeNames,
	})
	collecting.declaresStart = len(collecting.declareStore)
	return &collecting.units[len(collecting.units)-1]
}

// declare adds object to what unit declares, unit being the one newUnit added last: its declares are a slice of the
// collector's store, which the next package's walk writes over, so newRecord reads them into the record's own.
//
// Mutates: unit declares object
func (collecting *collector) declare(unit *unitRecord, object types.Object) {
	collecting.declareStore = append(collecting.declareStore, object)
	// Sliced again from the store each time, since a store grown past its capacity moved; the slice's capacity ends
	// with it, so nothing appended to it could write over the next unit's declares.
	end := len(collecting.declareStore)
	unit.declares = collecting.declareStore[collecting.declaresStart:end:end]
}

// owning is the collector's owners holding object alone, for collectMembers.
func (collecting *collector) owning(object types.Object) []types.Object {
	collecting.owners = append(collecting.owners[:0], object)
	return collecting.owners
}

// collectFunction collects a FuncDecl's unit: its function, the fields of anonymous structs in its signature, and its
// body's references and callees, which a declarations-only walk leaves out.
func (collecting *collector) collectFunction(tokenFile *token.File, declaration *ast.FuncDecl) {
	unit := collecting.newUnit(tokenFile, declaration)
	if declaration.Body != nil && !collecting.declarationsOnly {
		unit.references = collecting.bodies.references(declaration.Body)
		unit.callees = collecting.staticCallees(declaration.Body)
	}
	function, isFunction := collecting.typesInformation.Defs[declaration.Name].(*types.Func)
	if !isFunction {
		return
	}
	collecting.declare(unit, function)
	collecting.collectMembers(unit, collecting.owning(function), false, declaration.Type)
	// The signature's name bits, a signature moving being what a function's bit carries.
	signature := function.Signature()
	collecting.addEdges(function, false, collecting.walker.borrowedReferences(signature, false))
	collecting.addUses(function, false, false, declaration.Type)
	receiver := signature.Recv()
	if receiver == nil {
		return
	}
	// Method receivers are included, the larger set: without receiver edges the counter found 8 to 14% fewer
	// dependent objects. A method's bit takes its receiver's name bit, since its method set moving is what the name
	// bit carries.
	collecting.addEdges(function, false, collecting.walker.borrowedReferences(receiver.Type(), false))
	for parameter := range signature.RecvTypeParams().TypeParams() {
		collecting.addEdges(function, false, collecting.walker.borrowedReferences(parameter.Constraint(), false))
	}
	receiverType := receiver.Type()
	if pointer, isPointer := receiverType.(*types.Pointer); isPointer {
		receiverType = pointer.Elem()
	}
	if named, isNamed := receiverType.(*types.Named); isNamed {
		unit.receiver = named.Origin().Obj()
	}
}

// collectType collects a TypeSpec's unit: its type name and the fields and interface methods declared in it.
func (collecting *collector) collectType(tokenFile *token.File, spec *ast.TypeSpec) {
	unit := collecting.newUnit(tokenFile, spec)
	typeName, isTypeName := collecting.typesInformation.Defs[spec.Name].(*types.TypeName)
	if !isTypeName {
		return
	}
	collecting.declare(unit, typeName)
	collecting.collectMembers(unit, collecting.owning(typeName), true, spec.Type)
	collecting.collectTypeKinds(typeName, spec)
}

// collectTypeKinds is a type spec's edges. The name bit reads its type parameters' constraints in full, its declared
// methods, the name bits of its embedded fields, whose methods it promotes, for an interface its own underlying,
// since an interface's methods are its underlying, and for a type defined from another type's name, `type T S`, S in
// full, since T's fields and promoted methods are S's. The underlying bit reads everything the underlying type reaches
// in full, as a value.
func (collecting *collector) collectTypeKinds(typeName *types.TypeName, spec *ast.TypeSpec) {
	switch declared := typeName.Type().(type) {
	case *types.Named:
		collecting.addEdges(typeName, true, collecting.walker.borrowedReferences(declared.Underlying(), true))
		if typeAndValue, isRecorded := collecting.typesInformation.Types[spec.Type]; isRecorded {
			// `type T S` keeps S in its expression's type, where the resolved underlying has lost it, so S is read in
			// full into T's underlying.
			collecting.addEdges(typeName, true, collecting.walker.borrowedReferences(typeAndValue.Type, true))
			switch typeAndValue.Type.(type) {
			case *types.Named, *types.Alias:
				// T's shape is S's too: its fields, and the methods its embedded fields promote, are S's, and a struct
				// that embeds T reads them through T's name bit, so S is read in full into that as well (#9paaen9). A
				// type that spells its struct reads alike in every build, and its embedded fields' edges below cover
				// it.
				collecting.addEdges(typeName, false, collecting.walker.borrowedReferences(typeAndValue.Type, true))
			}
		}
		collecting.addUses(typeName, true, false, spec.Type)
		if spec.TypeParams != nil {
			collecting.addUses(typeName, false, false, spec.TypeParams)
		}
		for parameter := range declared.TypeParams().TypeParams() {
			collecting.addEdges(typeName, false, collecting.walker.borrowedReferences(parameter.Constraint(), true))
		}
		for method := range declared.Methods() {
			collecting.addEdge(kindEdge{from: typeName, to: method})
		}
		switch underlying := declared.Underlying().(type) {
		case *types.Interface:
			collecting.addEdge(kindEdge{from: typeName, to: typeName, toUnderlying: true})
		case *types.Struct:
			// An embedded field carries the embedded type's name bit into the struct's name bit, since promoted
			// methods are part of the method set the name bit covers. This keeps os.File name-dependent: its
			// embedded *file is declared in a seed file.
			for field := range underlying.Fields() {
				if !field.Embedded() {
					continue
				}
				embedded := field.Type()
				if pointer, isPointer := embedded.(*types.Pointer); isPointer {
					embedded = pointer.Elem()
				}
				switch embedded := embedded.(type) {
				case *types.Named:
					collecting.addEdge(kindEdge{from: typeName, to: embedded.Origin().Obj()})
				case *types.Alias:
					collecting.addEdge(kindEdge{from: typeName, to: embedded.Origin().Obj()})
				}
			}
		}
	case *types.Alias:
		// An alias is its target's bits.
		collecting.addEdges(typeName, false, collecting.walker.borrowedReferences(declared.Rhs(), false))
		collecting.addEdges(typeName, true, collecting.walker.borrowedReferences(declared.Rhs(), true))
		collecting.addUses(typeName, false, false, spec.Type)
		collecting.addUses(typeName, true, false, spec.Type)
		if spec.TypeParams != nil {
			collecting.addUses(typeName, false, false, spec.TypeParams)
			collecting.addUses(typeName, true, false, spec.TypeParams)
		}
		for parameter := range declared.TypeParams().TypeParams() {
			collecting.addEdges(typeName, false, collecting.walker.borrowedReferences(parameter.Constraint(), true))
		}
	}
}

// collectMembers declares the struct fields and interface methods under root for unit, each depending on its own type
// by name, and makes owners depend on each: on their underlying bit when ownerUnderlying is set, else on their only
// bit. A field's own bit and its owner's underlying bit together are what carry a field whose array length hangs on a
// constant into its struct's underlying.
//
// Mutates: unit declares the members, and collecting gains their edges
func (collecting *collector) collectMembers(
	unit *unitRecord,
	owners []types.Object,
	ownerUnderlying bool,
	root ast.Expr,
) {
	if root == nil {
		return
	}
	collecting.membersWalk = membersVisitor{collecting: collecting, unit: unit, owners: owners,
		ownerUnderlying: ownerUnderlying}
	ast.Walk(&collecting.membersWalk, root)
}

// collectFields declares the fields of structure, a struct type in a unit's header, as collectMembers says.
//
// Mutates: unit declares the fields, and collecting gains their edges
func (collecting *collector) collectFields(
	unit *unitRecord,
	owners []types.Object,
	ownerUnderlying bool,
	structure *ast.StructType,
) {
	typeAndValue, isRecorded := collecting.typesInformation.Types[structure]
	if !isRecorded {
		return
	}
	declared, isStruct := typeAndValue.Type.Underlying().(*types.Struct)
	if !isStruct {
		return
	}
	index := 0
	for _, field := range structure.Fields.List {
		for range max(len(field.Names), 1) {
			if index >= declared.NumFields() {
				return
			}
			variable := declared.Field(index)
			index++
			collecting.declare(unit, variable)
			collecting.addEdges(variable, false, collecting.walker.borrowedReferences(variable.Type(), false))
			collecting.addUses(variable, false, false, field.Type)
			for _, owner := range owners {
				collecting.addEdge(kindEdge{from: owner, fromUnderlying: ownerUnderlying, to: variable})
			}
		}
	}
}

// collectInterfaceMethods declares the methods of an interface type in a unit's header, as collectMembers says, each
// depending on its signature without its receiver, which for an interface method is the interface itself.
//
// Mutates: unit declares the methods, and collecting gains their edges
func (collecting *collector) collectInterfaceMethods(
	unit *unitRecord,
	owners []types.Object,
	ownerUnderlying bool,
	declared *ast.InterfaceType,
) {
	for _, field := range declared.Methods.List {
		for _, name := range field.Names {
			method, isMethod := collecting.typesInformation.Defs[name].(*types.Func)
			if !isMethod {
				continue
			}
			collecting.declare(unit, method)
			signature := method.Signature()
			collecting.addEdges(method, false, collecting.walker.borrowedReferences(signature.Params(), false))
			collecting.addEdges(method, false, collecting.walker.borrowedReferences(signature.Results(), false))
			collecting.addUses(method, false, false, field.Type)
			for _, owner := range owners {
				collecting.addEdge(kindEdge{from: owner, fromUnderlying: ownerUnderlying, to: method})
			}
		}
	}
}

// collectValue collects a ValueSpec's unit, specType and specValues being the type and values it takes, a const
// spec's repeated from the last one that had them: its names, the fields of anonymous structs in its type, and a
// var's initializers as its body, whose references and callees a declarations-only walk leaves out. The types the
// initializers pass through and the objects they use are a var's kind edges, collected in either mode.
func (collecting *collector) collectValue(
	tokenFile *token.File,
	keyword token.Token,
	spec *ast.ValueSpec,
	specType ast.Expr,
	specValues []ast.Expr,
) {
	unit := collecting.newUnit(tokenFile, spec)
	isVariable := keyword == token.VAR
	if isVariable && len(spec.Values) > 0 && !collecting.declarationsOnly {
		initializers := expressionNodes(spec.Values)
		unit.references = collecting.bodies.references(initializers...)
		unit.callees = collecting.staticCallees(initializers...)
	}
	// A var that declares no type takes it from what its initializers pass through, gathered here for every name.
	collecting.passedTypes = collecting.passedTypes[:0]
	clear(collecting.passedSeen)
	if isVariable && spec.Type == nil {
		collecting.initializerWalk = initializerVisitor{collecting: collecting}
		for _, value := range spec.Values {
			ast.Walk(&collecting.initializerWalk, value)
		}
	}
	values := expressionNodes(specValues)
	// The owners of the members collectMembers declares are the spec's names, gathered here; nothing between reads or
	// writes them.
	collecting.owners = collecting.owners[:0]
	for _, name := range spec.Names {
		object := collecting.typesInformation.Defs[name]
		if object == nil {
			continue
		}
		collecting.declare(unit, object)
		collecting.owners = append(collecting.owners, object)
		if isVariable {
			// A var's own type, as a name, and the initializer an untyped var takes its type from: each object it
			// uses by its own bit, and every type it passes through in full, entered behind pointers, slices, maps,
			// channels and signatures too. Its type as written can differ by what lies under a named type whose name
			// bit no build sets, `rows[0]` an [8]int where the host's is a [4]int over `type Rows [2][Length]int`, so
			// the types' names alone would leave its bit unset.
			collecting.addEdges(object, false, collecting.walker.borrowedReferences(object.Type(), false))
			collecting.addUses(object, false, false, specType)
			if spec.Type == nil {
				collecting.addUses(object, false, false, values...)
				for _, passed := range collecting.passedTypes {
					collecting.addEdges(object, false, collecting.walker.borrowedEnteringReferences(passed))
				}
			}
			continue
		}
		// A constant reads its own type in full, and whatever its value expression uses, in full too:
		// unsafe.Sizeof(v) reaches v's layout.
		collecting.addEdges(object, false, collecting.walker.borrowedReferences(object.Type(), true))
		collecting.addUses(object, false, true, specType)
		collecting.addUses(object, false, true, values...)
	}
	collecting.collectMembers(unit, collecting.owners, false, spec.Type)
}

// passType gathers passed into passedTypes, once, for the var collectValue is collecting (initializerVisitor).
func (collecting *collector) passType(passed types.Type) {
	if passed == nil || collecting.passedSeen[passed] {
		return
	}
	collecting.passedSeen[passed] = true
	collecting.passedTypes = append(collecting.passedTypes, passed)
}

// addUses makes from's bit depend on the own bit of each object an identifier or selector under roots resolves to.
// Type names are left to the structural walk of from's type, except where full, in a constant's value or an array's
// length (usesVisitor.Visit), where a type name or a var is read in full. A package name, a builtin, nil and a label
// are the same in every build.
func (collecting *collector) addUses(from types.Object, fromUnderlying bool, full bool, roots ...ast.Node) {
	collecting.usesWalk = usesVisitor{collecting: collecting, from: from, fromUnderlying: fromUnderlying, full: full}
	for _, root := range roots {
		if root == nil {
			continue
		}
		ast.Walk(&collecting.usesWalk, root)
	}
}

// addUse is addUses for one object used.
func (collecting *collector) addUse(from types.Object, fromUnderlying bool, full bool, used types.Object) {
	switch used.(type) {
	case *types.PkgName, *types.Builtin, *types.Nil, *types.Label:
		return
	case *types.TypeName:
		if full {
			collecting.addEdges(from, fromUnderlying, collecting.walker.borrowedReferences(used.Type(), true))
		}
		return
	}
	collecting.addEdge(kindEdge{from: from, fromUnderlying: fromUnderlying, to: used})
	if full {
		collecting.addEdges(from, fromUnderlying, collecting.walker.borrowedReferences(used.Type(), true))
	}
}

// addEdges makes from's bit depend on each of referenced, which may be the walker's own buffer (borrowedReferences):
// adding an edge walks no type.
func (collecting *collector) addEdges(from types.Object, fromUnderlying bool, referenced []reference) {
	for _, target := range referenced {
		collecting.addEdge(kindEdge{from: from, fromUnderlying: fromUnderlying, to: target.object,
			toUnderlying: target.touchesUnderlying})
	}
}

// addEdge records edge once, both its ends as their generics declare them. An edge to a local is dropped, as the
// counter dropped every edge to an object no unit declares, and so is one to an object of the universe, the same in
// every build.
func (collecting *collector) addEdge(edge kindEdge) {
	edge.from = canonical(edge.from)
	edge.to = canonical(edge.to)
	if local(edge.to) || universal(edge.to) || collecting.edgeSeen[edge] {
		return
	}
	collecting.edgeSeen[edge] = true
	collecting.edges = append(collecting.edges, edge)
}

// expressionNodes is expressions as syntax nodes.
func expressionNodes(expressions []ast.Expr) []ast.Node {
	nodes := make([]ast.Node, 0, len(expressions))
	for _, expression := range expressions {
		nodes = append(nodes, expression)
	}
	return nodes
}
