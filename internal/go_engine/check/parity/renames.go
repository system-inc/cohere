package parity

import (
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// Renames are the renames given beside a call of RequireUpstreamWants or RequireUpstreamWantsWithFixes, for an
// original's fixture copied unchanged, whose wants cannot carry a record: each a Record whose Text is a `renames:`
// record, the original's message `=>` the port's, as a differences fixture writes one after `// port `, on the File
// and Line of the want it renames, with its Reason. Its methods are the package's two functions, taking the same
// arguments, since a Go call takes one variadic list and theirs is the patterns, as Target's are.
type Renames struct {
	given []Record
}

// Renaming is the renames given, read as the port's finding at each one's file and line in the port's words being
// the original's, so the want there is met by the finding the original would make. Every other want, and every other
// finding, is held as RequireUpstreamWants holds it. A rename that no finding meets fails, named by its file and line,
// and so does one with no Reason; a record of any other kind fails the call, named, since only a rename reads a
// finding of the port's as one of the original's.
func Renaming(given ...Record) Renames {
	return Renames{given: slices.Clone(given)}
}

// RequireUpstreamWants is the package's RequireUpstreamWants with renames read.
func (renames Renames) RequireUpstreamWants(
	test testing.TB,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	requireUpstreamWants(test, analysistest.Run, port, testdata, patterns, renames.given)
}

// RequireUpstreamWantsWithFixes is the package's RequireUpstreamWantsWithFixes with renames read. A rename changes a
// finding's message alone, so the fixes are held as there.
func (renames Renames) RequireUpstreamWantsWithFixes(
	test testing.TB,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	requireUpstreamWants(test, analysistest.RunWithSuggestedFixes, port, testdata, patterns, renames.given)
}

// upstreamRename is one rename given beside a call of RequireUpstreamWants: at, the port's finding it reads, by file,
// line and message; original, the message it reads it as; and met, how many of the port's findings it read, counted
// across the loads and the packages a load runs at once.
type upstreamRename struct {
	at       Finding
	original string
	met      atomic.Int64
}

// upstreamRenamings is given read as renames, each failed that gives no Reason, as adjudicated fails a record given
// beside its call, and the call failed for one it cannot read or that renames nothing.
func upstreamRenamings(test testing.TB, given []Record) []*upstreamRename {
	test.Helper()
	renamings := []*upstreamRename{}
	for _, record := range given {
		match, _, isReadable := readRecord(record.Text)
		if !isReadable {
			test.Fatalf("%s:%d is given beside the call a record the harness cannot read: %q", record.File,
				record.Line, record.Text)
		}
		if match[renamesFromGroup] == "" {
			test.Fatalf("%s:%d is given beside the call a record that renames nothing, where only `renames:` reads a "+
				"finding of the port's as a want: %q", record.File, record.Line, record.Text)
		}
		if strings.TrimSpace(record.Reason) == "" {
			test.Errorf("%s:%d is given beside the call a record with no reason: write why in its Reason",
				record.File, record.Line)
		}
		renamings = append(renamings, &upstreamRename{
			at:       Finding{File: record.File, Line: record.Line, Message: match[renamesToGroup]},
			original: match[renamesFromGroup],
		})
	}
	return renamings
}

// renamedAt is port with each of its findings that a rename of renamings reads, in fixtures, reported in the
// original's message, and everything else of port's kept, as recovering keeps the original's. With no rename it is
// port itself, so a call given none runs exactly the analyzer it was handed.
func renamedAt(port *analysis.Analyzer, fixtures fixtureLoad, renamings []*upstreamRename) *analysis.Analyzer {
	if len(renamings) == 0 {
		return port
	}
	renamed := *port
	renamed.Run = func(pass *analysis.Pass) (any, error) {
		reporting := *pass
		reporting.Report = func(diagnostic analysis.Diagnostic) {
			position := pass.Fset.Position(diagnostic.Pos)
			for _, renaming := range renamings {
				if renaming.at.File == fixtures.fileName(position.Filename) && renaming.at.Line == position.Line &&
					renaming.at.Message == diagnostic.Message {
					diagnostic.Message = renaming.original
					renaming.met.Add(1)
					break
				}
			}
			pass.Report(diagnostic)
		}
		return port.Run(&reporting)
	}
	return &renamed
}
