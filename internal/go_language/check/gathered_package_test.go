package check

import (
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/build_reads"
)

// The gather reads a message once per directory its findings are in, never once per package: one message naming
// `z.go` names the package's own file from one of its directories and another package's from the other, so the
// finding in the second marks the package's reads never though the same message read in the first named nothing
// foreign.
func TestTheGatherReadsAMessageByTheDirectoryOfItsFinding(test *testing.T) {
	test.Parallel()

	split := &packages.Package{GoFiles: []string{"/w/b/y.go", "/w/b/z.go", "/w/a/x.go"}}
	other := &packages.Package{GoFiles: []string{"/w/a/z.go"}}
	known := newKnownFiles()
	known.addPackage(split)
	known.addPackage(other)
	gathered := gatheredPackage{reads: &build_reads.Reads{}}
	gathered.markForeignFindings(split, []Finding{
		{Rule: "first", File: "/w/b/y.go", Message: "see z.go"},
		{Rule: "second", File: "/w/a/x.go", Message: "see z.go"},
	}, func() *knownFiles { return known })
	if never := gathered.reads.Never(); never != "a finding of second names a file of another package" {
		test.Errorf("the package's reads were marked never for %q, want the second finding's foreign z.go", never)
	}
}

// The gather marks a package's reads never exactly when a finding of it names a file not its own as namesForeignFile
// reads one, or sits in such a file: a finding in another package's file, one whose fix edits another package's file
// and one whose message names one each mark it, and a clean one marks nothing.
func TestTheGatherMarksExactlyTheFindingsThatNameAnotherPackagesFile(test *testing.T) {
	test.Parallel()

	own := &packages.Package{GoFiles: []string{"/w/p/x.go"}}
	other := &packages.Package{GoFiles: []string{"/w/q/y.go"}}
	known := newKnownFiles()
	known.addPackage(own)
	known.addPackage(other)
	foreignEdit := []Suggestion{{Message: "move it", Edits: []Edit{{File: "/w/q/y.go", Start: 0, End: 1, Text: "a"}}}}
	cases := []struct {
		name       string
		finding    Finding
		wantMarked bool
	}{
		{name: "in another package's file", wantMarked: true,
			finding: Finding{Rule: "elsewhere", File: "/w/q/y.go", Message: "the value is never used"}},
		{name: "a fix edits another package's file", wantMarked: true,
			finding: Finding{Rule: "editing", File: "/w/p/x.go", Message: "the value is never used",
				Suggestions: foreignEdit}},
		{name: "the message names another package's file", wantMarked: true,
			finding: Finding{Rule: "naming", File: "/w/p/x.go", Message: "conflicts with ../q/y.go:3"}},
		{name: "clean", wantMarked: false,
			finding: Finding{Rule: "clean", File: "/w/p/x.go", Message: "the value is never used"}},
	}
	for _, each := range cases {
		gathered := gatheredPackage{reads: &build_reads.Reads{}}
		gathered.markForeignFindings(own, []Finding{each.finding}, func() *knownFiles { return known })
		marked := gathered.reads.Never() != ""
		ownFilesOf := ownFiles(own)
		predicate := !ownFilesOf[each.finding.File] || namesForeignFile(each.finding, ownFilesOf, known)
		if marked != each.wantMarked || marked != predicate {
			test.Errorf("%s: the gather marked the reads never %t, want %t, as the file and namesForeignFile read it "+
				"(%t)", each.name, marked, each.wantMarked, predicate)
		}
	}
}
