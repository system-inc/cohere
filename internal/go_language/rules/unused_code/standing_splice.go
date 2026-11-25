package unused_code

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/lint"

	"github.com/system-inc/cohere/internal/go_language/rule"
)

// The build's half of the road a body record travels (#xeafx79, unit 2c): an extra build that leaves a body standing
// takes the host's record of it from the host's side of the package (BodyRecordHost) and splices it in place of
// walking the body, where the record is one the build can trust, and walks the body as today where it is not. A pass
// with no standing answer, the host's and every plain run's, or with no side that answers a record, splices nothing.
// While a build still type-checks every body, it also writes its own record of each body it would splice and takes
// the host's only where the two are one, end for end (the record differential, machine 2 of the design): the check of
// the claim the splice rests on, that a standing body adds to the build's graph what it added to the host's.

// namedDifferences is how many of the bodies whose records differ SplicedBodies names: enough for a census to find
// the first few and read them, while SplicedBodies stays a value two runs compare with ==.
const namedDifferences = 3

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
	// RecordsDiffer are those whose record passed every check above but is not, end for end, the record the build
	// writes of the body itself, walking it with a recorder on (the record differential): a hole in what a record
	// reaches, or a read the checks of its files missed, which the body's walk keeps out of the verdict. A record that
	// does not encode at all, as no recorder writes one, is among them too.
	RecordsDiffer int
	// DifferingBodies names the first of RecordsDiffer, in the order the package declares them, as many as it has
	// room for, and holds the zero DifferingBody past them.
	DifferingBodies [namedDifferences]DifferingBody
	// SpliceFailed are the bodies whose records the build took and then dropped, because the graph could not take or
	// replay them, so a fresh graph walked every body instead (enterSpliced). They are not counted in Spliced.
	SpliceFailed int
	// SpliceFailure is the error the failed splice stopped at, as text for a census to read, and empty where no splice
	// failed. A run splices once, so it holds at most one.
	SpliceFailure string
}

// DifferingBody is a body whose host's record is not the build's own: the key it stands by, the file a reader sees
// its `func` in and the offset of that `func` as parsed, and the first field of the canonical form the two records
// disagree on, as the form's JSON names it, with the index of the first differing entry of a list: `uses[1]`.
// `record` says the build wrote no record of its own at the key, and `encoding` that the two encode differently
// though every field agrees, or that one of them does not encode at all.
type DifferingBody struct {
	File   string
	Offset int
	Field  string
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

// standingSplice is the host's record of each body of pass's package that stands there that built should take, where
// the record is usable, and what it did with each, nil when pass holds no standing answer or no host's side that
// answers a record. Whether a record is usable is JudgeRecords' judgment, the one the standing set's settlement takes
// a body out by where the side holds the package's records. A usable record is then taken only where it encodes as
// the build's own record of the body does (builtRecords), which a second graph of the package writes, built only when
// some record is usable. Any other standing body is walked, as every body is without a standing answer. A graph that
// records takes nothing: no run both records and stands, and a spliced local holds no object a recorder could place.
// The records are handed back rather than taken, so enterSpliced can walk every body where taking them fails.
func standingSplice(pass *analysis.Pass, built *graph) (*SplicedBodies, []bodyRecord, error) {
	_, stands := pass.ResultOf[rule.Standing].(rule.StandingBodies)
	host, hosts := pass.ResultOf[rule.FactHost].(BodyRecordHost)
	if !stands || !hosts || built.recorder != nil {
		return nil, nil, nil
	}
	spliced := &SplicedBodies{}
	spans := rule.StandingSpans(pass)
	if len(spans) == 0 {
		return spliced, nil, nil
	}
	var standingFunctions []*ast.FuncDecl
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
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Body != nil &&
				standingBodies[function.Body.Lbrace] {
				standingFunctions = append(standingFunctions, function)
			}
		}
	}
	// A span is read from a file the file set holds, so every standing body has a key and a judgment.
	var usable []bodyRecord
	for _, judged := range JudgeRecords(pass, host, standingFunctions) {
		switch judged.Use {
		case RecordUnrecorded:
			spliced.Unrecorded++
		case RecordFlagged:
			spliced.Flagged++
		case RecordOwnFileDiffers:
			spliced.OwnFileDiffers++
		case RecordFilesDiffer:
			spliced.FilesDiffer++
		case RecordUsable:
			usable = append(usable, judged.Record.record)
		}
	}
	if len(usable) == 0 {
		return spliced, nil, nil
	}
	ownRecords, ownError := builtRecords(built)
	if ownError != nil {
		// The build's own graph takes no record and walks every body, as a plain run does, so where its entry fails
		// a plain run of the package fails the same way, and no fallback has a verdict to give.
		return nil, nil, ownError
	}
	var taken []bodyRecord
	for _, record := range usable {
		own, isOwn := ownRecords[record.Body]
		field, differError := recordDifference(record, own, isOwn)
		if differError != nil {
			// A record no recorder writes cannot be compared, so its body is walked as one whose records differ: a
			// host's record the build cannot read costs that body's splice, never the package's findings.
			field = "encoding"
		}
		if field == "" {
			spliced.Spliced++
			taken = append(taken, record)
			continue
		}
		if spliced.RecordsDiffer < namedDifferences {
			spliced.DifferingBodies[spliced.RecordsDiffer] = DifferingBody{File: record.Body.File,
				Offset: record.Body.Offset, Field: field}
		}
		spliced.RecordsDiffer++
	}
	return spliced, taken, nil
}

// enterSpliced enters built, having it take taken, the records standingSplice handed back, first, and is the graph
// whose results are the verdict and what the run did with the standing bodies, spliced as standingSplice counted
// them. Where built cannot take the records or replay one, the run drops them all, counted in SpliceFailed in place
// of Spliced with the error kept as SpliceFailure, and enters a fresh graph of pass's package, as passGraph builds
// it, that walks every body, as a plain run does: a record the build cannot replay costs the splice, never the
// package's findings. Built is not entered again, since a failed entry leaves it half walked. With nothing taken,
// entry's error is the run's, as on a plain run, and so is the fresh graph's.
func enterSpliced(
	pass *analysis.Pass,
	packageDirectives []lint.Directive,
	generatedFiles map[string]generated.Generator,
	built *graph,
	taken []bodyRecord,
	spliced *SplicedBodies,
) (*graph, *SplicedBodies, error) {
	if len(taken) == 0 {
		return built, spliced, built.entry()
	}
	spliceError := built.takeRecords(taken)
	if spliceError == nil {
		// Entry fails only where a record it took cannot be spliced.
		spliceError = built.entry()
	}
	if spliceError == nil {
		return built, spliced, nil
	}
	// Records are taken only on a run standingSplice counted, so spliced is never nil here.
	fellBack := *spliced
	fellBack.SpliceFailed += len(taken)
	fellBack.Spliced -= len(taken)
	fellBack.SpliceFailure = spliceError.Error()
	walked := passGraph(pass, packageDirectives, generatedFiles)
	return walked, &fellBack, walked.entry()
}

// builtRecords is the record a build's own walk writes of each body of built's package, by the body's key: a second
// graph of the package, with a recorder on, walked whole as the host's run walks it. It is the record differential's
// side of the comparison (#xeafx79, machine 2), which holds while the build still type-checks every body, so it is
// built only on a pass that holds a standing answer and a usable record, and never on a plain run.
func builtRecords(built *graph) (map[recordedPlace]bodyRecord, error) {
	own := newGraph(built.fileSet, built.files, built.typesPackage, built.typesInfo, built.directives, built.generated)
	own.recordBodies()
	if entryError := own.entry(); entryError != nil {
		return nil, entryError
	}
	records := own.bodyRecords()
	byKey := make(map[recordedPlace]bodyRecord, len(records))
	for _, record := range records {
		byKey[record.Body] = record
	}
	return byKey, nil
}

// recordDifference is the first field the host's record of a body disagrees with own, the build's record of it, on,
// worded as DifferingBody.Field words it, and empty where the two encode alike, byte for byte. The build's parsed
// name of every file a usable record names is the host's, so the encodings are compared exactly. A build with no
// record of its own at the key, isOwn false, differs at `record`, so a recorder that wrote nothing reads as a
// difference and never as agreement. It fails for a record no recorder writes, as encoded does.
func recordDifference(host bodyRecord, own bodyRecord, isOwn bool) (string, error) {
	if !isOwn {
		return "record", nil
	}
	hostEncoded, hostError := host.encoded()
	if hostError != nil {
		return "", hostError
	}
	ownEncoded, ownError := own.encoded()
	if ownError != nil {
		return "", ownError
	}
	if bytes.Equal(hostEncoded, ownEncoded) {
		return "", nil
	}
	// Both encoded, so both have a canonical form, read again only for a body whose records differ.
	hostCanonical, hostCanonicalError := host.canonical()
	if hostCanonicalError != nil {
		return "", hostCanonicalError
	}
	ownCanonical, ownCanonicalError := own.canonical()
	if ownCanonicalError != nil {
		return "", ownCanonicalError
	}
	// The body is the key both records were found by, so it never differs.
	if index, differs := firstDifferingIndex(hostCanonical.Locals, ownCanonical.Locals); differs {
		return fmt.Sprintf("locals[%d]", index), nil
	}
	if index, differs := firstDifferingIndex(hostCanonical.Uses, ownCanonical.Uses); differs {
		return fmt.Sprintf("uses[%d]", index), nil
	}
	if index, differs := firstDifferingIndex(hostCanonical.Rooted, ownCanonical.Rooted); differs {
		return fmt.Sprintf("rooted[%d]", index), nil
	}
	if index, differs := firstDifferingIndex(hostCanonical.Files, ownCanonical.Files); differs {
		return fmt.Sprintf("files[%d]", index), nil
	}
	if hostCanonical.Present != ownCanonical.Present {
		return "present", nil
	}
	// Every field agrees while the bytes do not, which one encoding of one form never gives: still a difference.
	return "encoding", nil
}

// firstDifferingIndex is the index of the first entry where host and own differ, or where the shorter of them ends,
// and whether they differ at all.
func firstDifferingIndex[Entry any](host []Entry, own []Entry) (int, bool) {
	shorter := min(len(host), len(own))
	for index := range shorter {
		if !reflect.DeepEqual(host[index], own[index]) {
			return index, true
		}
	}
	return shorter, len(host) != len(own)
}
