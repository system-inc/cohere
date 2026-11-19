package go_engine

import (
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// buildStanding is one extra build's standing bodies on the census path (pipeline_standing_bodies.go, #e4rd5dw): the
// host's run's take for the build's confined rules (check.HostStanding), which answers each package's standing
// bodies, and what stands in each package as standing_facts settled it there, which the take's judgment reads. One is
// made for a build and lives through its reloads, so its counts are of every load, each file as its last load took it.
type buildStanding struct {
	host *check.HostStanding

	// settled is standing_facts' result on each package the build's last load handed a standing answer, by package ID,
	// written as each package's answer is first asked, by whichever action on it reads it first, and read by every
	// rule on the package after: settledLock guards it.
	settledLock sync.RWMutex
	settled     map[string]*standing_facts.Result
}

// newBuildStanding is the standing bodies of a build over host, the host's run, for the rules of enabled that
// confined names, nil when the host kept nothing, as under `--timing`, so nothing can stand.
func newBuildStanding(host *check.HostRun, enabled []rule.Rule, confined map[string]bool) *buildStanding {
	if host == nil {
		return nil
	}
	standing := &buildStanding{settled: map[string]*standing_facts.Result{}}
	standing.host = host.Standing(enabled, confined, standing.stands)
	return standing
}

// bodiesOf is check.Options.StandingBodies for the build: the standing bodies of the package identified by
// packageIdentifier, after its action of standing_facts, whose result is after. It is nil, so the package's passes
// hold nothing and every rule hears every body there, when after is no settled *standing_facts.Result, as where the
// build lints the package whole or handed it no host's side, when nothing stands once the facts settled, and when the
// host kept no entry of the package (check.HostStanding.Of).
func (standing *buildStanding) bodiesOf(packageIdentifier string, after any) rule.StandingBodies {
	settled, isResult := after.(*standing_facts.Result)
	if !isResult || !settled.Partial || settled.Settlement == nil || len(settled.Settlement.Standing) == 0 {
		return nil
	}
	standing.settledLock.Lock()
	standing.settled[packageIdentifier] = settled
	standing.settledLock.Unlock()
	return standing.host.Of(packageIdentifier)
}

// stands is the build's judgment of what stands, which the host's take asks for each function declared in a package
// it answered (check.HostRun.Standing): standing_facts' answer there, keyed as the facts' reach keys a body, by the
// file a `//line` directive names at the offset as parsed (standing_facts.Result.Stands). A package with no settled
// result has nothing standing.
func (standing *buildStanding) stands(packageIdentifier string, filename string, offset int) bool {
	standing.settledLock.RLock()
	settled := standing.settled[packageIdentifier]
	standing.settledLock.RUnlock()
	return settled.Stands(filename, offset)
}

// counts is how many standing declarations the build showed the host's findings in, and how many findings it showed
// there (check.HostStanding.Counts), both 0 for a build with no standing bodies.
func (standing *buildStanding) counts() (bodies int, findings int) {
	if standing == nil {
		return 0, 0
	}
	return standing.host.Counts()
}
