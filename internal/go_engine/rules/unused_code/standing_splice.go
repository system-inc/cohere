package unused_code

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// The build's half of the road a body record travels (#xeafx79, unit 2c): an extra build that leaves a body standing
// takes the host's record of it from the host's side of the package (BodyRecordHost) and splices it in place of
// walking the body, where the record is one the build can trust, and walks the body as today where it is not. A pass
// with no standing answer, the host's and every plain run's, or with no side that answers a record, splices nothing.

// SplicedBodies is what an extra build's U1000 did with the bodies that stand in its package: each spliced from the
// host's record, or walked, by why its record could not be taken.
type SplicedBodies struct {
	// Spliced are the bodies whose record the build took in place of walking them.
	Spliced int
	// Unrecorded are the standing bodies the host's side answers no record of: the host did not record the package,
	// restored it, recorded two bodies at the body's key, or could not digest the file it parsed the body from.
	Unrecorded int
	// Flagged are those whose record holds a flag (recordedPresence), so no record of positions replays it.
	Flagged int
	// OwnFileDiffers are those whose own file the build parsed under another name than the host did, could not read
	// again, or read with bytes whose digest is not the host's.
	OwnFileDiffers int
	// FilesDiffer are those whose record names a file the build does not compile, one whose host digest the side
	// does not answer, as a side that is no FileDigestHost answers none past the body's own file, or one the build
	// reads otherwise than the host did.
	FilesDiffer int
}

// SplicedOf is what analyzerResult, U1000's result on a package, says the run did with the bodies that stand there,
// and false for a run with no standing answer or no host's side that answers a record, as the host's and every plain
// run's are, or for any other analyzer's result.
func SplicedOf(analyzerResult any) (SplicedBodies, bool) {
	verdict, isVerdict := analyzerResult.(result)
	if !isVerdict || verdict.spliced == nil {
		return SplicedBodies{}, false
	}
	return *verdict.spliced, true
}

// standingSplice has built take the host's record of each body of pass's package that stands there, where the record
// is usable, and is what it did with each, nil when pass holds no standing answer or no host's side that answers a
// record. A record is usable when the side answers it for the package, it holds no flag, the build reads the file the
// body is in with the host's digest, under the name the host parsed it under, and reads every file the record names
// with the host's digest of it, which the side answers as a FileDigestHost for any file but the body's own. Any other
// standing body is walked, as every body is without a standing answer. A graph that records takes nothing: no run
// both records and stands, and a spliced local holds no object a recorder could place.
func standingSplice(pass *analysis.Pass, built *graph) (*SplicedBodies, error) {
	_, stands := pass.ResultOf[rule.Standing].(rule.StandingBodies)
	host, hosts := pass.ResultOf[rule.FactHost].(BodyRecordHost)
	if !stands || !hosts || built.recorder != nil {
		return nil, nil
	}
	spliced := &SplicedBodies{}
	spans := rule.StandingSpans(pass)
	if len(spans) == 0 {
		return spliced, nil
	}
	// An analysis pass carries no package ID, so it asks by its package's path, which is the ID of a package that is
	// no test variant. The side a pass holds is its own package's, by its ID, so a test variant's, keyed
	// `p [p.test]` or `p_test [p.test]`, answers no record for the path, and every body of a test variant is walked
	// until the side or the checker hands a pass its ID.
	packageIdentifier := pass.Pkg.Path()
	// A side that answers no file's digest leaves every record that names a file past its body's own walked.
	fileDigests, _ := host.(FileDigestHost)
	digests := &spliceDigests{pass: pass, host: fileDigests, packageIdentifier: packageIdentifier,
		own: map[string]ownDigest{}, parsed: map[string]bool{}}
	for _, file := range pass.Files {
		if tokenFile := pass.Fset.File(file.FileStart); tokenFile != nil {
			digests.parsed[tokenFile.Name()] = true
		}
	}
	var taken []bodyRecord
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.FileStart)
		standingBodies := map[token.Pos]bool{}
		for _, span := range spans[tokenFile] {
			standingBodies[span.Body] = true
		}
		if len(standingBodies) == 0 {
			continue
		}
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil || !standingBodies[function.Body.Lbrace] {
				continue
			}
			key, isKeyed := built.keyOf(function)
			if !isKeyed {
				// A span is read from a file the file set holds, so a standing body has a key.
				continue
			}
			recorded, digest, isRecorded := host.BodyRecord(packageIdentifier, key.File, key.Offset)
			switch {
			case !isRecorded:
				spliced.Unrecorded++
			case recorded.record.Present != (recordedPresence{}):
				spliced.Flagged++
			case recorded.ParsedFile != tokenFile.Name() || !digests.ownMatches(tokenFile.Name(), digest):
				spliced.OwnFileDiffers++
			case !digests.allMatch(recorded.record.Files, recorded.ParsedFile, digest):
				spliced.FilesDiffer++
			default:
				spliced.Spliced++
				taken = append(taken, recorded.record)
			}
		}
	}
	if len(taken) == 0 {
		return spliced, nil
	}
	if takeError := built.takeRecords(taken); takeError != nil {
		return nil, takeError
	}
	return spliced, nil
}
