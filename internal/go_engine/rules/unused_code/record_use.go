package unused_code

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// RecordUse is what an extra build may do with the host's record of a body it leaves standing (#xeafx79): splice it
// in place of walking the body, RecordUsable, or not, for the reason each other value names, which SplicedBodies
// counts under the field of the same name. One judgment, JudgeRecords, serves U1000's splice and the standing set's
// settlement, which takes a body whose record is unusable out of the set before anything of it is stripped.
type RecordUse string

const (
	// RecordUsable is a record the build can take: the side answers it, it holds no flag, and the build reads its
	// body's own file and every file it names with the host's digests.
	RecordUsable RecordUse = "usable"
	// RecordUnrecorded is a body the side answers no record of (SplicedBodies.Unrecorded).
	RecordUnrecorded RecordUse = "unrecorded"
	// RecordFlagged is a record holding a flag, which no record of positions replays (SplicedBodies.Flagged).
	RecordFlagged RecordUse = "flagged"
	// RecordOwnFileDiffers is a body whose own file the build reads otherwise than the host did
	// (SplicedBodies.OwnFileDiffers).
	RecordOwnFileDiffers RecordUse = "own file differs"
	// RecordFilesDiffer is a record naming a file the build does not hold as the host read it
	// (SplicedBodies.FilesDiffer).
	RecordFilesDiffer RecordUse = "files differ"
)

// JudgedRecord is the host's record of one function declaration's body, the zero RecordedBody where the side answers
// none, and what a build may do with it.
type JudgedRecord struct {
	Function *ast.FuncDecl
	Record   RecordedBody
	Use      RecordUse
}

// JudgeRecords is the judgment of the host's record of each of functions, declarations with a body in pass's package,
// as host, the host's side of the package, answers it, in the order of functions. A record is usable when the side
// answers it for the package, it holds no flag, the build reads the file the body is in with the host's digest, under
// the name the host parsed it under, and reads every file the record names with the host's digest of it, which the
// side answers as a FileDigestHost for any file but the body's own. Every other use names the first of those the
// record fails, in that order. A declaration in no file of the pass's file set has no key, and so no judgment: it is
// left out. The functions are judged in one call so each file is read and digested once for them all.
func JudgeRecords(pass *analysis.Pass, host BodyRecordHost, functions []*ast.FuncDecl) []JudgedRecord {
	// An analysis pass carries no package ID, so it asks by its package's path, which is the ID of a package that is
	// no test variant. The side a pass holds is its own package's, by its ID, so a test variant's, keyed
	// `p [p.test]` or `p_test [p.test]`, answers no record for the path, and every body of a test variant is walked
	// until the side or the checker hands a pass its ID.
	packageIdentifier := pass.Pkg.Path()
	// A side that answers no file's digest judges every record that names a file past its body's own unusable.
	fileDigests, _ := host.(FileDigestHost)
	digests := &spliceDigests{pass: pass, host: fileDigests, packageIdentifier: packageIdentifier,
		own: map[string]ownDigest{}, parsed: map[string]bool{}}
	for _, file := range pass.Files {
		if tokenFile := pass.Fset.File(file.FileStart); tokenFile != nil {
			digests.parsed[tokenFile.Name()] = true
		}
	}
	judged := make([]JudgedRecord, 0, len(functions))
	for _, function := range functions {
		key, isKeyed := bodyKey(pass.Fset, function)
		if !isKeyed {
			continue
		}
		parsedName := pass.Fset.File(function.Pos()).Name()
		recorded, digest, isRecorded := host.BodyRecord(packageIdentifier, key.File, key.Offset)
		use := RecordUsable
		switch {
		case !isRecorded:
			use = RecordUnrecorded
		case !recorded.Summarizable():
			use = RecordFlagged
		case recorded.ParsedFile != parsedName || !digests.ownMatches(parsedName, digest):
			use = RecordOwnFileDiffers
		case !digests.allMatch(recorded.Files(), recorded.ParsedFile, digest):
			use = RecordFilesDiffer
		}
		judged = append(judged, JudgedRecord{Function: function, Record: recorded, Use: use})
	}
	return judged
}
