package unused_code

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"math"
	"strings"
)

// bodySplice is the records a graph takes in place of walking their bodies (#xeafx79). An extra build that leaves a
// body to the host has no body to walk, so it makes what the host's walk of that body made from the host's record of
// it: a node for each local, each use and own edge, and each local the steps after the walk root. A graph holds one
// only when takeRecords handed it records, and without one the walk does what it always did.
type bodySplice struct {
	// records is each record taken, in the order takeRecords was handed them, which is the order they are spliced in.
	records []bodyRecord
	// byKey is each record's index in records, by the key of its body.
	byKey map[recordedPlace]int
	// functions is the node of the function each record's body is of, by the record's index, and met how many
	// function declarations the walk met at its key, which must be one. Both are filled as the walk meets them.
	functions []nodeIndex
	met       []int
	// locals is what a finding says of each node a record made, by its node, which holds no object (isSpliced).
	locals map[nodeIndex]object
	// parsedFiles is each file of the package by its name as parsed, nil for a name two files share, so a record's
	// place is found in this build's own file set. It is nil until a splice first places a local.
	parsedFiles map[string]*token.File
}

// takeRecords makes the graph take each of records in place of walking the body it is keyed by. It is asked before
// entry, which then walks none of those bodies and sees none of their locals, and splices each record in once every
// declaration is walked. A record that any flag keeps present cannot be taken: it holds what a record of positions
// cannot replay. Nor can two records of one key, nor any record by a graph that records, whose recorder places every
// node by its object, which a spliced node has none of.
func (graph *graph) takeRecords(records []bodyRecord) error {
	if graph.recorder != nil {
		return errors.New("a graph that records its bodies cannot take records too: a spliced local holds no object " +
			"for the recorder to place")
	}
	splice := &bodySplice{
		records:   records,
		byKey:     make(map[recordedPlace]int, len(records)),
		functions: make([]nodeIndex, len(records)),
		met:       make([]int, len(records)),
		locals:    map[nodeIndex]object{},
	}
	for index, record := range records {
		if record.Present != (recordedPresence{}) {
			return fmt.Errorf("the record of the body at %s is kept present, %+v, so no graph can take it",
				placeText(record.Body), record.Present)
		}
		if _, isTaken := splice.byKey[record.Body]; isTaken {
			return fmt.Errorf("two records are keyed by the body at %s", placeText(record.Body))
		}
		splice.byKey[record.Body] = index
	}
	graph.splice = splice
	return nil
}

// meetTaken is whether the graph takes declaration's body from a record, noting function as the node of the
// function the body is of when it does.
func (graph *graph) meetTaken(declaration *ast.FuncDecl, function nodeIndex) bool {
	key, isKeyed := graph.keyOf(declaration)
	if !isKeyed {
		// A declaration in no file of the file set has no key, and so no record.
		return false
	}
	index, isTaken := graph.splice.byKey[key]
	if !isTaken {
		return false
	}
	graph.splice.functions[index] = function
	graph.splice.met[index]++
	return true
}

// spliceBodies splices every record the graph took, in the order it took them.
func (graph *graph) spliceBodies() error {
	for index := range graph.splice.records {
		record := &graph.splice.records[index]
		if graph.splice.met[index] != 1 {
			return fmt.Errorf("the record of the body at %s is keyed where the package declares %d function bodies, "+
				"not one", placeText(record.Body), graph.splice.met[index])
		}
		if spliceError := graph.spliceBody(record, graph.splice.functions[index]); spliceError != nil {
			return spliceError
		}
	}
	return nil
}

// spliceBody makes what the walk of record's body made, function being the node of the function the body is of: a
// node for each local, each own edge and each use edge, and the root's use of each rooted local. An end it cannot
// resolve fails the splice by the body and the place, since a dropped edge would change the verdict unseen.
func (graph *graph) spliceBody(record *bodyRecord, function nodeIndex) error {
	locals := make([]nodeIndex, len(record.Locals))
	for index, local := range record.Locals {
		place, indexError := record.placeOf(local.Place)
		if indexError != nil {
			return fmt.Errorf("splicing the body at %s, local %d: %w", placeText(record.Body), index, indexError)
		}
		position, placeError := graph.positionAt(place)
		if placeError != nil {
			return fmt.Errorf("splicing the body at %s, local %d: %w", placeText(record.Body), index, placeError)
		}
		if uint64(local.Kind) >= uint64(len(kindWords)) {
			return fmt.Errorf("splicing the body at %s: local %d is of kind %d, which no word names",
				placeText(record.Body), index, local.Kind)
		}
		name, nameError := record.nameOf(index)
		if nameError != nil {
			return fmt.Errorf("splicing the body at %s: %w", placeText(record.Body), nameError)
		}
		if uint64(len(graph.nodes)) > math.MaxUint32 {
			return fmt.Errorf("splicing the body at %s: a package of more than %d objects", placeText(record.Body),
				uint64(math.MaxUint32))
		}
		locals[index] = nodeIndex(len(graph.nodes))
		graph.nodes = append(graph.nodes, node{})
		graph.firstOwners = append(graph.firstOwners, 0)
		graph.splice.locals[locals[index]] = object{
			name:      name,
			shortName: shortNameOf(name),
			kind:      local.Kind.word(),
			position:  position,
		}
	}
	for _, owned := range record.Owned {
		if uint64(owned.Local) >= uint64(len(locals)) {
			return fmt.Errorf("splicing the body at %s: it owns local %d of its %d", placeText(record.Body),
				owned.Local, len(locals))
		}
		ownerNode, resolveError := graph.endNode(record, owned.Owner, function, locals)
		if resolveError != nil {
			return fmt.Errorf("splicing the body at %s, an owner of local %d: %w", placeText(record.Body), owned.Local,
				resolveError)
		}
		if ownerNode == 0 {
			// The recorder records no owner of the root's (recordSeen), and the graph has no place for one
			// (addOwned).
			return fmt.Errorf("splicing the body at %s: the root owns local %d", placeText(record.Body), owned.Local)
		}
		graph.addOwned(ownerNode, locals[owned.Local])
	}
	for index, use := range record.Uses {
		byNode, byError := graph.endNode(record, use.By, function, locals)
		if byError != nil {
			return fmt.Errorf("splicing the body at %s, the user of use %d: %w", placeText(record.Body), index, byError)
		}
		usedNode, usedError := graph.endNode(record, use.Used, function, locals)
		if usedError != nil {
			return fmt.Errorf("splicing the body at %s, what use %d uses: %w", placeText(record.Body), index, usedError)
		}
		graph.addUse(byNode, usedNode)
	}
	for _, local := range record.Rooted {
		if uint64(local) >= uint64(len(locals)) {
			return fmt.Errorf("splicing the body at %s: it roots local %d of its %d", placeText(record.Body), local,
				len(locals))
		}
		graph.addUse(0, locals[local])
	}
	return nil
}

// endNode is the node end, an end of record's, names: the function, a local by its index among locals, the nodes the
// record's locals were made as, the root, or an object declared outside the body, which the package's own
// definitions are asked for by its place, as the recorder asked them (definedAt), and whose node is then the one the
// walk gives that object.
func (graph *graph) endNode(
	record *bodyRecord,
	end recordedEnd,
	function nodeIndex,
	locals []nodeIndex,
) (nodeIndex, error) {
	switch end.Kind {
	case endFunction:
		return function, nil
	case endRoot:
		return 0, nil
	case endLocal:
		if uint64(end.Local) >= uint64(len(locals)) {
			return 0, fmt.Errorf("local %d of its %d", end.Local, len(locals))
		}
		return locals[end.Local], nil
	case endDeclared:
		place, placeError := record.placeOf(end.Place)
		if placeError != nil {
			return 0, placeError
		}
		declared := graph.definedAt(place)
		if declared == nil {
			return 0, fmt.Errorf("an object declared at %s, where the package declares none or more than one",
				placeText(place))
		}
		return graph.node(declared), nil
	default:
		return 0, fmt.Errorf("an end of kind %d", end.Kind)
	}
}

// positionAt is place in the build's own file set: its file found by the name it was parsed under, and the offset
// in it. A record's offset is never negative (indexedPlace), so only its end is bounded.
func (graph *graph) positionAt(place recordedPlace) (token.Pos, error) {
	if graph.splice.parsedFiles == nil {
		graph.splice.parsedFiles = make(map[string]*token.File, len(graph.files))
		for _, file := range graph.files {
			tokenFile := graph.fileSet.File(file.FileStart)
			if tokenFile == nil {
				// A file the file set does not hold has no name a place could find it by.
				continue
			}
			if _, isNamed := graph.splice.parsedFiles[tokenFile.Name()]; isNamed {
				// Two parsed files of one name: the place tells them apart for no build, so it finds neither.
				graph.splice.parsedFiles[tokenFile.Name()] = nil
			} else {
				graph.splice.parsedFiles[tokenFile.Name()] = tokenFile
			}
		}
	}
	tokenFile := graph.splice.parsedFiles[place.File]
	if tokenFile == nil {
		return token.NoPos, fmt.Errorf("a place at %s, in no one parsed file of the package", placeText(place))
	}
	if place.Offset > tokenFile.Size() {
		return token.NoPos, fmt.Errorf("a place at %s, past the end of its file", placeText(place))
	}
	return tokenFile.Pos(place.Offset), nil
}

// placeText is place as a message names it, its file and its offset.
func placeText(place recordedPlace) string {
	return fmt.Sprintf("%s offset %d", place.File, place.Offset)
}

// shortNameOf is name bare of the receiver objectOf qualifies a method's with, `(*T).m` or `T.m`, which is all a
// dot in a name can be.
func shortNameOf(name string) string {
	return name[strings.LastIndex(name, ".")+1:]
}
