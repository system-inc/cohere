package unused_code

import (
	"crypto/sha256"
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/lint"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// The host's half of the road a body record travels (#xeafx79, unit 2b): a host's run of U1000, asked under
// rule.BodyRecords, records each body it walks, and its result hands the records on (RecordedOf), for the host's side
// of the package to hold and hand an extra build that leaves a body standing (BodyRecordHost), whose U1000 splices
// the record in place of walking the body. Off the host's ask, as on every plain run, the graph has no recorder.

// BodyRecords is what a host's run of U1000 recorded of one package under rule.BodyRecords' ask.
type BodyRecords struct {
	// Bodies are the record of each function declaration's body the walk read, in the order it read them, each keyed
	// apart from every other (bodyRecords).
	Bodies []RecordedBody
	// Digests are the sha256 of each file of the package, by the name it was parsed under, a cgo file's as cgo
	// rewrote it, of the bytes the host's rules read (pass.ReadFile), which are the bytes its records were walked
	// from: each file a body was parsed from, and each a record's Files names, whether or not it holds a body. A file
	// the pass could not read again has none.
	Digests map[string][sha256.Size]byte
}

// RecordedBody is one body's record as the host's U1000 wrote it, opaque outside this package but for its key, the
// file it was parsed from, whether it can stand for its body at all and the files it rests on, so only U1000's own
// splice reads what the record holds.
type RecordedBody struct {
	// ParsedFile is the name the file holding the body's `func` was parsed under, which no `//line` directive changes,
	// and which BodyRecords.Digests digests.
	ParsedFile string
	record     bodyRecord
}

// Key is the body's key, as a standing body is keyed (rule.StandingBodies.Stands): the file a reader sees its `func`
// in, past any `//line` directive, and the offset of that `func` in the file as parsed.
func (recorded RecordedBody) Key() (filename string, offset int) {
	return recorded.record.Body.File, recorded.record.Body.Offset
}

// Summarizable is whether the record can stand for its body at all: no flag of why the body must stay present is
// set (bodyRecord.Present), which a graph's takeRecords refuses a record for.
func (recorded RecordedBody) Summarizable() bool {
	return recorded.record.Present == recordedPresence{}
}

// Files are the files the record rests on besides the body's own, by the names they were parsed under, sorted: each
// file an object declared outside the body is declared in, where the body uses it or it uses a local, and each whose
// name the walk read to judge a write to a package-level variable (bodyRecord.Files). A build that does not hold each
// as the host read it, by BodyRecords.Digests, cannot take the record. It is the record's own list, not a copy, so a
// caller reads it and never writes it.
func (recorded RecordedBody) Files() []string {
	return recorded.record.Files
}

// BodyRecordHost is the host's side of a package as an extra build's U1000 asks it for a body's record: the record of
// the body declared at offset in filename, keyed as Key keys it, and the digest of the file the host parsed it from
// (BodyRecords.Digests), which the build's own file of the body must read alike before the record is spliced, and
// false where the side holds none for packageId, the build's own package's ID. RecordsHeld is whether the side holds
// the host's records of packageId at all, true only where the host's U1000 ran on the package with its records asked
// and did not fail, though the package has no body, so a body with no record there is one the host could not record,
// never one it was not asked to. The engine's side of each package for the facts' reach is one (rule.FactHost).
type BodyRecordHost interface {
	BodyRecord(packageId string, filename string, offset int) (RecordedBody, [sha256.Size]byte, bool)
	RecordsHeld(packageId string) bool
}

// FileDigestHost is the host's side of a package as an extra build's U1000 asks it for the digest of any file of the
// package a record names (bodyRecord.Files): the sha256 of the bytes the host's rules read of the file parsed under
// filename, and false where the side holds none for packageId, the build's own package's ID. A record naming a file
// past its body's own is spliced only where the side answers this and the build reads the file alike, so a side that
// is no FileDigestHost has such a body walked.
type FileDigestHost interface {
	FileDigest(packageId string, filename string) ([sha256.Size]byte, bool)
}

// RecordedOf is the records analyzerResult holds, U1000's result on a package, and false for the result of a run
// that asked for none, or for any other analyzer's result.
func RecordedOf(analyzerResult any) (BodyRecords, bool) {
	verdict, isVerdict := analyzerResult.(result)
	if !isVerdict || verdict.recorded == nil {
		return BodyRecords{}, false
	}
	return *verdict.recorded, true
}

// passGraph is the empty graph of pass's package, with a recorder on only where the host asks for one under
// rule.BodyRecords: no other run makes a record, so none pays for one.
func passGraph(
	pass *analysis.Pass,
	directives []lint.Directive,
	generatedFiles map[string]generated.Generator,
) *graph {
	built := newGraph(pass.Fset, pass.Files, pass.Pkg, pass.TypesInfo, directives, generatedFiles)
	if asked, isAsked := pass.ResultOf[rule.BodyRecords].(bool); isAsked && asked {
		built.recordBodies()
	}
	return built
}

// recordedBodies is what built recorded of pass's package, once entry has walked it: each kept record with the file
// its body was parsed from, and the digest of every file of the package, read through pass.ReadFile once a file.
func recordedBodies(pass *analysis.Pass, built *graph) *BodyRecords {
	records := built.bodyRecords()
	parsedFiles := make(map[recordedPlace]string, len(records))
	recorded := &BodyRecords{Bodies: make([]RecordedBody, 0, len(records)),
		Digests: make(map[string][sha256.Size]byte, len(pass.Files))}
	unread := map[string]bool{}
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.FileStart)
		if tokenFile == nil {
			// Its bodies have no key (keyOf), so no record either, and no record names it.
			continue
		}
		if _, isDigested := recorded.Digests[tokenFile.Name()]; !isDigested && !unread[tokenFile.Name()] {
			contents, readError := pass.ReadFile(tokenFile.Name())
			if readError != nil {
				// The file the host parsed cannot be read again, so no digest says what it held: the side answers no
				// record of its bodies, no record resting on it can be taken, and a build walks those bodies itself,
				// as it walks any body unrecorded.
				unread[tokenFile.Name()] = true
			} else {
				recorded.Digests[tokenFile.Name()] = sha256.Sum256(contents)
			}
		}
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			if key, isKeyed := built.keyOf(function); isKeyed {
				parsedFiles[key] = tokenFile.Name()
			}
		}
	}
	for _, record := range records {
		recorded.Bodies = append(recorded.Bodies, RecordedBody{ParsedFile: parsedFiles[record.Body], record: record})
	}
	return recorded
}
