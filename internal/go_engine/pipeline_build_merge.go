package go_engine

import (
	"cmp"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/suppression"
)

// buildOutcome is one build's findings after its own //cohere:ignore comments (#72n9mq0): each build's findings
// are suppressed alone, with only the files it has a say on indexed, before any build's are merged with another's, as
// staticcheck's runner filters each build's problems by its ignores before it merges them.
type buildOutcome struct {
	// said is the files the build has a say on: compiled into a package it analyzed (see buildRun).
	said map[string]bool
	// kept are its findings in scope that no directive withheld, and withheld those a directive did.
	kept     []check.Finding
	withheld []check.Finding
	// directiveFindings are what is wrong with the directives of the files it has a say on, as it saw them.
	directiveFindings []suppression.DirectiveFinding
	crashes           []check.Crash
	typeErrors        []load.Diagnostic
}

// outcomeOf is run's findings in scope after its own //cohere:ignore comments, with only the readable files run has
// a say on indexed, so a directive is judged only by the builds that compiled and analyzed its file. In scope is the
// host's scope or a file in the paths the host's build constraints leave out, which another build compiles. ran are
// the analyzer rules that ran, the same for every build.
func (engine *pipeline) outcomeOf(run buildRun, ran []string) buildOutcome {
	inScope := func(file string) bool {
		return engine.described.inScopeSet[file] || engine.described.constrainedInScope[file]
	}
	var files []string
	for file := range run.said {
		if _, read := engine.described.contents[file]; read && inScope(file) {
			files = append(files, file)
		}
	}
	slices.Sort(files)
	var found []check.Finding
	for _, each := range run.result.Findings {
		if inScope(each.File) {
			found = append(found, each)
		}
	}
	suppressed := engine.suppress(ran, found, files)
	return buildOutcome{
		said:              run.said,
		kept:              suppressed.kept,
		withheld:          suppressed.withheld,
		directiveFindings: suppressed.directiveFindings,
		crashes:           run.result.Crashes,
		typeErrors:        run.typeErrors,
	}
}

// mergeIfAllRules is the name of every rule of registered whose findings stand only where every build with a say on
// the file reports them (see rule.Rule's MergeIfAll).
func mergeIfAllRules(registered []rule.Rule) map[string]bool {
	names := map[string]bool{}
	for _, registeredRule := range registered {
		if registeredRule.MergeIfAll {
			names[registeredRule.Name] = true
		}
	}
	return names
}

// buildsVerdict is what the lint phase writes when more than one build ran: the merge of every build's findings, and
// what each build covered and brought, the host's first.
type buildsVerdict struct {
	merged  mergedBuilds
	records []contract.BuildRecord
}

// mergedBuilds is what the run reports of every build at once.
type mergedBuilds struct {
	// kept are the findings that stand, each once, sorted by file, line, column and rule.
	kept []check.Finding
	// withheldByRule counts each finding a directive withheld in some build once, by rule name.
	withheldByRule    map[string]int
	directiveFindings []suppression.DirectiveFinding
	crashes           []check.Crash
	typeErrors        []load.Diagnostic
	// added and dropped count, by build in the order merged, the findings that stand which the build kept and the
	// first build, the host's, did not, and the findings the build kept that the merge left out.
	added   []int
	dropped []int
}

// findingIdentity is what makes two builds' findings one finding: its file, position, rule and message. Anything
// else a build writes of it, its end or its suggestions, is the first build's to say.
type findingIdentity struct {
	file    string
	line    int
	column  int
	rule    string
	message string
}

// identityOf is found's identity across builds.
func identityOf(found check.Finding) findingIdentity {
	return findingIdentity{
		file: found.File, line: found.Line, column: found.Column, rule: found.Rule, message: found.Message,
	}
}

// mergeBuilds merges every build's outcome, the host's first (#72n9mq0). A build has a say on a file only where it
// compiled the file and analyzed its package, so a file whose package an extra build left out of its closure takes
// the host's findings as they are.
//
//   - A finding stands if any build with a say on its file kept it, but for a rule mergeIfAll names, which keeps it
//     only if every build with a say kept it: code is unused only if it is unused in every build. A file no build has
//     a say on, a `//line` directive's target, has none to disagree, so what any build kept there stands.
//   - A directive is unused only if every build with a say on its file found it unused, so a directive one build
//     needed stays, even over a mergeIfAll finding the merge drops: removing it would bring the finding back in that
//     build's own run. What else is wrong with a directive, a missing reason, a malformed comment or an unknown rule,
//     reads the same in every build, and is written once.
//   - A finding withheld in any build counts once toward its rule's withheld count.
//   - Crashes and type errors are every build's, each once.
func mergeBuilds(builds []buildOutcome, mergeIfAll map[string]bool) mergedBuilds {
	// sayers is how many builds have a say on each file, which a mergeIfAll finding must be kept by every one of.
	sayers := map[string]int{}
	for _, build := range builds {
		for file := range build.said {
			sayers[file]++
		}
	}

	merged := mergedBuilds{
		withheldByRule: map[string]int{}, added: make([]int, len(builds)), dropped: make([]int, len(builds)),
	}
	// keptIn is, by build, every finding the build kept, which says whom a finding that stands or falls is counted to.
	keptIn := make([]map[findingIdentity]bool, len(builds))
	keptBy := map[findingIdentity]int{}
	var kept []check.Finding
	withheld := map[findingIdentity]bool{}
	unusedBy := map[suppression.DirectiveFinding]int{}
	var unused []suppression.DirectiveFinding
	written := map[suppression.DirectiveFinding]bool{}
	crashed := map[check.Crash]bool{}
	typeErrors := map[load.Diagnostic]bool{}
	for buildIndex, build := range builds {
		// A build reports a finding once however its packages repeat it, so one build's say is counted once.
		keptHere := map[findingIdentity]bool{}
		keptIn[buildIndex] = keptHere
		for _, found := range build.kept {
			identity := identityOf(found)
			if keptHere[identity] {
				continue
			}
			keptHere[identity] = true
			if keptBy[identity] == 0 {
				kept = append(kept, found)
			}
			keptBy[identity]++
		}
		for _, found := range build.withheld {
			identity := identityOf(found)
			if !withheld[identity] {
				withheld[identity] = true
				merged.withheldByRule[found.Rule]++
			}
		}
		for _, directiveFinding := range build.directiveFindings {
			if directiveFinding.MessageId == suppression.MessageIdUnused {
				if unusedBy[directiveFinding] == 0 {
					unused = append(unused, directiveFinding)
				}
				unusedBy[directiveFinding]++
				continue
			}
			if !written[directiveFinding] {
				written[directiveFinding] = true
				merged.directiveFindings = append(merged.directiveFindings, directiveFinding)
			}
		}
		for _, crash := range build.crashes {
			if !crashed[crash] {
				crashed[crash] = true
				merged.crashes = append(merged.crashes, crash)
			}
		}
		for _, typeError := range build.typeErrors {
			if !typeErrors[typeError] {
				typeErrors[typeError] = true
				merged.typeErrors = append(merged.typeErrors, typeError)
			}
		}
	}

	// A file no build has a say on is one a finding's position names but no build compiled, a `//line` directive's
	// target such as a .y file or a template, where no build disagrees, so what any build says there stands.
	for _, found := range kept {
		identity := identityOf(found)
		stands := !mergeIfAll[found.Rule] || sayers[found.File] == 0 || keptBy[identity] == sayers[found.File]
		if stands {
			merged.kept = append(merged.kept, found)
		}
		for buildIndex := range builds {
			if !keptIn[buildIndex][identity] {
				continue
			}
			if !stands {
				merged.dropped[buildIndex]++
				continue
			}
			if buildIndex > 0 && !keptIn[0][identity] {
				merged.added[buildIndex]++
			}
		}
	}
	for _, directiveFinding := range unused {
		if sayers[directiveFinding.File] == 0 || unusedBy[directiveFinding] == sayers[directiveFinding.File] {
			merged.directiveFindings = append(merged.directiveFindings, directiveFinding)
		}
	}

	slices.SortStableFunc(merged.kept, func(left check.Finding, right check.Finding) int {
		return cmp.Or(
			cmp.Compare(left.File, right.File),
			cmp.Compare(left.Line, right.Line),
			cmp.Compare(left.Column, right.Column),
			cmp.Compare(left.Rule, right.Rule),
			cmp.Compare(left.Message, right.Message),
		)
	})
	slices.SortStableFunc(merged.directiveFindings,
		func(left suppression.DirectiveFinding, right suppression.DirectiveFinding) int {
			return cmp.Or(
				cmp.Compare(left.File, right.File),
				cmp.Compare(left.Line, right.Line),
				cmp.Compare(left.Column, right.Column),
				cmp.Compare(left.Rule, right.Rule),
				cmp.Compare(left.Message, right.Message),
			)
		})
	slices.SortStableFunc(merged.crashes, func(left check.Crash, right check.Crash) int {
		return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Error, right.Error))
	})
	slices.SortStableFunc(merged.typeErrors, func(left load.Diagnostic, right load.Diagnostic) int {
		return cmp.Or(
			cmp.Compare(left.File, right.File),
			cmp.Compare(left.Line, right.Line),
			cmp.Compare(left.Column, right.Column),
			cmp.Compare(left.Message, right.Message),
		)
	})
	return merged
}
