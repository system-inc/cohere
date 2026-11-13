package build_dependence

import (
	"encoding/binary"
	"maps"
	"slices"
)

// recordFormat opens every encoded record and names its format, so bytes of another format are refused rather than
// read wrong.
const recordFormat = "cohere dependence record 1"

// The encoded record (#6a3kjvz), after recordFormat, every number an unsigned varint and every text an index into the
// string table, the table itself each text's length and its bytes: the package's ID, path, whether it is the
// workspace's; the string table; the import map, each import path and ID, by path; the type names; the object table,
// each object its path then its local name; the units, each as its file, start, length, nodes, universe-name bits, its
// receiver's local name plus one, 0 for none, its declares' local names, its reads and its callees; and the kind
// edges. Reads, callees and edges name an object by its index in the table. A read is its flags (1 has a target, 2
// touches the underlying), its target when it has one, then its expansion, each bit an object and its underlying
// flag. An edge is its from local name, its flags (1 from underlying, 2 to underlying) and its to object. The
// unnameable count is no part of it: a record that met one is the collecting run's to report.

// recordWriter writes one record: its strings, each once, by index, and its body, after them.
type recordWriter struct {
	strings map[string]int
	ordered []string
	body    []byte
}

// number writes value.
func (writer *recordWriter) number(value uint64) {
	writer.body = binary.AppendUvarint(writer.body, value)
}

// text writes value's index in the string table, adding it on its first use.
func (writer *recordWriter) text(value string) {
	index, isKnown := writer.strings[value]
	if !isKnown {
		index = len(writer.ordered)
		writer.strings[value] = index
		writer.ordered = append(writer.ordered, value)
	}
	writer.number(uint64(index))
}

// flag writes value as 1 or 0.
func (writer *recordWriter) flag(value bool) {
	if value {
		writer.number(1)
		return
	}
	writer.number(0)
}

// object writes an object's path and local name.
func (writer *recordWriter) object(object recordedObject) {
	writer.text(object.path)
	writer.text(object.local)
}

// encodeRecord is record's bytes (see recordFormat).
func encodeRecord(record *packageRecord) []byte {
	writer := &recordWriter{strings: map[string]int{}}
	writer.number(uint64(len(record.imports)))
	for _, path := range slices.Sorted(maps.Keys(record.imports)) {
		writer.text(path)
		writer.text(record.imports[path])
	}
	writer.number(uint64(len(record.typeNames)))
	for _, name := range record.typeNames {
		writer.text(name)
	}
	writer.number(uint64(len(record.objects)))
	for _, object := range record.objects {
		writer.object(object)
	}
	writer.number(uint64(len(record.units)))
	for index := range record.units {
		unit := &record.units[index]
		writer.text(unit.file)
		writer.number(uint64(unit.start))
		writer.number(uint64(unit.end - unit.start))
		writer.number(uint64(unit.nodes))
		writer.number(unit.universeNames)
		if unit.receiver == "" {
			writer.number(0)
		} else {
			writer.number(1)
			writer.text(unit.receiver)
		}
		writer.number(uint64(len(unit.declares)))
		for _, declared := range unit.declares {
			writer.text(declared.local)
		}
		writer.number(uint64(len(unit.reads)))
		for _, read := range unit.reads {
			flags := uint64(0)
			if read.target >= 0 {
				flags |= 1
			}
			if read.underlying {
				flags |= 2
			}
			writer.number(flags)
			if read.target >= 0 {
				writer.number(uint64(read.target))
			}
			writer.number(uint64(len(read.through)))
			for _, reached := range read.through {
				writer.number(uint64(reached.object))
				writer.flag(reached.underlying)
			}
		}
		writer.number(uint64(len(unit.callees)))
		for _, callee := range unit.callees {
			writer.number(uint64(callee))
		}
	}
	writer.number(uint64(len(record.edges)))
	for _, edge := range record.edges {
		writer.text(edge.from)
		flags := uint64(0)
		if edge.fromUnderlying {
			flags |= 1
		}
		if edge.toUnderlying {
			flags |= 2
		}
		writer.number(flags)
		writer.number(uint64(edge.to))
	}
	encoded := append([]byte(recordFormat), 0)
	head := &recordWriter{}
	for _, value := range []string{record.identifier, record.path} {
		head.number(uint64(len(value)))
		head.body = append(head.body, value...)
	}
	head.flag(record.workspace)
	head.number(uint64(len(writer.ordered)))
	for _, value := range writer.ordered {
		head.number(uint64(len(value)))
		head.body = append(head.body, value...)
	}
	encoded = append(encoded, head.body...)
	return append(encoded, writer.body...)
}
