package main

import (
	"fmt"

	"github.com/system-inc/cohere/internal/types/program"
)

// typesRecord is the types section's own reading and writing, for a run with no recording to carry it: any
// flag, any named path, the editor's save of one file (#hfv0ae3). Those runs paid the full incremental
// session, about 180ms reading the build info and 150ms rewriting it, whether they checked 84 files or
// 3,889. The section is keyed per file on the shape fingerprint and on the compiler options, never on the
// arguments, so a scoped run may replay what a bare run recorded and the other way round.
type typesRecord struct {
	directory string
	anchor    program.PathAnchor
	reuse     *program.TypeDiagnosticsReuse

	// shapes is every project file's shape this run, kept for the next when computedShapes says any was
	// computed rather than carried from the table.
	shapes         map[string]program.SignatureEntry
	computedShapes int
	// shapesGraph is the graph shapes describe. See runCacheSession.shapesGraph.
	shapesGraph *program.Graph
}

// write puts the section, and the shapes when any was computed, into their files, and writes nothing when
// neither moved. The caller holds the table's lock.
func (record *typesRecord) write() {
	if record.reuse.Unchanged() && record.computedShapes == 0 {
		return
	}
	table := program.NewCacheTable()
	var sections program.CacheTableSections
	if recorded := record.reuse.Recorded(); recorded != nil {
		table.Types = recorded
		sections.Types = true
	}
	if record.computedShapes > 0 {
		table.Signatures = record.shapes
		sections.Signatures = true
	}
	if writeCacheTableError := program.WriteCacheTable(
		record.directory,
		table,
		cacheTableIdentity(),
		sections,
		record.anchor,
	); writeCacheTableError != nil {
		cacheNote(
			record.directory,
			fmt.Sprintf("the cache table could not be written: %v", firstLine(writeCacheTableError.Error())),
		)
	}
}
