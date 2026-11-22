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
	// Digests are the sha256 of each file a body of Bodies was parsed from, by the name it was parsed under, of the
	// bytes the host's rules read (pass.ReadFile), which are the bytes its record was walked from. A file the pass
	// could not read again has none.
	Digests map[string][sha256.Size]byte
}

// RecordedBody is one body's record as the host's U1000 wrote it, opaque outside this package but for its key and the
// file it was parsed from, so only U1000's own splice reads what the record holds.
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

// BodyRecordHost is the host's side of a package as an extra build's U1000 asks it for a body's record: the record of
// the body declared at offset in filename, keyed as Key keys it, and the digest of the file the host parsed it from
// (BodyRecords.Digests), which the build's own file of the body must read alike before the record is spliced, and
// false where the side holds none for packageId, the build's own package's ID. The engine's side of each package for
// the facts' reach is one (rule.FactHost).
type BodyRecordHost interface {
	BodyRecord(packageId string, filename string, offset int) (RecordedBody, [sha256.Size]byte, bool)
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
// its body was parsed from, and the digest of each such file, read through pass.ReadFile once a file.
func recordedBodies(pass *analysis.Pass, built *graph) *BodyRecords {
	records := built.bodyRecords()
	parsedFiles := make(map[recordedPlace]string, len(records))
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.FileStart)
		if tokenFile == nil {
			// Its bodies have no key (keyOf), so no record either.
			continue
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
	recorded := &BodyRecords{Bodies: make([]RecordedBody, 0, len(records)), Digests: map[string][sha256.Size]byte{}}
	unread := map[string]bool{}
	for _, record := range records {
		parsedFile := parsedFiles[record.Body]
		recorded.Bodies = append(recorded.Bodies, RecordedBody{ParsedFile: parsedFile, record: record})
		if _, isDigested := recorded.Digests[parsedFile]; isDigested || unread[parsedFile] {
			continue
		}
		contents, readError := pass.ReadFile(parsedFile)
		if readError != nil {
			// The file the host parsed cannot be read again, so no digest says what the record was walked from, and
			// the side answers no record of its bodies: a build walks them itself, as it walks any body unrecorded.
			unread[parsedFile] = true
			continue
		}
		recorded.Digests[parsedFile] = sha256.Sum256(contents)
	}
	return recorded
}
