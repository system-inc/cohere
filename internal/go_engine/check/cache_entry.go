package check

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"errors"
	"fmt"
	"go/ast"
	"maps"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/copied"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// CacheFormatVersion is the version of the cache entry's format, which the engine hashes into every key as the
// cache's own, so an entry written in another format is never read. It changes whenever cacheEntry or anything it
// holds changes shape, Finding included, or what a field means: at 5, Output became a digest of the package's API and
// facts by value rather than of their bytes (#z14ch7c), so an entry written at 4 holds a digest no second key of 5
// can match, and would only be a miss read the long way. At 6, an analysis holds its report analyzer's result
// (#jfvhbj7). At 7, an entry holds the answers a standard-library package was computed whole in (#w7zmvat wave 3).
// At 8, it holds the answers its package's analysis read of other packages (#w7zmvat wave 3b). At 9, a standard-library
// package's entry holds every answer of its package whenever a run writes it, so an entry of 8 may hold only some. At
// 10, every package's entry does, so an entry of 9 may hold only some for a package outside the standard library. At
// 11, an entry names its export data by the digest of its bytes, which the store keeps once under that digest however
// many entries name it, where an entry of 10 found its own beside it under its key (#5s1x2hg). At 12, the store keeps
// an entry as a record under its key (entryRecord), its reads and export data's digest beside its body, or, for an
// entry an extra build writes through, a reference to the record of the host's that holds the body (#fq7dxvx); a key
// of 11 held the whole entry.
const CacheFormatVersion = "go-engine-cache-12"

// errorUnstorable is a package an entry cannot hold, a fact gob refuses or factValueDigest cannot read, or types export
// data cannot carry, so it gets no entry, and is counted.
var errorUnstorable = errors.New("what the entry cannot hold")

// cacheEntry is one package as the cache holds it, gob-encoded: by slot, what each analyzer that a later run could
// ask for reported and exported on the package, and what the load needs to give the package's types without
// parsing it. An analyzer's slot is its rule's name, or for an analyzer no rule runs, its name and its fact types
// (see newCacheAnalyzers). The store keeps it as a record (entryRecord): its Reads and ReadsIncomplete beside the rest,
// its body, which the record holds or names in another record. The package's export data, most of its bytes, which the
// load reads only for a package something that missed imports, is the record's to name by digest, and the store keeps
// it once under exportDataKey of that.
type cacheEntry struct {
	// Version is CacheFormatVersion as it was when the entry was written, read back as a check on the key's.
	Version string

	Analyzers map[string]storedAnalysis

	// Imports and Withheld are the load's, as load.Restored names them.
	Imports  []string
	Withheld load.PackageWithheld

	// Files are the package's files as the rules were given them, for a package Analyze was given, whose walk the
	// lint record counts and whose generated and copied files withhold a house rule's findings.
	Files []storedFile

	// Output is the package's output digest (see outputDigest), what an importer can read of it, or nil for an entry
	// that holds no facts, which therefore cannot say what an importer would inherit.
	Output []byte

	// Answers are the answers of the fact kinds computed on demand (#w7zmvat wave 3) of the package, which a run that
	// restores it reads in place of a build: every answer, computed whole, whenever the run that wrote it could
	// compute them all without a reload, and otherwise those the runs which checked the package from source computed
	// in it (see demandOf). They depend only on the package's key, as everything else here does.
	Answers []kindAnswers

	// Reads are the answers of the fact kinds computed on demand that the package's analysis took from other packages,
	// in the runs that wrote its findings and answers, each with its owner's first key then (#w7zmvat wave 3b). A run
	// that restores the package by anything but its own first key holds the entry only while every read still holds
	// (see readsHold). ReadsIncomplete is set when a read could not be named, an object with no objectpath or an owner
	// the run did not key, and then the entry is held by its own first key alone.
	Reads           []storedRead
	ReadsIncomplete bool

	// Bodiless are the objectpaths of the functions and methods the package declares without a body, and of their
	// parameters and results, as the load that checked it from source recorded them (load.Bodiless), and
	// BodiesRecorded is whether it did. A run that restores the package answers a question of one from its types
	// alone, where an object it holds no answer of may be a function with a body it never loaded, so it reloads the
	// package (#b2t9xmf). An entry written before these fields decodes with BodiesRecorded false, and its package
	// reloads as it always did, so they need no new CacheFormatVersion.
	Bodiless       []objectpath.Path
	BodiesRecorded bool
}

// entryRecord is what the store keeps under a package's key (#fq7dxvx): the format, the entry's body, the sha256 of
// the body, and the entry's Reads and ReadsIncomplete, which name each read's owner by its key in the build that wrote
// it, so they differ between builds whose bodies are one.
//
// A record written from source holds its body (Inline). An extra build writing a package through from the host's run
// holds none: its record names, by BodyAt, the key of the host's record that holds the body, and the body by its
// digest, which that record must still hold, so the build writes neither the body nor a file of its own for it. A
// reader that finds the record under BodyAt gone, or holding another body, as the host's next run may rewrite it with
// more answers, reads a miss: the package is checked from source and its entry written whole. Every body a reference
// names is checked against the record it reads, field by field, so nothing is hashed on a read.
//
// ExportData is the sha256 of the package's export data, which the store keeps under exportDataKey of it, nil for a
// package that carries none (carriesExportData) and for an entry encoded without it, which only a run's memory holds
// (lazyExportData). It is the record's, not the body's, so a body kept without it, as the binary's table keeps one, is
// still the one an entry written through names (#5s1x2hg): one cold run at cohere's root wrote 562 MB of export data
// through, 98 MB of it distinct.
type entryRecord struct {
	Version         string
	Body            []byte
	Inline          []byte
	BodyAt          []byte
	ExportData      []byte
	Reads           []storedRead
	ReadsIncomplete bool
}

// storedRead is one read (see fact_resolver.Read): the kind, the owner's ID and its first key when the read was
// made, the object's objectpath in the owner, and the answer, as JSON.
type storedRead struct {
	Kind     string
	Owner    string
	OwnerKey []byte
	Path     objectpath.Path
	Value    []byte
}

// storedFile is one file of a package Analyze was given: its name as the syntax names it, the nodes of one
// ast.Inspect over it, and whether it is generated or a copy of upstream source.
type storedFile struct {
	Name      string
	Nodes     int
	Generated bool
	Copied    bool
}

// storedAnalysis is one analyzer on one package: its findings, as newFinding makes them before the dedupe and the
// withholding, and the facts the package itself exported that an importer inherits. For a report analyzer (see
// Options.Report), Report is its result, gob-encoded, where the others hold nothing.
type storedAnalysis struct {
	Findings     []Finding
	ObjectFacts  []storedObjectFact
	PackageFacts []storedFact
	Report       []byte
}

// storedObjectFact is a fact about one object of the package: the object as its objectpath within the package, and
// the fact.
type storedObjectFact struct {
	Path objectpath.Path
	Fact storedFact
}

// storedFact is one fact as its type's name, which factTypeName gives, and its gob bytes. Each fact is encoded on
// its own as its concrete type, as checker's codeFact encodes it, so no fact type need be registered with gob.
type storedFact struct {
	Type    string
	Encoded []byte
}

// restoredPackage is a package's entry decoded whole: its facts are values, and become facts about this run's
// objects when an action on the package restores them, once the load has given the package its types.
type restoredPackage struct {
	analyses map[*analysis.Analyzer]restoredAnalysis
	types    *load.Restored
	files    []storedFile

	// output is the entry's Output, and hasOutput whether it had one.
	output    cache.Key
	hasOutput bool

	// answers are the entry's Answers, reads and readsIncomplete its Reads and ReadsIncomplete, bodiless and
	// bodiesRecorded its Bodiless and BodiesRecorded, exportDigest its record's ExportData, bodyDigest the digest its
	// body is kept under, and slots the slots its body holds, every one, known to this run or not.
	answers         []kindAnswers
	reads           []storedRead
	readsIncomplete bool
	bodiless        []objectpath.Path
	bodiesRecorded  bool
	exportDigest    []byte
	bodyDigest      []byte
	slots           []string
}

// encodedEntry is one package's entry as encodeEntry makes it, with what the session needs of it beside the bytes: its
// body's bytes, its record, which holds or names them, and its export data. An entry written through from the host's
// run has no body of its own (nil): its record names the host's (throughEntry).
type encodedEntry struct {
	body       []byte
	record     entryRecord
	exportData []byte

	// dropped counts the facts left out because their object has no objectpath.
	dropped int

	// output is the package's output digest, and hasOutput whether it has one: an entry without facts has none.
	output    cache.Key
	hasOutput bool

	// positionDependent is set when a finding of the package names a file outside it, so its findings can change
	// with a line of another package (see positionDependent).
	positionDependent bool
}

// restoredAnalysis is a storedAnalysis decoded.
type restoredAnalysis struct {
	findings     []Finding
	objectFacts  []pathFact
	packageFacts []analysis.Fact

	// report is a report analyzer's result, decoded into its ResultType, nil for any other analyzer.
	report any
}

// pathFact is a decoded fact about the object at an objectpath of its package.
type pathFact struct {
	path objectpath.Path
	fact analysis.Fact
}

// factTypeName names a fact's type, a pointer to a named type, by its package path and name, which is what an
// entry calls it.
func factTypeName(factType reflect.Type) string {
	named := factType
	if named.Kind() == reflect.Pointer {
		named = named.Elem()
	}
	return named.PkgPath() + "." + named.Name()
}

// encodeEntry is the entry of one package that missed, from every action on it, each of which finished without an
// error, its types, what the load withheld of it, the answers of the fact kinds computed on demand in it, those its
// analysis read of other packages and what it declares without a body (demanded), and, for a package Analyze was
// given, its files' walk, with its export data beside it, its output digest, and whether a finding of it names a file
// outside it, among loadFiles, every file the load holds. It returns errorUnstorable, wrapped, when gob refuses a fact
// the package exported, or factValueDigest cannot read one, or its types cannot be written as export data. Without
// writesExportData the entry is encoded alone, for a caller that writes the export data itself only if it is ever read
// (lazyExportData).
func encodeEntry(
	known *cacheAnalyzers,
	ruleOf map[*analysis.Analyzer]rule.Rule,
	saved *packages.Package,
	actions []*checker.Action,
	withheld load.PackageWithheld,
	demanded entryDemand,
	files []storedFile,
	loadFiles *knownFiles,
	writesExportData bool,
) (encodedEntry, error) {
	entry := cacheEntry{
		Version:        CacheFormatVersion,
		Analyzers:      map[string]storedAnalysis{},
		Withheld:       withheld,
		Files:          files,
		Answers:        demanded.answers,
		Bodiless:       demanded.bodiless,
		BodiesRecorded: demanded.bodiesRecorded,
	}
	encoded := encodedEntry{}
	digested := map[string]analysisFacts{}
	var encoder objectpath.Encoder
	for _, action := range actions {
		slot, held := known.slot(action.Analyzer)
		if !held {
			continue
		}
		stored := storedAnalysis{}
		facts := analysisFacts{}
		if actionRule, isRule := ruleOf[action.Analyzer]; isRule {
			for _, diagnostic := range action.Diagnostics {
				stored.Findings = append(stored.Findings, newFinding(actionRule, action.Package.Fset, diagnostic))
			}
		}
		if known.reports[action.Analyzer] {
			var report bytes.Buffer
			if gobError := gob.NewEncoder(&report).Encode(action.Result); gobError != nil {
				return encodedEntry{}, fmt.Errorf("storing %s's result: %w: %w", action.Analyzer.Name,
					errorUnstorable, gobError)
			}
			stored.Report = report.Bytes()
		}
		objectFacts, packageFacts := action.ExportedFacts()
		for _, objectFact := range objectFacts {
			path, pathError := encoder.For(objectFact.Object)
			if pathError != nil {
				// An object with no objectpath is one no other package can name, such as the local closure printf
				// marks as a wrapper in `fail := func(format string, arguments ...any)`, which exportedFrom passes
				// since it cannot tell a local from a parameter. Its fact reaches an importer by two roads only.
				// ImportObjectFact needs the object, which the importer never holds. AllObjectFacts returns only the
				// calling analyzer's own fact type, and among our analyzers only function_purity, return_nil_ness and
				// deprecated call it: function_purity and return_nil_ness hold facts only on functions, which are
				// package-level and have a path, and deprecated keys by the declared objects an importer meets. So
				// leaving the fact out changes no finding. A new caller of AllObjectFacts is a reason to look at this
				// again.
				encoded.dropped++
				continue
			}
			fact, value, factError := encodeFactAndValue(objectFact.Fact)
			if factError != nil {
				return encodedEntry{}, fmt.Errorf("storing %s's fact on %s: %w", action.Analyzer.Name,
					objectFact.Object, factError)
			}
			stored.ObjectFacts = append(stored.ObjectFacts, storedObjectFact{Path: path, Fact: fact})
			facts.objectFacts = append(facts.objectFacts, objectFactDigest{path: path, fact: value})
		}
		for _, packageFact := range packageFacts {
			fact, value, factError := encodeFactAndValue(packageFact.Fact)
			if factError != nil {
				return encodedEntry{}, fmt.Errorf("storing %s's package fact: %w", action.Analyzer.Name, factError)
			}
			stored.PackageFacts = append(stored.PackageFacts, fact)
			facts.packageFacts = append(facts.packageFacts, value)
		}
		entry.Analyzers[slot] = stored
		digested[slot] = facts
	}
	if writesExportData {
		exportData, exportError := writeExportData(saved)
		if exportError != nil {
			return encodedEntry{}, exportError
		}
		encoded.exportData = exportData
	}
	if saved.Types != nil {
		for _, imported := range saved.Types.Imports() {
			entry.Imports = append(entry.Imports, imported.Path())
		}
	}
	if holdsFacts(known, entry.Analyzers) {
		encoded.output, encoded.hasOutput = outputDigest(exportDigest(saved.Types), digested), true
		entry.Output = encoded.output[:]
	}
	encoded.positionDependent = positionDependent(saved, entry.Analyzers, loadFiles)
	body, bodyError := encodeBody(entry)
	if bodyError != nil {
		return encodedEntry{}, fmt.Errorf("encoding the entry: %w: %w", errorUnstorable, bodyError)
	}
	encoded.body = body
	encoded.record = entryRecord{
		Version:         CacheFormatVersion,
		Body:            contentDigest(body),
		Inline:          body,
		ExportData:      exportDataDigest(encoded.exportData),
		Reads:           demanded.reads,
		ReadsIncomplete: demanded.readsIncomplete,
	}
	return encoded, nil
}

// encodeBody is entry's body, as the store keeps it under its digest: entry without its reads, which its record holds.
func encodeBody(entry cacheEntry) ([]byte, error) {
	entry.Reads, entry.ReadsIncomplete = nil, false
	var body bytes.Buffer
	if gobError := gob.NewEncoder(&body).Encode(entry); gobError != nil {
		return nil, gobError
	}
	return body.Bytes(), nil
}

// wholeEntry is encoded's entry as one gob, its body with its record's reads, for a reader of a whole entry, as the
// standard library's table is written from.
func wholeEntry(encoded encodedEntry) ([]byte, error) {
	entry, decodeError := entryOf(encoded.body, encoded.record)
	if decodeError != nil {
		return nil, decodeError
	}
	var whole bytes.Buffer
	if gobError := gob.NewEncoder(&whole).Encode(entry); gobError != nil {
		return nil, gobError
	}
	return whole.Bytes(), nil
}

// encodeRecord is record's bytes, as the store keeps them under a package's key.
func encodeRecord(record entryRecord) ([]byte, error) {
	var encoded bytes.Buffer
	if gobError := gob.NewEncoder(&encoded).Encode(record); gobError != nil {
		return nil, fmt.Errorf("encoding an entry's record: %w", gobError)
	}
	return encoded.Bytes(), nil
}

// decodeRecord is the record in contents, read from under a package's key, or an error, which is a miss: bytes gob
// refuses, or a record in another format, as a whole entry of 11 decodes, whose Version this one shares.
func decodeRecord(contents []byte) (entryRecord, error) {
	var record entryRecord
	if decodeError := gob.NewDecoder(bytes.NewReader(contents)).Decode(&record); decodeError != nil {
		return entryRecord{}, fmt.Errorf("decoding an entry's record: %w", decodeError)
	}
	if record.Version != CacheFormatVersion {
		return entryRecord{}, fmt.Errorf("reading an entry's record: it is in format %q, not %q", record.Version,
			CacheFormatVersion)
	}
	return record, nil
}

// holdsFacts reports whether stored holds every analyzer that learns facts, without which an entry cannot say what
// an importer inherits from its package.
func holdsFacts(known *cacheAnalyzers, stored map[string]storedAnalysis) bool {
	for _, analyzer := range known.facts {
		if _, held := stored[known.slots[analyzer]]; !held {
			return false
		}
	}
	return true
}

// exportDataKey is where the store keeps the export data whose sha256 is digest, apart from every entry's key: one
// copy of the bytes, however many entries name it (entryRecord.ExportData).
func exportDataKey(digest []byte) cache.Key {
	return sha256.Sum256(append([]byte("export data by content "), digest...))
}

// exportDataDigest is the digest an entry names exportData by, nil for none.
func exportDataDigest(exportData []byte) []byte {
	if exportData == nil {
		return nil
	}
	return contentDigest(exportData)
}

// contentDigest is the sha256 of contents, as the store names what it keeps by content.
func contentDigest(contents []byte) []byte {
	digest := sha256.Sum256(contents)
	return digest[:]
}

// writeExportData is a package's types as gcexportdata writes them with the package's positions, nothing for unsafe,
// whose types are the type checker's own. The writer panics on what it cannot carry, as types a faked package "C" left
// invalid may be, so a panic is a package the entry cannot hold.
func writeExportData(saved *packages.Package) (exportData []byte, exportError error) {
	if !carriesExportData(saved) {
		return nil, nil
	}
	if saved.Types == nil {
		return nil, fmt.Errorf("writing the export data of %s: %w: it has no types", saved.ID, errorUnstorable)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			exportData = nil
			exportError = fmt.Errorf("writing the export data of %s: %w: %v", saved.ID, errorUnstorable, recovered)
		}
	}()
	var written bytes.Buffer
	if writeError := gcexportdata.Write(&written, saved.Fset, saved.Types); writeError != nil {
		return nil, fmt.Errorf("writing the export data of %s: %w: %w", saved.ID, errorUnstorable, writeError)
	}
	return written.Bytes(), nil
}

// carriesExportData reports whether an entry of saved holds export data beside it: every package's does but unsafe's,
// whose types are the type checker's own, which the load gives it without reading any (see load.Restored). An entry a
// build writes of a package, from source or through from the host's run (throughEntry), follows this one rule.
func carriesExportData(saved *packages.Package) bool {
	return saved.PkgPath != "unsafe"
}

// storedFiles is the walk of a package's syntax, as walk, generatedFiles and copiedFiles read it.
func storedFiles(saved *packages.Package) []storedFile {
	files := make([]storedFile, 0, len(saved.Syntax))
	for _, file := range saved.Syntax {
		nodes := 0
		ast.Inspect(file, func(node ast.Node) bool {
			if node != nil {
				nodes++
			}
			return true
		})
		files = append(files, storedFile{
			Name:      saved.Fset.PositionFor(file.FileStart, false).Filename,
			Nodes:     nodes,
			Generated: ast.IsGenerated(file),
			Copied:    copied.IsCopied(file),
		})
	}
	return files
}

// encodeFact is one fact as an entry holds it.
func encodeFact(fact analysis.Fact) (storedFact, error) {
	var encoded bytes.Buffer
	if encodeError := gob.NewEncoder(&encoded).Encode(fact); encodeError != nil {
		return storedFact{}, fmt.Errorf("encoding a %T fact: %w: %w", fact, errorUnstorable, encodeError)
	}
	return storedFact{Type: factTypeName(reflect.TypeOf(fact)), Encoded: encoded.Bytes()}, nil
}

// encodeFactAndValue is one fact as an entry holds it, and as outputDigest reads it, by the digest of its value, which
// is taken only once gob has encoded the fact, so the walk never meets a value gob refuses.
func encodeFactAndValue(fact analysis.Fact) (storedFact, factDigest, error) {
	stored, encodeError := encodeFact(fact)
	if encodeError != nil {
		return storedFact{}, factDigest{}, encodeError
	}
	value, valueError := factValueDigest(fact)
	if valueError != nil {
		return storedFact{}, factDigest{}, fmt.Errorf("%w: %w", errorUnstorable, valueError)
	}
	return stored, factDigest{typeName: stored.Type, value: value}, nil
}

// decodeEntry is a package's entry decoded whole from its body and its record, or an error, which is a miss: an entry
// in another format, a fact of a type its analyzer does not learn, or bytes gob refuses. Which analyzers the entry
// must hold is the session's to judge (see covers), since it depends on what else misses. Its export data is read by
// readExportData, only when the load asks for it.
func decodeEntry(
	known *cacheAnalyzers,
	restoring *packages.Package,
	body []byte,
	record entryRecord,
	readExportData func() ([]byte, error),
) (*restoredPackage, error) {
	entry, decodeError := entryOf(body, record)
	if decodeError != nil {
		return nil, fmt.Errorf("decoding the entry of %s: %w", restoring.ID, decodeError)
	}
	restored, restoreError := restoredOf(known, restoring, entry, readExportData)
	if restoreError != nil {
		return nil, restoreError
	}
	restored.exportDigest, restored.bodyDigest = record.ExportData, record.Body
	if restored.bodyDigest == nil {
		restored.bodyDigest = contentDigest(body)
	}
	return restored, nil
}

// entryOf is the entry whose body and record these are: the body decoded, with the record's reads.
func entryOf(body []byte, record entryRecord) (cacheEntry, error) {
	var entry cacheEntry
	if decodeError := gob.NewDecoder(bytes.NewReader(body)).Decode(&entry); decodeError != nil {
		return cacheEntry{}, decodeError
	}
	if record.Version != entry.Version {
		return cacheEntry{}, fmt.Errorf("its record is in format %q and its body in %q", record.Version,
			entry.Version)
	}
	entry.Reads, entry.ReadsIncomplete = record.Reads, record.ReadsIncomplete
	return entry, nil
}

// restoredOf is an entry gob has decoded, read whole as decodeEntry reads it: the binary's table holds its entries in
// a form of its own (see standardLibraryEntry), which is read through here too.
func restoredOf(
	known *cacheAnalyzers,
	restoring *packages.Package,
	entry cacheEntry,
	readExportData func() ([]byte, error),
) (*restoredPackage, error) {
	if entry.Version != CacheFormatVersion {
		return nil, fmt.Errorf("reading the entry of %s: it is in format %q, not %q", restoring.ID, entry.Version,
			CacheFormatVersion)
	}
	restored := &restoredPackage{
		analyses: map[*analysis.Analyzer]restoredAnalysis{},
		types: &load.Restored{
			ReadExportData: readExportData,
			Imports:        entry.Imports,
			Withheld:       entry.Withheld,
		},
		files:           entry.Files,
		answers:         entry.Answers,
		reads:           entry.Reads,
		readsIncomplete: entry.ReadsIncomplete,
		bodiless:        entry.Bodiless,
		bodiesRecorded:  entry.BodiesRecorded,
		slots:           slices.Sorted(maps.Keys(entry.Analyzers)),
	}
	if len(entry.Output) == len(restored.output) {
		copy(restored.output[:], entry.Output)
		restored.hasOutput = true
	}
	for analyzer, slot := range known.slots {
		stored, held := entry.Analyzers[slot]
		if !held {
			continue
		}
		analysisRestored, restoreError := decodeAnalysis(known, analyzer, stored)
		if restoreError != nil {
			return nil, fmt.Errorf("reading the entry of %s: %w", restoring.ID, restoreError)
		}
		restored.analyses[analyzer] = analysisRestored
	}
	return restored, nil
}

// covers reports whether restored holds every one of analyzers.
func covers(restored *restoredPackage, analyzers []*analysis.Analyzer) bool {
	for _, analyzer := range analyzers {
		if _, held := restored.analyses[analyzer]; !held {
			return false
		}
	}
	return true
}

// decodeAnalysis is one analyzer's stored analysis decoded, its object facts still by objectpath.
func decodeAnalysis(
	known *cacheAnalyzers,
	analyzer *analysis.Analyzer,
	stored storedAnalysis,
) (restoredAnalysis, error) {
	restored := restoredAnalysis{findings: stored.Findings}
	if known.reports[analyzer] {
		report, reportError := decodeReport(analyzer, stored.Report)
		if reportError != nil {
			return restoredAnalysis{}, reportError
		}
		restored.report = report
	}
	for _, objectFact := range stored.ObjectFacts {
		fact, factError := decodeFact(known, analyzer, objectFact.Fact)
		if factError != nil {
			return restoredAnalysis{}, factError
		}
		restored.objectFacts = append(restored.objectFacts, pathFact{path: objectFact.Path, fact: fact})
	}
	for _, packageFact := range stored.PackageFacts {
		fact, factError := decodeFact(known, analyzer, packageFact)
		if factError != nil {
			return restoredAnalysis{}, factError
		}
		restored.packageFacts = append(restored.packageFacts, fact)
	}
	return restored, nil
}

// decodeFact is one stored fact as a value of the type its analyzer learns by that name.
func decodeFact(known *cacheAnalyzers, analyzer *analysis.Analyzer, stored storedFact) (analysis.Fact, error) {
	factType, learned := known.factTypes[analyzer][stored.Type]
	if !learned {
		return nil, fmt.Errorf("decoding a fact of %s: it learns no fact of type %s", analyzer.Name, stored.Type)
	}
	fact, isFact := reflect.New(factType.Elem()).Interface().(analysis.Fact)
	if !isFact {
		return nil, fmt.Errorf("decoding a fact of %s: a new %s is not a fact", analyzer.Name, stored.Type)
	}
	if decodeError := gob.NewDecoder(bytes.NewReader(stored.Encoded)).Decode(fact); decodeError != nil {
		return nil, fmt.Errorf("decoding a %s fact of %s: %w", stored.Type, analyzer.Name, decodeError)
	}
	return fact, nil
}

// decodeReport is a report analyzer's stored result as a value of its ResultType, a pointer to the type gob encoded.
func decodeReport(analyzer *analysis.Analyzer, encoded []byte) (any, error) {
	if analyzer.ResultType == nil || analyzer.ResultType.Kind() != reflect.Pointer {
		return nil, fmt.Errorf("decoding %s's result: its result type %v is no pointer", analyzer.Name,
			analyzer.ResultType)
	}
	report := reflect.New(analyzer.ResultType.Elem())
	if decodeError := gob.NewDecoder(bytes.NewReader(encoded)).DecodeValue(report.Elem()); decodeError != nil {
		return nil, fmt.Errorf("decoding %s's result: %w", analyzer.Name, decodeError)
	}
	return report.Interface(), nil
}
