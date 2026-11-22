package standing_facts

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/unused_code"
)

// recordReasonPrefix starts the reason of every body that leaves for its U1000 record (RecordReason), so it never
// reads as a presence row's (standing_presence.RowOf) or a fact's.
const recordReasonPrefix = "U1000 record "

// RecordReason is why a body left the standing set by itself when U1000 could not take the host's record of it, for
// use (unused_code.RecordUse).
func RecordReason(use unused_code.RecordUse) fact_reach.Reason {
	return fact_reach.Reason(recordReasonPrefix + string(use))
}

// RecordLeaving is a body that left the standing set by itself because the host's record of it is one U1000 cannot
// take, and why: the use the record was judged (unused_code.JudgeRecords), and its reason (RecordReason).
type RecordLeaving struct {
	Place  fact_reach.Place
	Use    unused_code.RecordUse
	Reason fact_reach.Reason
}

// unusableRecords are the bodies of pass's package that standing holds whose record on the host's side U1000 could
// not splice in place of walking them, in the order the package declares them, a place two declarations share once
// for each (Result.keepRecordLeaving keeps it once) (#xeafx79). A build will
// strip a standing body's syntax from its type check, and U1000 then cannot walk it, so a body stands only with a
// usable record, and one without leaves here, before anything is stripped, to be linted by the build.
//
// It is nothing where the side holds no records of the package, RecordsHeld false: the host was not asked to record,
// U1000 did not run there or failed, or the pass is a test variant's, which asks by a path that is not its ID. No body
// there leaves for its record, so the standing every other rule reads is what it was without U1000's records. Where
// the side holds them, a body it answers no record of is one the host could not record, never one it was not asked to.
func unusableRecords(
	pass *analysis.Pass,
	host fact_reach.Host,
	standing map[fact_reach.Place]bool,
) []RecordLeaving {
	records, isRecords := host.(unused_code.BodyRecordHost)
	if !isRecords || !records.RecordsHeld(pass.Pkg.Path()) {
		return nil
	}
	var standingFunctions []*ast.FuncDecl
	places := map[*ast.FuncDecl]fact_reach.Place{}
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.FileStart)
		if tokenFile == nil {
			continue
		}
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			// Placed as the facts' reach places a body: the name a `//line` directive gives its file, and the offset
			// of its `func` as parsed.
			place := fact_reach.Place{File: tokenFile.Position(function.Pos()).Filename,
				Offset: tokenFile.Offset(function.Pos())}
			if standing[place] {
				standingFunctions = append(standingFunctions, function)
				places[function] = place
			}
		}
	}
	var leaving []RecordLeaving
	for _, judged := range unused_code.JudgeRecords(pass, records, standingFunctions) {
		if judged.Use != unused_code.RecordUsable {
			leaving = append(leaving, RecordLeaving{Place: places[judged.Function], Use: judged.Use,
				Reason: RecordReason(judged.Use)})
		}
	}
	return leaving
}
