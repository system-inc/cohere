package go_engine

import (
	"cmp"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
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

// mergedBuilds is what the run reports of every build at once.
type mergedBuilds struct {
	// kept are the findings that stand, each once, sorted by file, line, column and rule.
	kept []check.Finding
	// withheldByRule counts each finding a directive withheld in some build once, by rule name.
	withheldByRule    map[string]int
	directiveFindings []suppression.DirectiveFinding
	crashes           []check.Crash
	typeErrors        []load.Diagnostic
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
//     only if every build with a say kept it: code is unused only if it is unused in every build.
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

	merged := mergedBuilds{withheldByRule: map[string]int{}}
	keptBy := map[findingIdentity]int{}
	var kept []check.Finding
	withheld := map[findingIdentity]bool{}
	unusedBy := map[suppression.DirectiveFinding]int{}
	var unused []suppression.DirectiveFinding
	written := map[suppression.DirectiveFinding]bool{}
	crashed := map[check.Crash]bool{}
	typeErrors := map[load.Diagnostic]bool{}
	for _, build := range builds {
		// A build reports a finding once however its packages repeat it, so one build's say is counted once.
		keptHere := map[findingIdentity]bool{}
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

	for _, found := range kept {
		if !mergeIfAll[found.Rule] || keptBy[identityOf(found)] == sayers[found.File] {
			merged.kept = append(merged.kept, found)
		}
	}
	for _, directiveFinding := range unused {
		if unusedBy[directiveFinding] == sayers[directiveFinding.File] {
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
