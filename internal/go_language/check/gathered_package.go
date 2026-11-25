package check

import (
	"fmt"
	"path/filepath"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/build_reads"
)

// gatheredPackage is what the gather reads of one analyzed package across every analyzer: its entry when the findings
// cache restored it, nil when it did not, its reads, nil when the run records none, and, made only once a finding of
// it is read for the files it names, its own files and what each finding's message was found to name.
type gatheredPackage struct {
	restored *restoredPackage
	reads    *build_reads.Reads
	own      map[string]bool
	named    map[findingMessage]bool
}

// findingMessage is a finding's message with the directory of the file it is in, which a relative path in the message
// is read against (namedFiles) and all the message is read with.
type findingMessage struct {
	directory string
	message   string
}

// markForeignFindings marks the package's reads never when a finding of analyzedPackage names a file that is not the
// package's own: the file it is in, a file its message names or one its fix edits (see namesForeignFile). Such a
// finding can change with a line of another package, which no read records. known gives every file of the load. A
// message is read once per directory it is in, since the package's analyzers report the same message again and again.
func (gathered *gatheredPackage) markForeignFindings(
	analyzedPackage *packages.Package,
	found []Finding,
	known func() *knownFiles,
) {
	if gathered.reads.Never() != "" {
		return
	}
	if gathered.own == nil {
		gathered.own = ownFiles(analyzedPackage)
		gathered.named = map[findingMessage]bool{}
	}
	for _, finding := range found {
		if !gathered.own[finding.File] || editsForeignFile(finding, gathered.own) {
			gathered.reads.MarkNever(fmt.Sprintf("a finding of %s names a file of another package", finding.Rule))
			return
		}
		message := findingMessage{directory: filepath.Dir(finding.File), message: finding.Message}
		names, read := gathered.named[message]
		if !read {
			names = messageNamesForeignFile(finding, gathered.own, known())
			gathered.named[message] = names
		}
		if names {
			gathered.reads.MarkNever(fmt.Sprintf("a finding of %s names a file of another package", finding.Rule))
			return
		}
	}
}
