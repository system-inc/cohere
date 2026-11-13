package build_dependence

import (
	"fmt"
	"go/ast"
	"go/types"
	"maps"
	"slices"
)

// noReturnKey is a declared function as both builds can match it: its package ID, the file parsed and the offset of
// its declaration there, the same in each build that compiles the file.
type noReturnKey struct {
	packageIdentifier string
	file              string
	offset            int
}

// noReturnAnswer is what a build's ctrlflow says of one declared function: whether it never returns, with the file a
// reader sees it in, which names its unit, and its name, for the report.
type noReturnAnswer struct {
	readerFile string
	name       string
	noReturn   bool
}

// noReturnJudgment is block 2's facts judgment (go_engine/pipeline_build_facts.go, compareFacts) for ctrlflow's
// no-return fact alone: the units declaring a function whose answer differs between the host and the build, and the
// units judged again for them, those and every unit block 1 calls independent that calls one statically, followed to
// a fixed point, as factCallers follows them. A difference in such a unit is the facts judgment's to catch, not block
// 1's.
type noReturnJudgment struct {
	differing          map[int]bool
	judgedAgain        []bool
	differingFunctions []string
}

// noReturnAnswers are ctrlflow's answers for every function declared in loaded's packages that ctrlflow ran on, each
// by the package's own graphs, as production's NewPackage is handed them.
func noReturnAnswers(loaded loadedBuild, flows controlFlows) map[noReturnKey]noReturnAnswer {
	answers := map[noReturnKey]noReturnAnswer{}
	for identifier, analyzed := range loaded {
		graphs := flows[identifier]
		if graphs == nil || analyzed.TypesInfo == nil {
			continue
		}
		for _, file := range analyzed.Syntax {
			tokenFile := analyzed.Fset.File(file.FileStart)
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction {
					continue
				}
				object, isObject := analyzed.TypesInfo.Defs[function.Name].(*types.Func)
				if !isObject {
					continue
				}
				key := noReturnKey{packageIdentifier: identifier, file: tokenFile.Name(),
					offset: tokenFile.Offset(function.Pos())}
				answers[key] = noReturnAnswer{readerFile: tokenFile.Position(function.Pos()).Filename,
					name: object.FullName(), noReturn: graphs.NoReturn(object)}
			}
		}
	}
	return answers
}

// judgeNoReturn is the build's no-return judgment against the host, over the units the build's dependence names.
// A function only one build declares sits in a file only that build compiles, whose units are dependent or not the
// host's, and is left out, as compareFacts leaves out a fact about an object in such a file.
func judgeNoReturn(host loadedBuild, build loadedBuild, hostFlows controlFlows, buildFlows controlFlows,
	dependence *Dependence) noReturnJudgment {
	units := dependence.Units()
	judgment := noReturnJudgment{differing: map[int]bool{}, judgedAgain: make([]bool, len(units))}
	builtAnswers := noReturnAnswers(build, buildFlows)
	for key, hosted := range noReturnAnswers(host, hostFlows) {
		built, isShared := builtAnswers[key]
		if !isShared || built.noReturn == hosted.noReturn {
			continue
		}
		judgment.differingFunctions = append(judgment.differingFunctions, fmt.Sprintf("%s (%s:%d): host never "+
			"returns %t, build %t", hosted.name, hosted.readerFile, key.offset, hosted.noReturn, built.noReturn))
		if unit, isUnit := dependence.UnitContaining(key.packageIdentifier, hosted.readerFile, key.offset); isUnit {
			judgment.differing[unit] = true
		}
	}
	slices.Sort(judgment.differingFunctions)
	callers := make([][]int, len(units))
	for index, unit := range units {
		for _, callee := range unit.Calls {
			callers[callee] = append(callers[callee], index)
		}
	}
	for unit := range judgment.differing {
		judgment.judgedAgain[unit] = true
	}
	reached := maps.Clone(judgment.differing)
	pending := slices.Collect(maps.Keys(judgment.differing))
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, caller := range callers[current] {
			if units[caller].Dependent || judgment.judgedAgain[caller] {
				continue
			}
			judgment.judgedAgain[caller] = true
			if !reached[caller] {
				reached[caller] = true
				pending = append(pending, caller)
			}
		}
	}
	return judgment
}

// judgedAgainCount is how many units block 1 calls independent the judgment sends to be judged again.
func judgedAgainCount(judgment noReturnJudgment, units []Unit) int {
	count := 0
	for index, unit := range units {
		if judgment.judgedAgain[index] && !unit.Dependent {
			count++
		}
	}
	return count
}
