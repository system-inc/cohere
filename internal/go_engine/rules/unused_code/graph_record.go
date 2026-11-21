package unused_code

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"math"
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
	// parsedNames is how many of the package's files were parsed under each name (sharesParsedName), nil until a
	// local is first recorded.
	parsedNames map[string]int
	// interfaceInstances is where the package's source instantiates one of its own generic interface types, sorted,
	// the entries of typesInfo.Instances that entry adds to the interfaces a named type may implement.
	interfaceInstances []token.Pos

	// uses, owned and files are the open body's use edges and own edges, each keyed by edgeKey, and the files its
	// record names. They belong to the cursor, and live here so that one set of maps serves every body.
	uses  map[uint64]struct{}
	owned map[uint64]struct{}
	files map[string]struct{}
	// openLocals, openNames, openOwned, openUses and openPlaceFiles are the open body's record as the walk writes it,
	// and placeFileIndexes each file's index among openPlaceFiles. closeBody copies each into the record at its
	// length, since a run holds every record to its end and room an append left over would be held with it. They
	// belong to the cursor too, and one set serves every body.
	openLocals       []recordedLocal
	openNames        []byte
	openOwned        []recordedOwner
	openUses         []recordedUse
	openPlaceFiles   []string
	placeFileIndexes map[string]uint32
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

// declaredEnd is an object declared outside the bodies that name it: its place, whether it has one, in a file of the
// file set, and whether the package's definitions, asked for what is declared at that place, hand back this object.
type declaredEnd struct {
	place     recordedPlace
	isPlaced  bool
	isDefined bool
}

// localOfRecord is a local's record, by its index in bodyRecorder.records, and its index among that record's locals.
type localOfRecord struct {
	record int
	local  uint32
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

		placeFileIndexes: map[string]uint32{},
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
			// The rooted locals are appended after the walk, a body's at a time, so they are the one list closeBody
			// could not make at its length.
			record.Rooted = exactly(record.Rooted)
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
	key, isKeyed := graph.keyOf(declaration)
	recorder.records = append(recorder.records, bodyRecord{Body: key})
	if !isKeyed {
		// A body in no file of the file set has no key a build could ask for it by, so it is keyed by no place, and
		// kept present.
		recorder.records[recorder.cursor.record].Present.UnplacedEnd = true
	}
}

// closeBody closes the cursor, the body walked, and settles what of its record the walk alone decides.
func (graph *graph) closeBody() {
	recorder := graph.recorder
	record := &recorder.records[recorder.cursor.record]
	record.PlaceFiles = exactly(recorder.openPlaceFiles)
	record.Locals = exactly(recorder.openLocals)
	record.Names = string(recorder.openNames)
	record.Owned = exactly(recorder.openOwned)
	record.Uses = exactly(recorder.openUses)
	if len(recorder.files) > 0 {
		record.Files = make([]string, 0, len(recorder.files))
		for file := range recorder.files {
			record.Files = append(record.Files, file)
		}
		slices.Sort(record.Files)
	}

	first, _ := slices.BinarySearch(recorder.interfaceInstances, recorder.cursor.opening)
	if first < len(recorder.interfaceInstances) && recorder.interfaceInstances[first] < recorder.cursor.closing {
		record.Present.InterfaceInstance = true
	}

	recorder.isOpen = false
	clear(recorder.uses)
	clear(recorder.owned)
	clear(recorder.files)
	recorder.openLocals = recorder.openLocals[:0]
	recorder.openNames = recorder.openNames[:0]
	recorder.openOwned = recorder.openOwned[:0]
	recorder.openUses = recorder.openUses[:0]
	recorder.openPlaceFiles = recorder.openPlaceFiles[:0]
	clear(recorder.placeFileIndexes)
}

// exactly is a copy of list as long as it and no longer, nil for an empty one.
func exactly[Element any](list []Element) []Element {
	if len(list) == 0 {
		return nil
	}
	copied := make([]Element, len(list))
	copy(copied, list)
	return copied
}

// indexedPlaceOf is place as the open body's record holds it, its file named by its index among the record's
// PlaceFiles, which it joins the first time a place of the body is in it. It is false for an offset past what an
// indexedPlace counts, in a file of four billion bytes, which no build could take a place in either.
func (graph *graph) indexedPlaceOf(place recordedPlace) (indexedPlace, bool) {
	if place.Offset < 0 || uint64(place.Offset) > math.MaxUint32 {
		return indexedPlace{}, false
	}
	recorder := graph.recorder
	file, isIndexed := recorder.placeFileIndexes[place.File]
	if !isIndexed {
		// A body places nothing in more files than its package has, so the index is far inside 32 bits.
		file = uint32(len(recorder.openPlaceFiles))
		recorder.openPlaceFiles = append(recorder.openPlaceFiles, place.File)
		recorder.placeFileIndexes[place.File] = file
	}
	return indexedPlace{File: file, Offset: uint32(place.Offset)}, true
}

// keyOf is the key of declaration's body, as a standing body is keyed: the file a `//line` directive names at its
// `func`, or else the file's own name, and the offset of that `func` in the file as parsed. It is false for a
// declaration in no file of the file set, which has no key.
func (graph *graph) keyOf(declaration *ast.FuncDecl) (recordedPlace, bool) {
	tokenFile := graph.fileSet.File(declaration.Pos())
	if tokenFile == nil {
		return recordedPlace{}, false
	}
	return recordedPlace{
		File:   tokenFile.Position(declaration.Pos()).Filename,
		Offset: tokenFile.Offset(declaration.Pos()),
	}, true
}

// placeOf is position as a record places an object: by its file's name as parsed, which no `//line` directive
// changes, and by the offset in that file. It is false for a position in no file of the file set, which no place
// names. The package's syntax and every object the walk makes a node are in its files, so none should be, but a
// record flags such a body rather than stop the host's run (UnplacedEnd).
func (graph *graph) placeOf(position token.Pos) (recordedPlace, bool) {
	tokenFile := graph.fileSet.File(position)
	if tokenFile == nil {
		return recordedPlace{}, false
	}
	return recordedPlace{File: tokenFile.Name(), Offset: tokenFile.Offset(position)}, true
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
		typesObject := graph.nodes[index].typesObject
		worded := graph.objectOf(typesObject)
		// A local is a node, and a node's index is 32 bits, so a body has no more locals than the index counts.
		placed := localOfRecord{record: recorder.cursor.record, local: uint32(len(recorder.openLocals))}
		place, isInFile := graph.placeOf(worded.position)
		indexed, isIndexed := graph.indexedPlaceOf(place)
		recorder.openNames = append(recorder.openNames, worded.name...)
		recorder.openLocals = append(recorder.openLocals, recordedLocal{
			Place: indexed,
			// One body's locals' names come to far fewer bytes than the four billion a 32-bit offset counts.
			NameEnd: uint32(len(recorder.openNames)),
			Kind:    kindOf(typesObject),
		})
		// A build places a spliced local in its own file set by the name its file was parsed under, which must be one
		// file's alone there (positionAt): flagged here, as an end its place does not find is, rather than failing
		// the build's splice.
		if !isInFile || !isIndexed || graph.sharesParsedName(place.File) {
			recorder.records[recorder.cursor.record].Present.UnplacedEnd = true
		}
		recorder.locals[index] = placed
		return recordedEnd{Kind: endLocal, Local: placed.local}
	}
	declared, isAsked := recorder.declared[index]
	if !isAsked {
		typesObject := graph.nodes[index].typesObject
		declared.place, declared.isPlaced = graph.placeOf(typesObject.Pos())
		declared.isDefined = declared.isPlaced && graph.definedAt(declared.place) == typesObject
		recorder.declared[index] = declared
	}
	if declared.isPlaced {
		recorder.files[declared.place.File] = struct{}{}
	}
	indexed, isIndexed := graph.indexedPlaceOf(declared.place)
	if !declared.isDefined || !isIndexed {
		// The record still names the end by its place, so it says what the walk found. The flag is what keeps a
		// build from resolving it.
		recorder.records[recorder.cursor.record].Present.UnplacedEnd = true
	}
	return recordedEnd{Kind: endDeclared, Place: indexed}
}

// sharesParsedName is whether two of the package's files were parsed under name, so no place in either can say
// which file it is in. The names are counted once a graph, the first time it is asked.
func (graph *graph) sharesParsedName(name string) bool {
	recorder := graph.recorder
	if recorder.parsedNames == nil {
		recorder.parsedNames = make(map[string]int, len(graph.files))
		for _, file := range graph.files {
			// A file the file set does not hold places nothing (placeOf), so it shares no name.
			if tokenFile := graph.fileSet.File(file.FileStart); tokenFile != nil {
				recorder.parsedNames[tokenFile.Name()]++
			}
		}
	}
	return recorder.parsedNames[name] > 1
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
		// A file the file set does not hold keeps its start, so its identifiers are found to be its own, and is
		// unnamed, so they are placed nowhere, as placeOf places nothing in it.
		type parsedFile struct {
			start   token.Pos
			name    string
			isNamed bool
		}
		parsedFiles := make([]parsedFile, 0, len(graph.files))
		for _, file := range graph.files {
			parsed := parsedFile{start: file.FileStart}
			if tokenFile := graph.fileSet.File(file.FileStart); tokenFile != nil {
				parsed.name, parsed.isNamed = tokenFile.Name(), true
			}
			parsedFiles = append(parsedFiles, parsed)
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
			if !parsed.isNamed {
				continue
			}
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
	recorder.openUses = append(recorder.openUses, recordedUse{By: byEnd, Used: usedEnd})
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
		// A body's walk sees only what the body's own syntax declares, a name of one of its scopes, a field, a method
		// of an interface literal or a parameter of a function literal, so this should not be reached. A record has
		// no place for an own edge onto anything else, and a build would lose that edge, so the body is kept present
		// rather than recorded without it, and the host's run goes on.
		recorder.records[recorder.cursor.record].Present.UnplacedEnd = true
		return
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
	recorder.openOwned = append(recorder.openOwned, recordedOwner{Local: seenEnd.Local, Owner: ownerEnd})
}

// recordWritten records the file written is declared in, a package-level variable whose file's name write read to
// judge whether writing to it is a use. The name decides the edge, so it is an input of the record whichever way it
// decided. Only a variable of this package is: one a dot import brings in is never a node, so its file decides no
// edge, and a file of another package in the record would only keep a build from taking it.
func (graph *graph) recordWritten(written types.Object) {
	if !graph.recorder.isOpen || written.Pkg() != graph.typesPackage {
		return
	}
	place, isInFile := graph.placeOf(written.Pos())
	if !isInFile {
		// The record cannot name the file that decided the edge, so a build could not check it.
		graph.recorder.records[graph.recorder.cursor.record].Present.UnplacedEnd = true
		return
	}
	graph.recorder.files[place.File] = struct{}{}
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
