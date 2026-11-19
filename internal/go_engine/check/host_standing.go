package check

import (
	"sync"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// HostStanding is an extra build's standing bodies over the host's run (#e4rd5dw): for each package the build checks
// from source, the rule.StandingBodies its passes read (Of), which answers what stands there as the build's own
// judgment says (stands), which rules are confined, and the host's findings of a confined rule on a file. It is the
// sibling of HostInheritance one level down, a function declaration where that is a whole file, and differs from it in
// which of the host's packages it reads: a package's view reads the host's entry of that same package identifier and no
// other. HostInheritance takes a file's findings from the lowest identifier holding it, p before `p [p.test]`, which
// is right for a rule that reads only syntax and wrong here, since a confined rule reads types and the test variant's
// differ.
//
// It counts what the build showed of the host's (hostStandingPackage.Took, Counts): the standing declarations of each
// file of each package some rule took the host's findings in, and each rule's findings there. A file asked for whose
// findings would not place, or whose rule then crashed, is not counted. Every method, of it and of a package's view
// (hostStandingPackage), may be called from several goroutines at once: what a view reads is never written once the
// type is made, and the counts are held under lock.
type HostStanding struct {
	host *HostRun
	// analyzerOf is each rule's analyzer, by the rule's name, for the rules the run enables that are confined, in
	// whose slot the host's entries hold its findings. A rule absent from it is not confined and is answered nothing.
	analyzerOf map[string]*analysis.Analyzer
	// stands is the build's judgment of what stands: whether the function declared at offset of filename, in the
	// package identified so, stands.
	stands func(packageIdentifier string, filename string, offset int) bool

	lock sync.Mutex
	// declarations is how many declarations stood in each file of each package, as the last rule to take the file
	// there told it, which is every rule's number: a file's standing declarations are the package's, not a rule's.
	declarations map[standingFile]int
	// taken is how many of the host's findings each rule reported in each file of each package.
	taken map[standingTake]int
}

// standingFile is one file as one of the build's packages holds it. A file two packages hold, p and `p [p.test]`, is
// two of them, since each package's passes take it apart, each from its own entry of the host's.
type standingFile struct {
	packageIdentifier string
	filename          string
}

// standingTake is one rule's findings in the standing declarations of one file of one package, as the build showed
// them.
type standingTake struct {
	ruleName string
	file     standingFile
}

// Of is the standing bodies of the package identified by packageIdentifier, or nil when nothing can stand there: the
// host holds no entry under that identifier, so there is nothing of the host's to take and the build lints the whole
// package. A nil standing, which a build without one holds, answers nil for every package. The nil is a nil
// rule.StandingBodies, never a typed one, so the checker hands that package's passes nothing.
func (standing *HostStanding) Of(packageIdentifier string) rule.StandingBodies {
	if standing == nil || standing.host.entries[packageIdentifier] == nil {
		return nil
	}
	return &hostStandingPackage{standing: standing, packageIdentifier: packageIdentifier}
}

// Counts is how many standing declarations the build showed the host's findings in, and how many findings it showed
// there. bodies is the sum, over each file of each package some rule took, of the declarations that stood in it,
// counted once however many rules took the file, so a file two packages hold counts once for each. findings is the
// sum of each rule's findings on each file of each package. A file a reload took again counts as the reload told it,
// both numbers replacing the earlier ones. A nil standing counted nothing.
func (standing *HostStanding) Counts() (bodies int, findings int) {
	if standing == nil {
		return 0, 0
	}
	standing.lock.Lock()
	defer standing.lock.Unlock()
	for _, count := range standing.declarations {
		bodies += count
	}
	for _, count := range standing.taken {
		findings += count
	}
	return bodies, findings
}

// took counts, for the package identified by packageIdentifier, what the rule named ruleName reported of the host's
// in filename's standing declarations (hostStandingPackage.Took).
func (standing *HostStanding) took(
	packageIdentifier string,
	ruleName string,
	filename string,
	declarations int,
	findings int,
) {
	file := standingFile{packageIdentifier: packageIdentifier, filename: filename}
	standing.lock.Lock()
	defer standing.lock.Unlock()
	standing.declarations[file] = declarations
	standing.taken[standingTake{ruleName: ruleName, file: file}] = findings
}
