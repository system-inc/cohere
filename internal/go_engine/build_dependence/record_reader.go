package build_dependence

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Record is a package's record decoded from its bytes, for a caller that decodes it apart from adding it to a graph
// (Graph.AddDecoded): the findings cache decodes each record it reads beside the entries, so one that does not decode
// is its own to say and to write again (#6a3kjvz).
type Record struct {
	record *packageRecord
}

// DecodeRecord is the record encoded holds (EncodedRecord), or why it holds none.
func DecodeRecord(encoded []byte) (*Record, error) {
	record, decodeError := decodeRecord(encoded)
	if decodeError != nil {
		return nil, decodeError
	}
	return &Record{record: record}, nil
}

// errorRecordShort is a record whose bytes end before it does.
var errorRecordShort = errors.New("the record ends early")

// recordReader reads one record: its bytes, where it is, its string table, and the first error it met, after which
// every read returns zero.
type recordReader struct {
	data    []byte
	at      int
	strings []string
	failed  error
}

// number reads one varint.
func (reader *recordReader) number() uint64 {
	if reader.failed != nil {
		return 0
	}
	value, length := binary.Uvarint(reader.data[reader.at:])
	if length <= 0 {
		reader.failed = errorRecordShort
		return 0
	}
	reader.at += length
	return value
}

// count reads a count, refused when it is more than the bytes left could hold, so a damaged record never asks for
// more memory than it is long.
func (reader *recordReader) count() int {
	value := reader.number()
	if value > uint64(len(reader.data)-reader.at) {
		reader.failed = errorRecordShort
		return 0
	}
	return int(value)
}

// raw reads one text written whole: its length, then its bytes.
func (reader *recordReader) raw() string {
	length := reader.count()
	if reader.failed != nil {
		return ""
	}
	value := string(reader.data[reader.at : reader.at+length])
	reader.at += length
	return value
}

// text reads one index into the string table.
func (reader *recordReader) text() string {
	index := reader.number()
	if reader.failed != nil {
		return ""
	}
	if index >= uint64(len(reader.strings)) {
		reader.failed = fmt.Errorf("the record names text %d of %d", index, len(reader.strings))
		return ""
	}
	return reader.strings[index]
}

// flag reads one flag.
func (reader *recordReader) flag() bool {
	return reader.number() == 1
}

// object reads an object's path and local name.
func (reader *recordReader) object() recordedObject {
	return recordedObject{path: reader.text(), local: reader.text()}
}

// decodeRecord is the record encoded holds, with no live object, or why it holds none: another format, or bytes that
// end early or name what the record does not hold.
func decodeRecord(encoded []byte) (*packageRecord, error) {
	head := append([]byte(recordFormat), 0)
	if len(encoded) < len(head) || string(encoded[:len(head)]) != string(head) {
		return nil, errors.New("decoding a dependence record: it is in another format")
	}
	reader := &recordReader{data: encoded, at: len(head)}
	record := &packageRecord{identifier: reader.raw(), path: reader.raw(), workspace: reader.flag()}
	reader.strings = make([]string, reader.count())
	for index := range reader.strings {
		reader.strings[index] = reader.raw()
	}
	imports := reader.count()
	record.imports = make(map[string]string, imports)
	for range imports {
		path := reader.text()
		record.imports[path] = reader.text()
	}
	record.typeNames = make([]string, reader.count())
	for index := range record.typeNames {
		record.typeNames[index] = reader.text()
	}
	record.objects = make([]recordedObject, reader.count())
	for index := range record.objects {
		record.objects[index] = reader.object()
	}
	record.units = make([]recordedUnit, reader.count())
	for index := range record.units {
		record.units[index] = reader.unit(record.path, len(record.objects))
	}
	record.edges = make([]recordedEdge, reader.count())
	for index := range record.edges {
		edge := recordedEdge{from: reader.text()}
		flags := reader.number()
		edge.fromUnderlying, edge.toUnderlying = flags&1 != 0, flags&2 != 0
		edge.to = reader.index(len(record.objects))
		record.edges[index] = edge
	}
	if reader.failed != nil {
		return nil, fmt.Errorf("decoding the dependence record of %s: %w", record.identifier, reader.failed)
	}
	if reader.at != len(reader.data) {
		return nil, fmt.Errorf("decoding the dependence record of %s: %d bytes follow it", record.identifier,
			len(reader.data)-reader.at)
	}
	return record, nil
}

// index reads an index into a table of count entries.
func (reader *recordReader) index(count int) int32 {
	value := reader.number()
	if reader.failed == nil && value >= uint64(count) {
		reader.failed = fmt.Errorf("the record names object %d of %d", value, count)
	}
	return int32(value)
}

// unit reads one unit of the package at path, whose record's table holds objects entries. A unit whose start, end or
// size passes what a unit column holds (column) is refused here, a number no source file under 2 GiB gives: added to a
// graph, the record would panic every run that resolves it until the store let it go, where a record refused is the
// findings cache's to say and to write again (Record).
func (reader *recordReader) unit(path string, objects int) recordedUnit {
	unit := recordedUnit{file: reader.text()}
	start, length, nodes := reader.number(), reader.number(), reader.number()
	if reader.failed == nil && (start > math.MaxInt32 || length > math.MaxInt32-start || nodes > math.MaxInt32) {
		reader.failed = fmt.Errorf("the record places a unit at %d, %d bytes long, of %d nodes, past what a unit "+
			"column holds", start, length, nodes)
	}
	unit.start, unit.end, unit.nodes = int(start), int(start+length), int(nodes)
	unit.universeNames = reader.number()
	unitFlags := reader.number()
	unit.fromCgo = unitFlags&2 != 0
	unit.withheld = unitFlags&4 != 0
	if unitFlags&1 != 0 {
		unit.receiver = reader.text()
	}
	unit.declares = make([]recordedObject, reader.count())
	for index := range unit.declares {
		unit.declares[index] = recordedObject{path: path, local: reader.text()}
	}
	unit.reads = make([]recordedRead, reader.count())
	for index := range unit.reads {
		flags := reader.number()
		read := recordedRead{target: -1, underlying: flags&2 != 0}
		if flags&1 != 0 {
			read.target = reader.index(objects)
		}
		read.through = make([]recordedBit, reader.count())
		for at := range read.through {
			read.through[at] = recordedBit{object: reader.index(objects), underlying: reader.flag()}
		}
		unit.reads[index] = read
	}
	unit.callees = make([]int32, reader.count())
	for index := range unit.callees {
		unit.callees[index] = reader.index(objects)
	}
	return unit
}
