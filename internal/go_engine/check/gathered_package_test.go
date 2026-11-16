package check

import (
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
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
