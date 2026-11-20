package unused_code

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
)

// bodyRecorder writes a bodyRecord for each function declaration's body the walk reads (#xeafx79). A graph holds one
// only when recordBodies asked for it, and without one the walk does what it always did, each place the recorder
// listens at costing one nil check.
//
// It listens where the graph is made, in use and in see, above the graph's own dedupe: the graph keeps an edge from
// the first walk that adds it alone, so its lists cannot say which bodies added an edge two of them share.
type bodyRecorder struct {
	// records is each body's record, in the order the walk read the bodies, those that share a key among them
	// (bodyRecords).
	records []bodyRecord
	// cursor is the body the walk is in, which isOpen says it is in.
	cursor bodyCursor
	isOpen bool
	// locals is each local of every body read so far, by its node: whose it is, and its index there. A step after
	// the walk finds the record a node belongs to here.
	locals map[nodeIndex]localOfRecord
	// rooted is each local a step after the walk made the root use, so a local two steps reach is recorded once.
	rooted map[nodeIndex]struct{}
	// declared is each object declared outside a body that some body's record names, by its node: its place and
	// whether the place finds it, each asked once however many bodies name it.
	declared map[nodeIndex]declaredEnd
	// interfaceInstances is where the package's source instantiates one of its own generic interface types, sorted,
	// the entries of typesInfo.Instances that entry adds to the interfaces a named type may implement.
	interfaceInstances []token.Pos

	// uses, owned and files are the open body's use edges and own edges, each keyed by edgeKey, and the files its
	// record names. They belong to the cursor, and live here so that one set of maps serves every body.
	uses  map[uint64]struct{}
	owned map[uint64]struct{}
	files map[string]struct{}
}

// bodyCursor is the function body the walk is in.
type bodyCursor struct {
	// record is the body's record, by its index in bodyRecorder.records.
	record int
	// function is the node of the function the body is of.
	function nodeIndex
	// opening and closing are the body's braces. An object is a local of the body when its position lies between
	// them.
	opening token.Pos
	closing token.Pos
	// inSignature is whether the walk is still reading the function's type. go/types puts a body's immediate locals in
	// the function type's scope, so the walk sees them there, before it enters the body, among the parameters and the
	// results. Those are the signature's, which a build without the body still reads, so while this is set only what
	// has a local of the body for an end is recorded.
	inSignature bool
}

// declaredEnd is an object declared outside the bodies that name it: its place, and whether the package's
// definitions, asked for what is declared at that place, hand back this object.
type declaredEnd struct {
	place     recordedPlace
	isDefined bool
}

// localOfRecord is a local's record, by its index in bodyRecorder.records, and its index among that record's locals.
type localOfRecord struct {
	record int
	local  int
}

// recordBodies makes the graph record each function declaration's body as entry walks it. It is asked before entry,
// and bodyRecords gives the records once entry has returned.
func (graph *graph) recordBodies() {
	recorder := &bodyRecorder{
		locals:   map[nodeIndex]localOfRecord{},
		rooted:   map[nodeIndex]struct{}{},
		declared: map[nodeIndex]declaredEnd{},
		uses:     map[uint64]struct{}{},
		owned:    map[uint64]struct{}{},
		files:    map[string]struct{}{},
	}
	// The same test entry puts to each instance, so a body is flagged for exactly the instances entry would read.
	for identifier, instance := range graph.typesInfo.Instances {
		if named, isNamed := instance.Type.(*types.Named); isNamed && named.Obj().Pkg() == graph.typesPackage {
			if _, isInterface := named.Underlying().(*types.Interface); isInterface {
				recorder.interfaceInstances = append(recorder.interfaceInstances, identifier.Pos())
			}
		}
	}
	slices.Sort(recorder.interfaceInstances)
	graph.recorder = recorder
}

// bodyRecords is the record of each function declaration's body entry walked, in the order it walked them, and none
// for a graph that was not asked to record. Two bodies whose keys are one, in two files whose `//line` directives
// name one file, have no record, neither of them: a build asking by that key could be handed the other body's, so
// both are left for it to walk.
func (graph *graph) bodyRecords() []bodyRecord {
	if graph.recorder == nil {
		return nil
	}
	keyed := make(map[recordedPlace]int, len(graph.recorder.records))
	for _, record := range graph.recorder.records {
		keyed[record.Body]++
	}
	kept := make([]bodyRecord, 0, len(graph.recorder.records))
	for _, record := range graph.recorder.records {
		if keyed[record.Body] == 1 {
			kept = append(kept, record)
		}
	}
	return kept
}

// openBody opens the cursor on declaration's body, function being the node of the function it declares. It is opened
// before the function's type is read, where the walk sees the body's immediate locals.
func (graph *graph) openBody(declaration *ast.FuncDecl, function nodeIndex) {
	recorder := graph.recorder
	recorder.cursor = bodyCursor{
		record:      len(recorder.records),
		function:    function,
		opening:     declaration.Body.Lbrace,
		closing:     declaration.Body.Rbrace,
		inSignature: true,
	}
	recorder.isOpen = true
	// Each list is made empty and never nil, so a record with none of something encodes as it does on any run.
	recorder.records = append(recorder.records, bodyRecord{
		Body:   graph.keyOf(declaration),
		Locals: []recordedLocal{},
		Uses:   []recordedUse{},
		Rooted: []int{},
		Files:  []string{},
	})
}

// closeBody closes the cursor, the body walked, and settles what of its record the walk alone decides.
func (graph *graph) closeBody() {
	recorder := graph.recorder
	record := &recorder.records[recorder.cursor.record]
	for file := range recorder.files {
		record.Files = append(record.Files, file)
	}
	slices.Sort(record.Files)

	first, _ := slices.BinarySearch(recorder.interfaceInstances, recorder.cursor.opening)
	if first < len(recorder.interfaceInstances) && recorder.interfaceInstances[first] < recorder.cursor.closing {
		record.Present.InterfaceInstance = true
	}

	recorder.isOpen = false
	clear(recorder.uses)
	clear(recorder.owned)
	clear(recorder.files)
}

// keyOf is the key of declaration's body, as a standing body is keyed: the file a `//line` directive names at its
// `func`, or else the file's own name, and the offset of that `func` in the file as parsed.
func (graph *graph) keyOf(declaration *ast.FuncDecl) recordedPlace {
	tokenFile := graph.tokenFileOf(declaration.Pos())
	return recordedPlace{
		File:   tokenFile.Position(declaration.Pos()).Filename,
		Offset: tokenFile.Offset(declaration.Pos()),
	}
}

// placeOf is position as a record places an object: by its file's name as parsed, which no `//line` directive
// changes, and by the offset in that file.
func (graph *graph) placeOf(position token.Pos) recordedPlace {
	tokenFile := graph.tokenFileOf(position)
	return recordedPlace{File: tokenFile.Name(), Offset: tokenFile.Offset(position)}
}

// tokenFileOf is the file of the file set that position is in.
func (graph *graph) tokenFileOf(position token.Pos) *token.File {
	tokenFile := graph.fileSet.File(position)
	if tokenFile == nil {
		// Invariant: a record places only a function declaration of the package's syntax and an object the walk made
		// a node, each declared in a file of the file set.
		panic(fmt.Sprintf("a recorded position, %d, is in no file of the file set", position))
	}
	return tokenFile
}

// isLocalOfOpenBody is whether index's node is an object declared inside the open body's braces.
func (graph *graph) isLocalOfOpenBody(index nodeIndex) bool {
	if index == 0 {
		return false
	}
	position := graph.nodes[index].typesObject.Pos()
	return graph.recorder.cursor.opening < position && position < graph.recorder.cursor.closing
}

// endOf is index's node as an end of an edge in the open body's record. A local is numbered, and worded as a finding
// would word it, the first time the body meets it. A node is the generic origin of every instance of it (node), so
// a local generic type's field and each instantiated field a selection resolves to are one local.
func (graph *graph) endOf(index nodeIndex) recordedEnd {
	recorder := graph.recorder
	if index == 0 {
		return recordedEnd{Kind: endRoot}
	}
	if index == recorder.cursor.function {
		return recordedEnd{Kind: endFunction}
	}
	if placed, isRecorded := recorder.locals[index]; isRecorded && placed.record == recorder.cursor.record {
		return recordedEnd{Kind: endLocal, Local: placed.local}
	}
	if graph.isLocalOfOpenBody(index) {
		record := &recorder.records[recorder.cursor.record]
		worded := graph.objectOf(graph.nodes[index].typesObject)
		placed := localOfRecord{record: recorder.cursor.record, local: len(record.Locals)}
		record.Locals = append(record.Locals, recordedLocal{
			Place:  graph.placeOf(worded.position),
			Kind:   worded.kind,
			Name:   worded.name,
			Owners: []recordedEnd{},
		})
		recorder.locals[index] = placed
		return recordedEnd{Kind: endLocal, Local: placed.local}
	}
	declared, isPlaced := recorder.declared[index]
	if !isPlaced {
		typesObject := graph.nodes[index].typesObject
		declared.place = graph.placeOf(typesObject.Pos())
		declared.isDefined = graph.definedAt(declared.place) == typesObject
		recorder.declared[index] = declared
	}
	recorder.files[declared.place.File] = struct{}{}
	if !declared.isDefined {
		// The record still names the end by its place, so it says what the walk found. The flag is what keeps a
		// build from resolving it.
		recorder.records[recorder.cursor.record].Present.UnplacedEnd = true
	}
	return recordedEnd{Kind: endDeclared, Place: declared.place}
}

// definedAt is the object the package's declaring identifier at place declares, the generic one as a node holds it,
// and nil where no identifier declares one there or two do. The recorder asks it the question a splice asks to resolve
// a declared end (spliceBody), one function for both, so the host learns as it records, and not the build at its
// splice, that a place finds another object than the one the walk used. The definitions are indexed whole the first
// time it is asked.
func (graph *graph) definedAt(place recordedPlace) types.Object {
	if graph.defined == nil {
		// Each identifier is placed from its own file's start, which the syntax holds, so indexing asks the shared
		// file set once a file and not once an identifier. The files are sorted by where they start, so an
		// identifier's is the last that starts at or before it.
		type parsedFile struct {
			start token.Pos
			name  string
		}
		parsedFiles := make([]parsedFile, 0, len(graph.files))
		for _, file := range graph.files {
			parsedFiles = append(parsedFiles, parsedFile{
				start: file.FileStart,
				name:  graph.tokenFileOf(file.FileStart).Name(),
			})
		}
		slices.SortFunc(parsedFiles, func(one parsedFile, other parsedFile) int {
			return cmp.Compare(one.start, other.start)
		})
		graph.defined = make(map[recordedPlace]types.Object, len(graph.typesInfo.Defs))
		for identifier, defined := range graph.typesInfo.Defs {
			if defined == nil {
				// The package clause's name and a type switch's symbolic variable declare no object.
				continue
			}
			after, _ := slices.BinarySearchFunc(parsedFiles, identifier.Pos()+1,
				func(parsed parsedFile, at token.Pos) int {
					return cmp.Compare(parsed.start, at)
				})
			if after == 0 {
				// An identifier before every file of the package is in none of them, so no place names it.
				continue
			}
			parsed := parsedFiles[after-1]
			identifierPlace := recordedPlace{File: parsed.name, Offset: int(identifier.Pos() - parsed.start)}
			if _, isTaken := graph.defined[identifierPlace]; isTaken {
				// Two parsed files of one name: the place tells their identifiers apart for no build, so it finds
				// neither, whichever the map's order handed over first.
				graph.defined[identifierPlace] = nil
			} else {
				graph.defined[identifierPlace] = origin(defined)
			}
		}
	}
	return graph.defined[place]
}

// recordUse records that by's node uses used's, which use is about to add to the graph. In an open body it is one of
// the body's use edges. Between bodies only the root's use of a local is recorded, with the body the local is of: the
// steps after the walk that use every node of a generated file and every object a directive ignores.
func (graph *graph) recordUse(by nodeIndex, used nodeIndex) {
	recorder := graph.recorder
	if !recorder.isOpen {
		if by != 0 {
			return
		}
		placed, isLocal := recorder.locals[used]
		if !isLocal {
			return
		}
		if _, isRooted := recorder.rooted[used]; isRooted {
			return
		}
		recorder.rooted[used] = struct{}{}
		recorder.records[placed.record].Rooted = append(recorder.records[placed.record].Rooted, placed.local)
		return
	}
	if recorder.cursor.inSignature && !graph.isLocalOfOpenBody(used) && !graph.isLocalOfOpenBody(by) {
		return
	}
	key := edgeKey(by, used)
	if _, isRecorded := recorder.uses[key]; isRecorded {
		return
	}
	recorder.uses[key] = struct{}{}
	// The ends are met in the order use makes their nodes, the used one first, so the locals are numbered in the
	// order the graph numbers them.
	usedEnd := graph.endOf(used)
	byEnd := graph.endOf(by)
	record := &recorder.records[recorder.cursor.record]
	record.Uses = append(record.Uses, recordedUse{By: byEnd, Used: usedEnd})
}

// recordSeen records that the walk saw seen's node, owned by owner's, the root's when nothing owns it, which see is
// about to add to the graph.
func (graph *graph) recordSeen(seen nodeIndex, owner nodeIndex) {
	recorder := graph.recorder
	if !recorder.isOpen {
		return
	}
	if !graph.isLocalOfOpenBody(seen) {
		if recorder.cursor.inSignature {
			// A parameter, a result or a type parameter: the signature's own.
			return
		}
		// Invariant: a body's walk sees only what the body's own syntax declares, a name of one of its scopes, a
		// field, a method of an interface literal or a parameter of a function literal. A record has no place for an
		// own edge onto anything else, and a build would lose that edge, so the walk stops rather than drop it.
		panic(fmt.Sprintf("the body at %v saw %v, which it does not declare",
			recorder.records[recorder.cursor.record].Body, graph.nodes[seen].typesObject))
	}
	seenEnd := graph.endOf(seen)
	if owner == 0 {
		return
	}
	key := edgeKey(owner, seen)
	if _, isRecorded := recorder.owned[key]; isRecorded {
		return
	}
	recorder.owned[key] = struct{}{}
	ownerEnd := graph.endOf(owner)
	local := &recorder.records[recorder.cursor.record].Locals[seenEnd.Local]
	local.Owners = append(local.Owners, ownerEnd)
}

// recordWritten records the file written is declared in, a package-level variable whose file's name write read to
// judge whether writing to it is a use. The name decides the edge, so it is an input of the record whichever way it
// decided.
func (graph *graph) recordWritten(written types.Object) {
	if !graph.recorder.isOpen {
		return
	}
	graph.recorder.files[graph.placeOf(written.Pos()).File] = struct{}{}
}

// recordInterface records an interface type literal the walk read, interfaceType, which keeps the open body present
// when its method set holds an unexported method. One in the function's own signature is no part of the body.
func (graph *graph) recordInterface(interfaceType *types.Interface) {
	recorder := graph.recorder
	if !recorder.isOpen || recorder.cursor.inSignature {
		return
	}
	for method := range interfaceType.Methods() {
		if !method.Exported() {
			recorder.records[recorder.cursor.record].Present.UnexportedInterfaceMethod = true
			return
		}
	}
}

// recordNamedType records a named type the walk read the declaration of, typeName, which keeps the open body present
// when the type or its pointer has a method: entry reads both method sets of every named type.
func (graph *graph) recordNamedType(typeName *types.TypeName) {
	recorder := graph.recorder
	if !recorder.isOpen {
		return
	}
	if types.NewMethodSet(typeName.Type()).Len() != 0 ||
		types.NewMethodSet(types.NewPointer(typeName.Type())).Len() != 0 {
		recorder.records[recorder.cursor.record].Present.LocalMethodSet = true
	}
}

// recordIgnoredType records that useIgnored reached its type branch for the type name at index, whichever directive
// matched it, one on its line or one on its whole file, which keeps its body present when it is a body's local.
func (graph *graph) recordIgnoredType(index nodeIndex) {
	if placed, isLocal := graph.recorder.locals[index]; isLocal {
		graph.recorder.records[placed.record].Present.IgnoredType = true
	}
}

// recordAllFieldsUsed records that the walk used every field of a type, through the structs and arrays it holds,
// which keeps the open body present.
func (graph *graph) recordAllFieldsUsed() {
	if graph.recorder.isOpen {
		graph.recorder.records[graph.recorder.cursor.record].Present.AllFieldsUsed = true
	}
}
