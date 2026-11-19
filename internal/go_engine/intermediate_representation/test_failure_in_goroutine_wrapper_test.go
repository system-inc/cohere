package intermediate_representation_test

// SA2002's check over a function whose body was built without its wrappers (#h0k644s). It is a test of the rule
// test_failure_in_goroutine, and it lives in the representation's directory because only here can a body be built
// without the wrappers it queued (BuildBodyAloneForTest, compiled into this package's tests alone). Every build the
// rule's own tests can run goes on to build them, so there no test tells a check that builds a wrapper before it reads
// it from one that reads it as found.

import (
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/test_failure_in_goroutine"
)

// unbuiltWrapperSource holds the shape SA2002 reports from a wrapper's blocks: a goroutine started from a method value
// of the test, whose bound method wrapper calls the method that stops it.
const unbuiltWrapperSource = `package wrapped

import "testing"

func TestStop(t *testing.T) {
	stop := t.FailNow
	go stop()
}
`

// A goroutine started from a method value is found though the method value's wrapper is unbuilt when the check
// starts, as it is for a caller whose Build of the test function returned before another builder reached the wrapper:
// the check calls Build on the wrapper before it reads its blocks. A check that read them as it found them would read
// none and report nothing.
func TestTheGoroutineCheckBuildsAWrapperBeforeItReadsItsBlocks(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "wrapped", unbuiltWrapperSource)
	program := intermediate_representation.NewProgram(unit.fileSet)
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	function := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false).Function("TestStop")
	intermediate_representation.BuildBodyAloneForTest(function)
	var wrapper *intermediate_representation.Function
	var goStatement *intermediate_representation.Go
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			started, isGo := instruction.(*intermediate_representation.Go)
			if !isGo {
				continue
			}
			goStatement = started
			if closure, isClosure := started.Call.Value.(*intermediate_representation.MakeClosure); isClosure {
				wrapper, _ = closure.Function.(*intermediate_representation.Function)
			}
		}
	}
	if wrapper == nil || wrapper.Package != nil || wrapper.Blocks != nil {
		test.Fatalf("the go statement of TestStop starts %v, want a wrapper, a function of no package, left unbuilt "+
			"by the build of the body alone", wrapper)
	}
	var found []analysis.Diagnostic
	pass := &analysis.Pass{Fset: unit.fileSet, Files: unit.files, Pkg: unit.types, TypesInfo: unit.typeInfo}
	context := rule.NewContext(rule.Namespace+"SA2002", pass, test_failure_in_goroutine.Listener.Requires,
		&test_failure_in_goroutine.Analyzer.Flags, func(string) bool { return false },
		func(_ string, diagnostic analysis.Diagnostic) { found = append(found, diagnostic) })
	test_failure_in_goroutine.Check(context, function)
	if len(found) != 1 {
		test.Fatalf("the check found %d findings over a body built without its wrapper, want the one at the go "+
			"statement: %v", len(found), found)
	}
	const message = "the goroutine calls T.FailNow, which must be called in the same goroutine as the test"
	if found[0].Message != message || found[0].Pos != goStatement.Source().Pos() || len(found[0].Related) != 0 {
		test.Errorf("the check found %q at %s with %d related entries, want %q at %s with none, the wrapper's call "+
			"having no syntax", found[0].Message, unit.fileSet.Position(found[0].Pos), len(found[0].Related), message,
			unit.fileSet.Position(goStatement.Source().Pos()))
	}
	if wrapper.Blocks == nil {
		test.Error("the wrapper has no blocks after the check, which reads them")
	}
}
